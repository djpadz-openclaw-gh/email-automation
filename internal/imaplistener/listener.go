// Package imaplistener monitors IMAP accounts and publishes new messages
// to JetStream for processing by the rules engine.
package imaplistener

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomessage "github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/models"
	natsbus "github.com/djpadz/email-automation/internal/nats"
	"github.com/djpadz/email-automation/internal/oauth2"
)

// Listener manages IMAP connections for all active accounts and publishes
// new messages to the email.incoming JetStream stream.
type Listener struct {
	db  *db.DB
	bus *natsbus.Bus

	idleTimeout  time.Duration
	pollInterval time.Duration

	// OAuth2 providers for token refresh
	oauthProviders map[string]oauth2.Provider

	workers map[int64]*worker
	mu      sync.RWMutex
	stopCh  chan struct{}
}

// New creates a new IMAP listener.
func New(database *db.DB, bus *natsbus.Bus, idleTimeout, pollInterval time.Duration) *Listener {
	return &Listener{
		db:             database,
		bus:            bus,
		idleTimeout:    idleTimeout,
		pollInterval:   pollInterval,
		oauthProviders: make(map[string]oauth2.Provider),
		workers:        make(map[int64]*worker),
		stopCh:         make(chan struct{}),
	}
}

// SetOAuthProviders sets the OAuth2 providers for token refresh.
func (l *Listener) SetOAuthProviders(providers map[string]oauth2.Provider) {
	l.oauthProviders = providers
}

// Start begins the listener, spawning workers for all active accounts.
func (l *Listener) Start(ctx context.Context) error {
	log.Info().Msg("starting IMAP listener")

	if err := l.refreshWorkers(ctx); err != nil {
		return fmt.Errorf("initial worker refresh: %w", err)
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-l.stopCh:
				return
			case <-ticker.C:
				if err := l.refreshWorkers(ctx); err != nil {
					log.Error().Err(err).Msg("failed to refresh IMAP workers")
				}
			}
		}
	}()

	return nil
}

// Stop shuts down all workers.
func (l *Listener) Stop() {
	close(l.stopCh)
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, w := range l.workers {
		log.Info().Int64("account_id", id).Msg("stopping IMAP worker")
		w.stop()
		delete(l.workers, id)
	}
}

func (l *Listener) refreshWorkers(ctx context.Context) error {
	accounts, err := l.db.ListActiveAccounts(ctx)
	if err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	activeIDs := make(map[int64]bool)
	for _, acc := range accounts {
		activeIDs[acc.ID] = true
		if w, exists := l.workers[acc.ID]; exists {
			// Check if credentials changed — restart worker if so
			if credentialsChanged(w.account, acc) {
				log.Info().Int64("account_id", acc.ID).Str("email", acc.Email).Msg("credentials changed, restarting worker")
				w.stop()
				delete(l.workers, acc.ID)
			} else {
				continue
			}
		}
		w := newWorker(acc, l.db, l.bus, l.idleTimeout, l.pollInterval, l.oauthProviders)
		l.workers[acc.ID] = w
		go w.run(ctx)
		log.Info().Int64("account_id", acc.ID).Str("email", acc.Email).Msg("started IMAP listener worker")
	}

	for id, w := range l.workers {
		if !activeIDs[id] {
			log.Info().Int64("account_id", id).Msg("stopping deactivated IMAP worker")
			w.stop()
			delete(l.workers, id)
		}
	}

	return nil
}

// credentialsChanged returns true if the account's authentication credentials
// differ between the cached worker copy and the freshly-loaded database copy.
func credentialsChanged(cached, fresh models.Account) bool {
	if cached.Password != fresh.Password {
		return true
	}
	if cached.OAuthRefreshToken != fresh.OAuthRefreshToken {
		return true
	}
	if cached.Username != fresh.Username {
		return true
	}
	if cached.IMAPHost != fresh.IMAPHost || cached.IMAPPort != fresh.IMAPPort {
		return true
	}
	return false
}

// worker monitors a single IMAP account and publishes new messages to JetStream.
type worker struct {
	account        models.Account
	db             *db.DB
	bus            *natsbus.Bus
	idleTimeout    time.Duration
	pollInterval   time.Duration
	oauthProviders map[string]oauth2.Provider
	stopCh         chan struct{}
	client         *imapclient.Client
	clientMu       sync.Mutex
	mailboxCh      chan struct{} // signaled by UnilateralDataHandler on new mail

	// IDLE-based move detection
	expungeCh chan uint32   // receives sequence numbers from EXPUNGE notifications
	inboxUIDs []imap.UID   // current INBOX UIDs ordered by sequence number
	uidsMu    sync.Mutex   // protects inboxUIDs
}

func newWorker(account models.Account, database *db.DB, bus *natsbus.Bus, idleTimeout, pollInterval time.Duration, oauthProviders map[string]oauth2.Provider) *worker {
	return &worker{
		account:        account,
		db:             database,
		bus:            bus,
		idleTimeout:    idleTimeout,
		pollInterval:   pollInterval,
		oauthProviders: oauthProviders,
		stopCh:         make(chan struct{}),
		mailboxCh:      make(chan struct{}, 1),
		expungeCh:      make(chan uint32, 64),
	}
}

func (w *worker) run(ctx context.Context) {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("email", w.account.Email).
		Logger()

	logger.Info().Msg("IMAP listener worker starting")

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("IMAP listener worker stopping (context cancelled)")
			w.disconnect()
			return
		case <-w.stopCh:
			logger.Info().Msg("IMAP listener worker stopping (stop signal)")
			w.disconnect()
			return
		default:
		}

		if err := w.idleLoop(ctx); err != nil {
			logger.Error().Err(err).Msg("IDLE loop failed, reconnecting")
			w.disconnect()

			// Back off before reconnecting
			select {
			case <-ctx.Done():
				return
			case <-w.stopCh:
				return
			case <-time.After(10 * time.Second):
			}
		}
	}
}

// idleLoop connects, does an initial poll, then enters IMAP IDLE to wait
// for real-time notifications of new messages and EXPUNGE events for move detection.
func (w *worker) idleLoop(ctx context.Context) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("email", w.account.Email).
		Logger()

	client, err := w.connect()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Select INBOX
	selectCmd := client.Select("INBOX", nil)
	if _, err := selectCmd.Wait(); err != nil {
		return fmt.Errorf("select INBOX: %w", err)
	}

	// Build UID tracking list for EXPUNGE→UID mapping
	if err := w.buildUIDMap(ctx, client); err != nil {
		logger.Warn().Err(err).Msg("failed to build UID map, move detection may not work")
	}

	// Drain any stale expunge notifications
	w.drainExpungeChannel()

	// Initial poll for new messages (UID > last_uid_processed)
	if err := w.pollNewMessages(ctx, client); err != nil {
		return fmt.Errorf("initial poll: %w", err)
	}

	logger.Info().Msg("entering IMAP IDLE mode")

	// IDLE loop: enter IDLE, wait for notification, poll, repeat
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.stopCh:
			return nil
		default:
		}

		// Drain any pending notifications before entering IDLE
		select {
		case <-w.mailboxCh:
		default:
		}

		// Enter IDLE
		idleCmd, err := client.Idle()
		if err != nil {
			return fmt.Errorf("start IDLE: %w", err)
		}

		logger.Debug().Msg("IDLE started, waiting for server notifications")

		// Collect EXPUNGE notifications during IDLE
		var expungedSeqNums []uint32
		timer := time.NewTimer(w.idleTimeout)

		var idleInterrupted bool
	idle_loop:
		for {
			select {
			case <-w.mailboxCh:
				logger.Debug().Msg("mailbox notification received during IDLE")
				idleInterrupted = true
				// Give a short window to collect rapid-fire notifications
				timer.Reset(2 * time.Second)
			case seqNum := <-w.expungeCh:
				logger.Info().Uint32("seq_num", seqNum).Msg("EXPUNGE received during IDLE")
				expungedSeqNums = append(expungedSeqNums, seqNum)
				idleInterrupted = true
				// Give a short window to collect multiple rapid expunges
				timer.Reset(2 * time.Second)
			case <-timer.C:
				if !idleInterrupted {
					logger.Debug().Msg("IDLE timeout reached, will re-poll")
				}
				break idle_loop
			case <-ctx.Done():
				timer.Stop()
				idleCmd.Close()
				return ctx.Err()
			case <-w.stopCh:
				timer.Stop()
				idleCmd.Close()
				return nil
			}
		}
		timer.Stop()

		// Stop IDLE so we can issue commands
		if err := idleCmd.Close(); err != nil {
			return fmt.Errorf("close IDLE: %w", err)
		}
		if err := idleCmd.Wait(); err != nil {
			return fmt.Errorf("wait IDLE: %w", err)
		}

		// Handle EXPUNGE-based move detection
		if len(expungedSeqNums) > 0 {
			expungedUIDs := w.mapSeqNumsToUIDs(expungedSeqNums)
			if len(expungedUIDs) > 0 {
				if err := w.handleExpungedMessages(ctx, client, expungedUIDs); err != nil {
					logger.Warn().Err(err).Msg("failed to handle expunged messages")
				}
				// Re-select INBOX after searching other folders
				reselectCmd := client.Select("INBOX", nil)
				if _, err := reselectCmd.Wait(); err != nil {
					return fmt.Errorf("re-select INBOX after move detection: %w", err)
				}
			}
		}

		// Poll for new messages
		if err := w.pollNewMessages(ctx, client); err != nil {
			return fmt.Errorf("poll after IDLE: %w", err)
		}

		// Rebuild UID map after changes
		if err := w.buildUIDMap(ctx, client); err != nil {
			logger.Warn().Err(err).Msg("failed to rebuild UID map")
		}
	}
}

func (w *worker) stop() {
	close(w.stopCh)
}

func (w *worker) connect() (*imapclient.Client, error) {
	w.clientMu.Lock()
	defer w.clientMu.Unlock()

	if w.client != nil {
		return w.client, nil
	}

	host := w.account.IMAPHost
	port := w.account.IMAPPort
	if port == 0 {
		port = 993
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	// Set up unilateral data handler to receive new-mail and EXPUNGE notifications during IDLE
	opts := &imapclient.Options{
		TLSConfig: &tls.Config{
			ServerName: host,
		},
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: func(data *imapclient.UnilateralDataMailbox) {
				if data.NumMessages != nil {
					log.Debug().
						Int64("account_id", w.account.ID).
						Uint32("num_messages", *data.NumMessages).
						Msg("mailbox update received")
					// Non-blocking send to signal new mail
					select {
					case w.mailboxCh <- struct{}{}:
					default:
					}
				}
			},
			Expunge: func(seqNum uint32) {
				log.Debug().
					Int64("account_id", w.account.ID).
					Uint32("seq_num", seqNum).
					Msg("EXPUNGE notification received")
				// Send to expunge channel for move detection
				select {
				case w.expungeCh <- seqNum:
				default:
					log.Warn().
						Int64("account_id", w.account.ID).
						Uint32("seq_num", seqNum).
						Msg("expunge channel full, dropping notification")
				}
				// Also signal mailbox change
				select {
				case w.mailboxCh <- struct{}{}:
				default:
				}
			},
		},
	}

	var client *imapclient.Client
	var err error

	if w.account.IMAPTLS {
		client, err = imapclient.DialTLS(addr, opts)
	} else {
		conn, dialErr := net.DialTimeout("tcp", addr, 30*time.Second)
		if dialErr != nil {
			return nil, fmt.Errorf("dial %s: %w", addr, dialErr)
		}
		client = imapclient.New(conn, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}

	if err := client.WaitGreeting(); err != nil {
		client.Close()
		return nil, fmt.Errorf("greeting from %s: %w", addr, err)
	}

	username := w.account.Username
	if username == "" {
		username = w.account.Email
	}

	// Authenticate: use XOAUTH2 for OAuth2 accounts, plain login otherwise
	if w.account.OAuthProvider != "" && w.account.OAuthToken != "" {
		// Refresh token if expired
		if oauth2.IsTokenExpired(w.account.OAuthTokenExpiry) {
			if err := w.refreshOAuthToken(); err != nil {
				client.Close()
				return nil, fmt.Errorf("refresh OAuth2 token: %w", err)
			}
		}

		// Build XOAUTH2 SASL client and authenticate
		xoauth2Client := &oauth2.XOAuth2Client{
			Username: username,
			Token:    w.account.OAuthToken,
		}
		if err := client.Authenticate(xoauth2Client); err != nil {
			// Token might have just expired, try one refresh
			log.Warn().Err(err).Int64("account_id", w.account.ID).Msg("XOAUTH2 auth failed, attempting token refresh")
			if refreshErr := w.refreshOAuthToken(); refreshErr != nil {
				client.Close()
				return nil, fmt.Errorf("XOAUTH2 auth failed and refresh failed: auth=%w, refresh=%v", err, refreshErr)
			}
			// Reconnect with new token
			client.Close()
			return nil, fmt.Errorf("XOAUTH2 auth failed, token refreshed, will reconnect: %w", err)
		}
		log.Info().Int64("account_id", w.account.ID).Str("addr", addr).Msg("IMAP XOAUTH2 login successful")
	} else {
		loginCmd := client.Login(username, w.account.Password)
		if err := loginCmd.Wait(); err != nil {
			client.Close()
			return nil, fmt.Errorf("login to %s: %w", addr, err)
		}
		log.Info().Int64("account_id", w.account.ID).Str("addr", addr).Msg("IMAP login successful")
	}

	w.client = client
	return client, nil
}

// refreshOAuthToken refreshes the OAuth2 access token using the refresh token.
func (w *worker) refreshOAuthToken() error {
	provider, ok := w.oauthProviders[w.account.OAuthProvider]
	if !ok {
		return fmt.Errorf("OAuth2 provider %s not configured", w.account.OAuthProvider)
	}

	if w.account.OAuthRefreshToken == "" {
		return fmt.Errorf("no refresh token available for account %d", w.account.ID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tokenResp, err := provider.RefreshToken(ctx, w.account.OAuthRefreshToken)
	if err != nil {
		return fmt.Errorf("refresh token: %w", err)
	}

	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	// Update in-memory account
	w.account.OAuthToken = tokenResp.AccessToken
	w.account.OAuthTokenExpiry = &expiry
	if tokenResp.RefreshToken != "" {
		w.account.OAuthRefreshToken = tokenResp.RefreshToken
	}

	// Persist to database
	refreshToken := tokenResp.RefreshToken
	if refreshToken == "" {
		refreshToken = w.account.OAuthRefreshToken
	}
	if err := w.db.UpdateAccountOAuthTokens(ctx, w.account.ID, tokenResp.AccessToken, refreshToken, &expiry); err != nil {
		log.Error().Err(err).Int64("account_id", w.account.ID).Msg("failed to persist refreshed OAuth2 tokens")
		// Don't return error - we have the token in memory and can still use it
	}

	log.Info().
		Int64("account_id", w.account.ID).
		Str("provider", w.account.OAuthProvider).
		Time("expiry", expiry).
		Msg("OAuth2 token refreshed")

	return nil
}

func (w *worker) disconnect() {
	w.clientMu.Lock()
	defer w.clientMu.Unlock()

	if w.client != nil {
		logoutCmd := w.client.Logout()
		_ = logoutCmd.Wait()
		w.client.Close()
		w.client = nil
	}
}

// pollNewMessages searches for messages with UID > last_uid_processed
// and publishes them to JetStream. This replaces the unseen-flag approach.
func (w *worker) pollNewMessages(ctx context.Context, client *imapclient.Client) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("email", w.account.Email).
		Logger()

	logger.Debug().Msg("polling for new messages (UID-based)")

	// Get last processed UID from database
	lastUID, err := w.db.GetLastUIDProcessed(ctx, w.account.ID)
	if err != nil {
		return fmt.Errorf("get last UID processed: %w", err)
	}

	// Search for messages with UID > lastUID
	// We search all UIDs and filter client-side for compatibility
	criteria := &imap.SearchCriteria{}
	searchCmd := client.UIDSearch(criteria, nil)
	searchData, err := searchCmd.Wait()
	if err != nil {
		return fmt.Errorf("search UIDs: %w", err)
	}

	// Filter to only UIDs greater than lastUID
	var uids []imap.UID
	for _, uid := range searchData.AllUIDs() {
		if uint32(uid) > lastUID {
			uids = append(uids, uid)
		}
	}
	if len(uids) == 0 {
		logger.Debug().Uint32("last_uid", lastUID).Msg("no new messages")
		if err := w.db.UpdateAccountSyncTime(ctx, w.account.ID); err != nil {
			logger.Warn().Err(err).Msg("failed to update sync time")
		}
		return nil
	}

	logger.Info().Int("count", len(uids)).Uint32("last_uid", lastUID).Msg("found new messages")

	// Process in batches
	batchSize := 50
	var maxUID imap.UID
	for i := 0; i < len(uids); i += batchSize {
		end := i + batchSize
		if end > len(uids) {
			end = len(uids)
		}
		batch := uids[i:end]

		if err := w.publishBatch(ctx, client, batch); err != nil {
			logger.Error().Err(err).Int("batch_start", i).Msg("batch publish failed")
		}

		// Track max UID in this batch
		for _, uid := range batch {
			if uid > maxUID {
				maxUID = uid
			}
		}
	}

	// Update last_uid_processed to the max UID we just processed
	if maxUID > 0 {
		if err := w.db.UpdateLastUIDProcessed(ctx, w.account.ID, uint32(maxUID)); err != nil {
			logger.Warn().Err(err).Uint32("max_uid", uint32(maxUID)).Msg("failed to update last UID processed")
		} else {
			logger.Info().Uint32("max_uid", uint32(maxUID)).Msg("updated last UID processed")
		}
	}

	return nil
}

// publishBatch fetches a batch of messages and publishes each to JetStream.
func (w *worker) publishBatch(ctx context.Context, client *imapclient.Client, uids []imap.UID) error {
	if len(uids) == 0 {
		return nil
	}

	uidSet := imap.UIDSet{}
	for _, uid := range uids {
		uidSet.AddNum(uid)
	}

	fetchOptions := &imap.FetchOptions{
		Envelope: true,
		Flags:    true,
		UID:      true,
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierHeader, Peek: true},
			{Specifier: imap.PartSpecifierText, Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: 4096}},
		},
		BodyStructure: &imap.FetchItemBodyStructure{Extended: false},
	}

	fetchCmd := client.Fetch(uidSet, fetchOptions)
	defer fetchCmd.Close()

	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}

		buf, err := msg.Collect()
		if err != nil {
			log.Error().Err(err).Msg("failed to collect message data")
			continue
		}

		// Check if already processed
		processed, err := w.db.IsMessageProcessed(ctx, w.account.ID, fmt.Sprintf("%d", buf.UID))
		if err != nil {
			log.Warn().Err(err).Msg("failed to check processed status")
		}
		if processed {
			continue
		}

		event := w.bufferToEvent(buf)
		if event == nil {
			continue
		}

		// Fetch image attachments for vision analysis if images were detected
		if event.HasImages && buf.BodyStructure != nil {
			imageParts := findImageParts(buf.BodyStructure, nil)
			if len(imageParts) > 0 {
				log.Info().
					Str("message_id", event.MessageID).
					Int("image_count", len(imageParts)).
					Msg("fetching image attachments for vision analysis")
				event.ImageAttachments = w.fetchImageAttachments(client, buf.UID, imageParts)
			}
		}

		// Publish to JetStream
		subject := fmt.Sprintf("email.incoming.%d", w.account.ID)
		if err := w.bus.PublishToStream(ctx, subject, event); err != nil {
			log.Error().Err(err).
				Str("message_id", event.MessageID).
				Msg("failed to publish message to JetStream")
			continue
		}

		log.Info().
			Str("message_id", event.MessageID).
			Str("subject", event.Subject).
			Str("sender", event.Sender).
			Msg("published message to email.incoming")
	}

	return nil
}

// bufferToEvent converts a FetchMessageBuffer to an IncomingEmailEvent.
func (w *worker) bufferToEvent(buf *imapclient.FetchMessageBuffer) *natsbus.IncomingEmailEvent {
	if buf.Envelope == nil {
		return nil
	}

	env := buf.Envelope

	var senderName, senderAddr string
	if len(env.From) > 0 {
		senderName = env.From[0].Name
		senderAddr = env.From[0].Addr()
	}

	var recipients []string
	for _, addr := range env.To {
		recipients = append(recipients, addr.Addr())
	}
	for _, addr := range env.Cc {
		recipients = append(recipients, addr.Addr())
	}

	headers := make(map[string]string)
	var bodyPreview string

	for _, section := range buf.BodySection {
		if section.Section.Specifier == imap.PartSpecifierHeader && len(section.Bytes) > 0 {
			headers = parseHeaders(section.Bytes)
		}
		if section.Section.Specifier == imap.PartSpecifierText && len(section.Bytes) > 0 {
			bodyPreview = string(section.Bytes)
			if len(bodyPreview) > 500 {
				bodyPreview = bodyPreview[:500]
			}
		}
	}

	var attachmentNames, attachmentTypes []string
	hasAttachments := false
	if buf.BodyStructure != nil {
		attachmentNames, attachmentTypes = extractAttachmentInfo(buf.BodyStructure)
		hasAttachments = len(attachmentNames) > 0 || len(attachmentTypes) > 0
	}

	msgDate := env.Date
	if msgDate.IsZero() {
		msgDate = buf.InternalDate
	}

	ageSeconds := time.Since(msgDate).Seconds()
	if ageSeconds < 0 {
		ageSeconds = 0
	}

	return &natsbus.IncomingEmailEvent{
		AccountID:       w.account.ID,
		TenantID:        w.account.TenantID,
		MessageID:       env.MessageID,
		UID:             uint32(buf.UID),
		Subject:         strings.TrimSpace(env.Subject),
		Sender:          senderAddr,
		SenderName:      senderName,
		Recipients:      recipients,
		Date:            msgDate,
		AgeSeconds:      ageSeconds,
		BodyPreview:     bodyPreview,
		Folder:          "INBOX",
		Headers:         headers,
		HasAttachments:  hasAttachments,
		AttachmentNames: attachmentNames,
		AttachmentTypes: attachmentTypes,
		HasImages:       hasImageAttachments(attachmentTypes),
	}
}

// --- Helper functions (extracted from internal/imap/worker.go) ---

func parseHeaders(raw []byte) map[string]string {
	headers := make(map[string]string)

	entity, err := gomessage.Read(strings.NewReader(string(raw) + "\r\n\r\n"))
	if err != nil {
		return parseHeadersSimple(raw)
	}

	headerFields := entity.Header.Fields()
	for headerFields.Next() {
		key := strings.ToLower(headerFields.Key())
		val := headerFields.Value()
		headers[key] = val
	}

	return headers
}

func parseHeadersSimple(raw []byte) map[string]string {
	headers := make(map[string]string)
	lines := strings.Split(string(raw), "\n")
	var currentKey, currentVal string

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			currentVal += " " + strings.TrimSpace(line)
			continue
		}
		if currentKey != "" {
			headers[strings.ToLower(currentKey)] = currentVal
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			currentKey = strings.TrimSpace(parts[0])
			currentVal = strings.TrimSpace(parts[1])
		}
	}
	if currentKey != "" {
		headers[strings.ToLower(currentKey)] = currentVal
	}
	return headers
}

type imagePartInfo struct {
	Part      []int
	MediaType string
	Filename  string
	Size      uint32
}

const maxImageSize = 5 * 1024 * 1024
const maxImageAttachments = 4

func extractAttachmentInfo(bs imap.BodyStructure) (names []string, types []string) {
	switch s := bs.(type) {
	case *imap.BodyStructureSinglePart:
		disp := s.Disposition()
		if disp != nil {
			dispVal := strings.ToLower(disp.Value)
			if dispVal == "attachment" || dispVal == "inline" {
				ct := s.MediaType()
				types = append(types, ct)
				filename := s.Filename()
				if filename != "" {
					names = append(names, filename)
				}
			}
		}
	case *imap.BodyStructureMultiPart:
		for _, child := range s.Children {
			cn, ct := extractAttachmentInfo(child)
			names = append(names, cn...)
			types = append(types, ct...)
		}
	}
	return
}

func findImageParts(bs imap.BodyStructure, path []int) []imagePartInfo {
	var parts []imagePartInfo

	switch s := bs.(type) {
	case *imap.BodyStructureSinglePart:
		mt := strings.ToLower(s.MediaType())
		if strings.HasPrefix(mt, "image/") {
			switch mt {
			case "image/jpeg", "image/png", "image/gif", "image/webp":
				if s.Size <= maxImageSize {
					partPath := make([]int, len(path))
					copy(partPath, path)
					parts = append(parts, imagePartInfo{
						Part:      partPath,
						MediaType: mt,
						Filename:  s.Filename(),
						Size:      s.Size,
					})
				}
			}
		}
	case *imap.BodyStructureMultiPart:
		for i, child := range s.Children {
			childPath := append(append([]int{}, path...), i+1)
			parts = append(parts, findImageParts(child, childPath)...)
		}
	}

	return parts
}

func (w *worker) fetchImageAttachments(client *imapclient.Client, uid imap.UID, imageParts []imagePartInfo) []natsbus.ImageAttachment {
	if len(imageParts) == 0 {
		return nil
	}

	if len(imageParts) > maxImageAttachments {
		imageParts = imageParts[:maxImageAttachments]
	}

	var bodySections []*imap.FetchItemBodySection
	for _, ip := range imageParts {
		bodySections = append(bodySections, &imap.FetchItemBodySection{
			Part: ip.Part,
			Peek: true,
		})
	}

	uidSet := imap.UIDSetNum(uid)
	fetchOptions := &imap.FetchOptions{
		UID:         true,
		BodySection: bodySections,
	}

	fetchCmd := client.Fetch(uidSet, fetchOptions)
	defer fetchCmd.Close()

	var attachments []natsbus.ImageAttachment

	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}

		buf, err := msg.Collect()
		if err != nil {
			log.Error().Err(err).Msg("failed to collect image attachment data")
			continue
		}

		for _, section := range buf.BodySection {
			if len(section.Bytes) == 0 {
				continue
			}

			for _, ip := range imageParts {
				if partsEqual(section.Section.Part, ip.Part) {
					encoded := base64.StdEncoding.EncodeToString(section.Bytes)
					attachments = append(attachments, natsbus.ImageAttachment{
						Filename:  ip.Filename,
						MediaType: ip.MediaType,
						Data:      encoded,
					})
					break
				}
			}
		}
	}

	return attachments
}

func partsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasImageAttachments(attachmentTypes []string) bool {
	for _, ct := range attachmentTypes {
		lower := strings.ToLower(ct)
		if strings.HasPrefix(lower, "image/") {
			return true
		}
	}
	return false
}
