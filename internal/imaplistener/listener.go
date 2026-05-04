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
)

// Listener manages IMAP connections for all active accounts and publishes
// new messages to the email.incoming JetStream stream.
type Listener struct {
	db  *db.DB
	bus *natsbus.Bus

	idleTimeout  time.Duration
	pollInterval time.Duration

	workers map[int64]*worker
	mu      sync.RWMutex
	stopCh  chan struct{}
}

// New creates a new IMAP listener.
func New(database *db.DB, bus *natsbus.Bus, idleTimeout, pollInterval time.Duration) *Listener {
	return &Listener{
		db:           database,
		bus:          bus,
		idleTimeout:  idleTimeout,
		pollInterval: pollInterval,
		workers:      make(map[int64]*worker),
		stopCh:       make(chan struct{}),
	}
}

// Start begins the listener, spawning workers for all active accounts.
func (l *Listener) Start(ctx context.Context) error {
	log.Info().Msg("starting IMAP listener")

	if err := l.refreshWorkers(ctx); err != nil {
		return fmt.Errorf("initial worker refresh: %w", err)
	}

	go func() {
		ticker := time.NewTicker(60 * time.Second)
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
		if _, exists := l.workers[acc.ID]; !exists {
			w := newWorker(acc, l.db, l.bus, l.idleTimeout, l.pollInterval)
			l.workers[acc.ID] = w
			go w.run(ctx)
			log.Info().Int64("account_id", acc.ID).Str("email", acc.Email).Msg("started IMAP listener worker")
		}
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

// worker monitors a single IMAP account and publishes new messages to JetStream.
type worker struct {
	account      models.Account
	db           *db.DB
	bus          *natsbus.Bus
	idleTimeout  time.Duration
	pollInterval time.Duration
	stopCh       chan struct{}
	client       *imapclient.Client
	clientMu     sync.Mutex
}

func newWorker(account models.Account, database *db.DB, bus *natsbus.Bus, idleTimeout, pollInterval time.Duration) *worker {
	return &worker{
		account:      account,
		db:           database,
		bus:          bus,
		idleTimeout:  idleTimeout,
		pollInterval: pollInterval,
		stopCh:       make(chan struct{}),
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

		if err := w.poll(ctx); err != nil {
			logger.Error().Err(err).Msg("poll cycle failed")
			w.disconnect()
		}

		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-time.After(w.pollInterval):
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

	opts := &imapclient.Options{
		TLSConfig: &tls.Config{
			ServerName: host,
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

	loginCmd := client.Login(username, w.account.Password)
	if err := loginCmd.Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("login to %s: %w", addr, err)
	}

	log.Info().Int64("account_id", w.account.ID).Str("addr", addr).Msg("IMAP login successful")
	w.client = client
	return client, nil
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

// poll checks for new messages and publishes them to JetStream.
func (w *worker) poll(ctx context.Context) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("email", w.account.Email).
		Logger()

	client, err := w.connect()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	logger.Debug().Msg("polling for new messages")

	selectCmd := client.Select("INBOX", nil)
	mbox, err := selectCmd.Wait()
	if err != nil {
		return fmt.Errorf("select INBOX: %w", err)
	}

	logger.Debug().Uint32("messages", mbox.NumMessages).Msg("INBOX selected")

	if mbox.NumMessages == 0 {
		if err := w.db.UpdateAccountSyncTime(ctx, w.account.ID); err != nil {
			logger.Warn().Err(err).Msg("failed to update sync time")
		}
		return nil
	}

	criteria := &imap.SearchCriteria{
		NotFlag: []imap.Flag{imap.FlagSeen},
	}
	searchCmd := client.UIDSearch(criteria, nil)
	searchData, err := searchCmd.Wait()
	if err != nil {
		return fmt.Errorf("search unseen: %w", err)
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		logger.Debug().Msg("no unseen messages")
		if err := w.db.UpdateAccountSyncTime(ctx, w.account.ID); err != nil {
			logger.Warn().Err(err).Msg("failed to update sync time")
		}
		return nil
	}

	logger.Info().Int("count", len(uids)).Msg("found unseen messages")

	batchSize := 50
	for i := 0; i < len(uids); i += batchSize {
		end := i + batchSize
		if end > len(uids) {
			end = len(uids)
		}
		batch := uids[i:end]

		if err := w.publishBatch(ctx, client, batch); err != nil {
			logger.Error().Err(err).Int("batch_start", i).Msg("batch publish failed")
		}
	}

	if err := w.db.UpdateAccountSyncTime(ctx, w.account.ID); err != nil {
		logger.Warn().Err(err).Msg("failed to update sync time")
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
		Subject:         env.Subject,
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
