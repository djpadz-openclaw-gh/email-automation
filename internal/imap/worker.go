package imap

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomessage "github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/engine"
	"github.com/djpadz/email-automation/internal/models"
	natsbus "github.com/djpadz/email-automation/internal/nats"
	"github.com/djpadz/email-automation/internal/notifier"
)

// Pool manages a set of IMAP workers, one per active account.
type Pool struct {
	db       *db.DB
	engine   *engine.Engine
	bus      *natsbus.Bus
	notifier *notifier.Telegram

	idleTimeout  time.Duration
	pollInterval time.Duration

	workers map[int64]*Worker
	mu      sync.RWMutex
	stopCh  chan struct{}
}

// NewPool creates a new IMAP worker pool.
func NewPool(database *db.DB, eng *engine.Engine, bus *natsbus.Bus, telegram *notifier.Telegram, idleTimeout, pollInterval time.Duration) *Pool {
	return &Pool{
		db:           database,
		engine:       eng,
		bus:          bus,
		notifier:     telegram,
		idleTimeout:  idleTimeout,
		pollInterval: pollInterval,
		workers:      make(map[int64]*Worker),
		stopCh:       make(chan struct{}),
	}
}

// Start begins the worker pool, spawning workers for all active accounts.
func (p *Pool) Start(ctx context.Context) error {
	log.Info().Msg("starting IMAP worker pool")

	if err := p.refreshWorkers(ctx); err != nil {
		return fmt.Errorf("initial worker refresh: %w", err)
	}

	// Subscribe to deferred action events from NATS
	if p.bus != nil {
		if _, err := p.bus.Subscribe(natsbus.SubjectDeferredAction, func(data []byte) {
			var event natsbus.DeferredActionEvent
			if err := json.Unmarshal(data, &event); err != nil {
				log.Error().Err(err).Msg("failed to unmarshal deferred action event")
				return
			}
			log.Info().
				Int64("action_id", event.ActionID).
				Int64("account_id", event.AccountID).
				Str("action", event.Action).
				Str("message_id", event.MessageID).
				Msg("received deferred action event")

			w := p.getWorker(event.AccountID)
			if w == nil {
				log.Error().
					Int64("account_id", event.AccountID).
					Msg("no worker found for account, cannot execute deferred action")
				return
			}

			if err := w.ExecuteDeferredAction(ctx, &event); err != nil {
				log.Error().Err(err).
					Int64("action_id", event.ActionID).
					Str("action", event.Action).
					Msg("failed to execute deferred action")
			}
		}); err != nil {
			log.Error().Err(err).Msg("failed to subscribe to deferred action events")
		} else {
			log.Info().Msg("subscribed to deferred action events on NATS")
		}
	}

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.stopCh:
				return
			case <-ticker.C:
				if err := p.refreshWorkers(ctx); err != nil {
					log.Error().Err(err).Msg("failed to refresh workers")
				}
			}
		}
	}()

	return nil
}

// Stop shuts down all workers.
func (p *Pool) Stop() {
	close(p.stopCh)
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, w := range p.workers {
		log.Info().Int64("account_id", id).Msg("stopping worker")
		w.Stop()
		delete(p.workers, id)
	}
}

// getWorker returns the worker for a given account ID, or nil if not found.
func (p *Pool) getWorker(accountID int64) *Worker {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.workers[accountID]
}

func (p *Pool) refreshWorkers(ctx context.Context) error {
	accounts, err := p.db.ListActiveAccounts(ctx)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	activeIDs := make(map[int64]bool)
	for _, acc := range accounts {
		activeIDs[acc.ID] = true
		if _, exists := p.workers[acc.ID]; !exists {
			w := NewWorker(acc, p.db, p.engine, p.bus, p.notifier, p.idleTimeout, p.pollInterval)
			p.workers[acc.ID] = w
			go w.Run(ctx)
			log.Info().Int64("account_id", acc.ID).Str("email", acc.Email).Msg("started IMAP worker")
		}
	}

	for id, w := range p.workers {
		if !activeIDs[id] {
			log.Info().Int64("account_id", id).Msg("stopping deactivated worker")
			w.Stop()
			delete(p.workers, id)
		}
	}

	return nil
}

// Worker monitors a single IMAP account.
type Worker struct {
	account      models.Account
	db           *db.DB
	engine       *engine.Engine
	bus          *natsbus.Bus
	notifier     *notifier.Telegram
	idleTimeout  time.Duration
	pollInterval time.Duration
	stopCh       chan struct{}
	client       *imapclient.Client
	clientMu     sync.Mutex

	// IDLE-based move detection
	expungeCh chan uint32    // receives sequence numbers from EXPUNGE notifications
	inboxUIDs []imap.UID    // current INBOX UIDs ordered by sequence number
	uidsMu    sync.Mutex    // protects inboxUIDs
}

// NewWorker creates a new IMAP worker for an account.
func NewWorker(account models.Account, database *db.DB, eng *engine.Engine, bus *natsbus.Bus, telegram *notifier.Telegram, idleTimeout, pollInterval time.Duration) *Worker {
	return &Worker{
		account:      account,
		db:           database,
		engine:       eng,
		bus:          bus,
		notifier:     telegram,
		idleTimeout:  idleTimeout,
		pollInterval: pollInterval,
		stopCh:       make(chan struct{}),
		expungeCh:    make(chan uint32, 64),
	}
}

// Run starts the worker's main loop with IDLE-based monitoring.
func (w *Worker) Run(ctx context.Context) {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("email", w.account.Email).
		Logger()

	logger.Info().Msg("IMAP worker starting")

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("IMAP worker stopping (context cancelled)")
			w.disconnect()
			return
		case <-w.stopCh:
			logger.Info().Msg("IMAP worker stopping (stop signal)")
			w.disconnect()
			return
		default:
		}

		if err := w.poll(ctx); err != nil {
			logger.Error().Err(err).Msg("poll cycle failed")
			w.disconnect()
			// Back off before retrying
			select {
			case <-ctx.Done():
				return
			case <-w.stopCh:
				return
			case <-time.After(w.pollInterval):
			}
			continue
		}

		// Enter IDLE mode to wait for server notifications
		expungedUIDs := w.idle(ctx)

		// Process any messages that were expunged during IDLE
		if len(expungedUIDs) > 0 {
			if err := w.handleExpungedMessages(ctx, expungedUIDs); err != nil {
				logger.Warn().Err(err).Msg("failed to handle expunged messages")
			}
		}
	}
}

// Stop signals the worker to stop.
func (w *Worker) Stop() {
	close(w.stopCh)
}

// connect establishes an IMAP connection and logs in.
// It sets up the UnilateralDataHandler to capture EXPUNGE notifications for move detection.
func (w *Worker) connect() (*imapclient.Client, error) {
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
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("addr", addr).
		Logger()

	logger.Info().Msg("connecting to IMAP server")

	var client *imapclient.Client
	var err error

	opts := &imapclient.Options{
		TLSConfig: &tls.Config{
			ServerName: host,
		},
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Expunge: func(seqNum uint32) {
				logger.Debug().Uint32("seq_num", seqNum).Msg("received EXPUNGE notification")
				// Non-blocking send to avoid blocking the client
				select {
				case w.expungeCh <- seqNum:
				default:
					logger.Warn().Uint32("seq_num", seqNum).Msg("expunge channel full, dropping notification")
				}
			},
			Mailbox: func(data *imapclient.UnilateralDataMailbox) {
				// Log mailbox status changes (EXISTS, etc.) for debugging
				if data.NumMessages != nil {
					logger.Debug().Uint32("num_messages", *data.NumMessages).Msg("mailbox EXISTS update")
				}
			},
		},
	}

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

	logger.Info().Msg("IMAP login successful")
	w.client = client
	return client, nil
}

// disconnect closes the IMAP connection.
func (w *Worker) disconnect() {
	w.clientMu.Lock()
	defer w.clientMu.Unlock()

	if w.client != nil {
		logoutCmd := w.client.Logout()
		_ = logoutCmd.Wait()
		w.client.Close()
		w.client = nil
	}
}

// poll checks for new messages, processes them through the rule engine,
// and records message locations for IDLE-based move detection.
func (w *Worker) poll(ctx context.Context) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("email", w.account.Email).
		Logger()

	client, err := w.connect()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	logger.Debug().Msg("polling for new messages")

	// Select INBOX
	selectCmd := client.Select("INBOX", nil)
	mbox, err := selectCmd.Wait()
	if err != nil {
		return fmt.Errorf("select INBOX: %w", err)
	}

	logger.Debug().Uint32("messages", mbox.NumMessages).Msg("INBOX selected")

	if mbox.NumMessages == 0 {
		// Clear the UID tracking since INBOX is empty
		w.uidsMu.Lock()
		w.inboxUIDs = nil
		w.uidsMu.Unlock()
		if err := w.db.UpdateAccountSyncTime(ctx, w.account.ID); err != nil {
			logger.Warn().Err(err).Msg("failed to update sync time")
		}
		return nil
	}

	// Get ALL messages in INBOX for UID tracking (needed for IDLE move detection)
	allCriteria := &imap.SearchCriteria{}
	allSearchCmd := client.UIDSearch(allCriteria, nil)
	allSearchData, err := allSearchCmd.Wait()
	if err != nil {
		return fmt.Errorf("search all messages: %w", err)
	}
	allUIDs := allSearchData.AllUIDs()

	// Record message locations for all INBOX messages (needed for move detection lookups)
	w.recordMessageLocations(ctx, client, allUIDs)

	// Update the UID sequence list for IDLE expunge tracking
	// UIDs from SEARCH are returned in ascending order which matches sequence number order
	w.uidsMu.Lock()
	w.inboxUIDs = make([]imap.UID, len(allUIDs))
	copy(w.inboxUIDs, allUIDs)
	w.uidsMu.Unlock()

	logger.Debug().Int("tracked_uids", len(allUIDs)).Msg("updated INBOX UID tracking for IDLE")

	// Drain any stale expunge notifications from before this poll
	for {
		select {
		case <-w.expungeCh:
		default:
			goto drained
		}
	}
drained:

	// Search for UNSEEN messages
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

	// Process messages in batches of 50
	batchSize := 50
	for i := 0; i < len(uids); i += batchSize {
		end := i + batchSize
		if end > len(uids) {
			end = len(uids)
		}
		batch := uids[i:end]

		if err := w.processBatch(ctx, client, batch); err != nil {
			logger.Error().Err(err).Int("batch_start", i).Msg("batch processing failed")
		}
	}

	if err := w.db.UpdateAccountSyncTime(ctx, w.account.ID); err != nil {
		logger.Warn().Err(err).Msg("failed to update sync time")
	}

	return nil
}

// recordMessageLocations fetches envelope data for all INBOX UIDs and records their locations.
func (w *Worker) recordMessageLocations(ctx context.Context, client *imapclient.Client, uids []imap.UID) {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Logger()

	if len(uids) == 0 {
		return
	}

	// Fetch in batches to avoid overwhelming the server
	batchSize := 100
	for i := 0; i < len(uids); i += batchSize {
		end := i + batchSize
		if end > len(uids) {
			end = len(uids)
		}
		batch := uids[i:end]

		uidSet := imap.UIDSet{}
		for _, uid := range batch {
			uidSet.AddNum(uid)
		}

		fetchOptions := &imap.FetchOptions{
			Envelope: true,
			UID:      true,
		}

		fetchCmd := client.Fetch(uidSet, fetchOptions)
		for {
			msg := fetchCmd.Next()
			if msg == nil {
				break
			}
			buf, err := msg.Collect()
			if err != nil {
				logger.Warn().Err(err).Msg("failed to collect message for location tracking")
				continue
			}
			if buf.Envelope == nil {
				continue
			}

			var senderAddr string
			if len(buf.Envelope.From) > 0 {
				senderAddr = buf.Envelope.From[0].Addr()
			}

			loc := &models.MessageLocation{
				AccountID:  w.account.ID,
				MessageUID: fmt.Sprintf("%d", buf.UID),
				Folder:     "INBOX",
				MessageID:  buf.Envelope.MessageID,
				Sender:     senderAddr,
				Subject:    buf.Envelope.Subject,
			}
			if err := w.db.UpsertMessageLocation(ctx, loc); err != nil {
				logger.Warn().Err(err).Str("message_id", buf.Envelope.MessageID).Msg("failed to record message location")
			}
		}
		fetchCmd.Close()
	}
}

// idle enters IMAP IDLE mode and waits for EXPUNGE notifications or timeout.
// Returns a list of UIDs that were expunged during the IDLE session.
func (w *Worker) idle(ctx context.Context) []imap.UID {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("email", w.account.Email).
		Logger()

	w.clientMu.Lock()
	client := w.client
	w.clientMu.Unlock()

	if client == nil {
		logger.Warn().Msg("no client available for IDLE")
		return nil
	}

	// Start IDLE command
	idleCmd, err := client.Idle()
	if err != nil {
		logger.Error().Err(err).Msg("failed to start IDLE")
		w.disconnect()
		return nil
	}

	logger.Debug().Dur("timeout", w.idleTimeout).Msg("entering IDLE mode")

	// Wait for expunge notifications, timeout, or stop signal
	var expungedSeqNums []uint32
	timer := time.NewTimer(w.idleTimeout)
	defer timer.Stop()

idle_loop:
	for {
		select {
		case <-ctx.Done():
			break idle_loop
		case <-w.stopCh:
			break idle_loop
		case seqNum := <-w.expungeCh:
			logger.Info().Uint32("seq_num", seqNum).Msg("EXPUNGE received during IDLE")
			expungedSeqNums = append(expungedSeqNums, seqNum)
			// Give a short window to collect multiple rapid expunges
			// (e.g., user moves several messages at once)
			timer.Reset(2 * time.Second)
		case <-timer.C:
			break idle_loop
		}
	}

	// Close IDLE to resume normal command mode
	if err := idleCmd.Close(); err != nil {
		logger.Error().Err(err).Msg("failed to close IDLE")
		w.disconnect()
		return nil
	}

	logger.Debug().Int("expunge_count", len(expungedSeqNums)).Msg("IDLE ended")

	if len(expungedSeqNums) == 0 {
		return nil
	}

	// Map sequence numbers to UIDs
	// EXPUNGE notifications are processed in order: when seqNum N is expunged,
	// all subsequent sequence numbers shift down by 1.
	w.uidsMu.Lock()
	expungedUIDs := w.mapSeqNumsToUIDs(expungedSeqNums)
	w.uidsMu.Unlock()

	return expungedUIDs
}

// mapSeqNumsToUIDs converts a series of EXPUNGE sequence numbers to UIDs.
// Must be called with uidsMu held.
// IMAP EXPUNGE notifications are sequential: after each expunge, remaining
// sequence numbers shift down. We process them in order against our local copy.
func (w *Worker) mapSeqNumsToUIDs(seqNums []uint32) []imap.UID {
	var result []imap.UID

	// Work on a copy so we can mutate it
	uidsCopy := make([]imap.UID, len(w.inboxUIDs))
	copy(uidsCopy, w.inboxUIDs)

	for _, seqNum := range seqNums {
		idx := int(seqNum) - 1 // sequence numbers are 1-based
		if idx < 0 || idx >= len(uidsCopy) {
			log.Warn().
				Uint32("seq_num", seqNum).
				Int("uid_count", len(uidsCopy)).
				Msg("EXPUNGE sequence number out of range")
			continue
		}

		result = append(result, uidsCopy[idx])
		// Remove the expunged entry - subsequent seqNums reference the shifted list
		uidsCopy = append(uidsCopy[:idx], uidsCopy[idx+1:]...)
	}

	// Update the worker's UID list to reflect the expunges
	w.inboxUIDs = uidsCopy

	return result
}

// handleExpungedMessages processes UIDs that were expunged from INBOX during IDLE.
// For each expunged UID, it looks up the message details from message_locations
// and searches other folders to detect where the message was moved.
func (w *Worker) handleExpungedMessages(ctx context.Context, expungedUIDs []imap.UID) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Logger()

	logger.Info().Int("count", len(expungedUIDs)).Msg("processing expunged messages for move detection")

	client, err := w.connect()
	if err != nil {
		return fmt.Errorf("connect for move detection: %w", err)
	}

	for _, uid := range expungedUIDs {
		uidStr := fmt.Sprintf("%d", uid)

		// Look up the message details from our recorded locations
		locs, err := w.db.GetMessageLocations(ctx, w.account.ID, uidStr)
		if err != nil {
			logger.Warn().Err(err).Str("uid", uidStr).Msg("failed to get message location")
			continue
		}

		if len(locs) == 0 {
			logger.Debug().Str("uid", uidStr).Msg("no recorded location for expunged UID, skipping")
			continue
		}

		// Use the first (and typically only) location record
		loc := locs[0]
		if loc.MessageID == "" {
			logger.Debug().Str("uid", uidStr).Msg("no Message-ID for expunged message, skipping")
			continue
		}

		logger.Info().
			Str("uid", uidStr).
			Str("message_id", loc.MessageID).
			Str("sender", loc.Sender).
			Str("subject", loc.Subject).
			Msg("looking for moved message in other folders")

		// Search other folders for this message by Message-ID
		if err := w.findMovedMessageAndLearn(ctx, client, loc); err != nil {
			logger.Warn().Err(err).
				Str("message_id", loc.MessageID).
				Msg("failed to find moved message")
		}
	}

	return nil
}

// findMovedMessageAndLearn searches other folders for a message by Message-ID
// and creates an auto-learned rule if found.
func (w *Worker) findMovedMessageAndLearn(ctx context.Context, client *imapclient.Client, loc models.MessageLocation) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("message_id", loc.MessageID).
		Logger()

	// List all folders
	listCmd := client.List("", "%", nil)
	mailboxes, err := listCmd.Collect()
	if err != nil {
		return fmt.Errorf("list mailboxes: %w", err)
	}

	// Search for the message in each folder (except INBOX)
	for _, mbox := range mailboxes {
		if mbox.Mailbox == "INBOX" {
			continue
		}

		selectCmd := client.Select(mbox.Mailbox, nil)
		if _, err := selectCmd.Wait(); err != nil {
			continue // Skip folders we can't select
		}

		criteria := &imap.SearchCriteria{
			Header: []imap.SearchCriteriaHeaderField{
				{Key: "Message-ID", Value: loc.MessageID},
			},
		}
		searchCmd := client.UIDSearch(criteria, nil)
		searchData, err := searchCmd.Wait()
		if err != nil {
			continue
		}

		uids := searchData.AllUIDs()
		if len(uids) > 0 {
			// Found the message in this folder
			logger.Info().Str("folder", mbox.Mailbox).Msg("found moved message via IDLE detection")

			// Record the move
			move := &models.DetectedMove{
				AccountID:  w.account.ID,
				MessageUID: fmt.Sprintf("%d", uids[0]),
				MessageID:  loc.MessageID,
				Sender:     loc.Sender,
				Subject:    loc.Subject,
				FromFolder: "INBOX",
				ToFolder:   mbox.Mailbox,
			}
			if err := w.db.RecordDetectedMove(ctx, move); err != nil {
				logger.Warn().Err(err).Msg("failed to record detected move")
				return nil
			}

			// Generate and create a rule based on heuristics
			if err := w.generateAndCreateRuleFromMove(ctx, loc.Sender, loc.Subject, mbox.Mailbox); err != nil {
				logger.Warn().Err(err).Msg("failed to generate rule from move")
			}

			// Re-select INBOX for subsequent operations
			reselectCmd := client.Select("INBOX", nil)
			if _, err := reselectCmd.Wait(); err != nil {
				logger.Warn().Err(err).Msg("failed to re-select INBOX after move detection")
			}

			return nil
		}
	}

	// Message not found in any folder - might have been deleted
	logger.Debug().Msg("expunged message not found in other folders (likely deleted)")

	// Re-select INBOX for subsequent operations
	reselectCmd := client.Select("INBOX", nil)
	if _, err := reselectCmd.Wait(); err != nil {
		logger.Warn().Err(err).Msg("failed to re-select INBOX after move search")
	}

	return nil
}

// processBatch fetches and processes a batch of messages by UID.
func (w *Worker) processBatch(ctx context.Context, client *imapclient.Client, uids []imap.UID) error {
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

		email := w.bufferToEmailContext(buf)
		if email == nil {
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

		// Fetch image attachment bodies for vision analysis if images were detected
		if email.HasImages && buf.BodyStructure != nil {
			imageParts := findImageParts(buf.BodyStructure, nil)
			if len(imageParts) > 0 {
				log.Info().
					Str("message_id", email.MessageID).
					Int("image_count", len(imageParts)).
					Msg("fetching image attachments for vision analysis")
				email.ImageAttachments = w.fetchImageAttachments(client, buf.UID, imageParts)
			}
		}

		// Process through rule engine
		if err := w.processMessage(ctx, client, email, buf.UID); err != nil {
			log.Error().Err(err).Str("message_id", email.MessageID).Msg("failed to process message")
		}
	}

	return nil
}

// bufferToEmailContext converts a FetchMessageBuffer to an EmailContext.
func (w *Worker) bufferToEmailContext(buf *imapclient.FetchMessageBuffer) *models.EmailContext {
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

	// Extract headers from body sections
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

	// Extract attachment info from body structure
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

	return &models.EmailContext{
		MessageID:       env.MessageID,
		Subject:         strings.TrimSpace(env.Subject),
		SenderName:      senderName,
		SenderAddress:   senderAddr,
		Recipients:      recipients,
		Date:            msgDate,
		AgeSeconds:      ageSeconds,
		BodyPreview:     bodyPreview,
		HasAttachments:  hasAttachments,
		AttachmentNames: attachmentNames,
		AttachmentTypes: attachmentTypes,
		Headers:         headers,
		Folder:          "INBOX",
		AccountID:       w.account.ID,
		HasImages:       hasImageAttachments(attachmentTypes),
	}
}

// parseHeaders extracts key headers from raw header bytes.
func parseHeaders(raw []byte) map[string]string {
	headers := make(map[string]string)

	entity, err := gomessage.Read(strings.NewReader(string(raw) + "\r\n\r\n"))
	if err != nil {
		// Fallback: simple line-based parsing
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

// parseHeadersSimple is a fallback header parser.
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

// imagePartInfo holds the MIME part path and metadata for an image attachment.
type imagePartInfo struct {
	Part      []int  // MIME part path, e.g. [1, 2] for part 1.2
	MediaType string // e.g. "image/jpeg"
	Filename  string
	Size      uint32 // estimated size from body structure
}

// extractAttachmentInfo walks the body structure to find attachments.
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

// maxImageSize is the maximum size per image attachment to fetch (5 MB).
const maxImageSize = 5 * 1024 * 1024

// maxImageAttachments is the maximum number of image attachments to fetch per email.
const maxImageAttachments = 4

// findImageParts walks the body structure and returns info about image parts
// suitable for vision analysis. It tracks the MIME part path for fetching.
func findImageParts(bs imap.BodyStructure, path []int) []imagePartInfo {
	var parts []imagePartInfo

	switch s := bs.(type) {
	case *imap.BodyStructureSinglePart:
		mt := strings.ToLower(s.MediaType())
		if strings.HasPrefix(mt, "image/") {
			// Only include common image types that Anthropic supports
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
			childPath := append(append([]int{}, path...), i+1) // MIME parts are 1-indexed
			parts = append(parts, findImageParts(child, childPath)...)
		}
	}

	return parts
}

// fetchImageAttachments fetches the actual image data for the given image parts
// from IMAP and returns them as base64-encoded ImageAttachment structs.
func (w *Worker) fetchImageAttachments(client *imapclient.Client, uid imap.UID, imageParts []imagePartInfo) []models.ImageAttachment {
	if len(imageParts) == 0 {
		return nil
	}

	// Limit the number of images we fetch
	if len(imageParts) > maxImageAttachments {
		imageParts = imageParts[:maxImageAttachments]
	}

	// Build fetch options for each image part
	var bodySections []*imap.FetchItemBodySection
	for _, ip := range imageParts {
		bodySections = append(bodySections, &imap.FetchItemBodySection{
			Part: ip.Part,
			Peek: true, // Don't mark as seen
		})
	}

	uidSet := imap.UIDSetNum(uid)
	fetchOptions := &imap.FetchOptions{
		UID:         true,
		BodySection: bodySections,
	}

	fetchCmd := client.Fetch(uidSet, fetchOptions)
	defer fetchCmd.Close()

	var attachments []models.ImageAttachment

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

			// Match this section back to our image part info
			for _, ip := range imageParts {
				if partsEqual(section.Section.Part, ip.Part) {
					// The data from IMAP may already be decoded or may need base64 encoding
					encoded := base64.StdEncoding.EncodeToString(section.Bytes)
					attachments = append(attachments, models.ImageAttachment{
						Filename:  ip.Filename,
						MediaType: ip.MediaType,
						Data:      encoded,
					})
					log.Debug().
						Str("filename", ip.Filename).
						Str("media_type", ip.MediaType).
						Int("size", len(section.Bytes)).
						Msg("fetched image attachment for vision analysis")
					break
				}
			}
		}
	}

	return attachments
}

// partsEqual compares two MIME part paths for equality.
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

// hasImageAttachments checks if any attachment types are images.
func hasImageAttachments(attachmentTypes []string) bool {
	for _, ct := range attachmentTypes {
		lower := strings.ToLower(ct)
		if strings.HasPrefix(lower, "image/") {
			return true
		}
	}
	return false
}

// processMessage evaluates a single message against all active rules.
func (w *Worker) processMessage(ctx context.Context, client *imapclient.Client, email *models.EmailContext, uid imap.UID) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("message_id", email.MessageID).
		Str("subject", email.Subject).
		Logger()

	rules, err := w.db.ListActiveRules(ctx, w.account.TenantID)
	if err != nil {
		return fmt.Errorf("list rules: %w", err)
	}

	result, matchedRule, err := w.engine.EvaluateAll(rules, email)
	if err != nil {
		return fmt.Errorf("evaluate rules: %w", err)
	}

	if result.Action == "skip" {
		logger.Debug().Msg("no rules matched")
		return nil
	}

	logger.Info().
		Str("action", result.Action).
		Str("target", result.Target).
		Str("rule", matchedRule.Name).
		Msg("rule matched")

	// Log execution
	execLog := &models.RuleExecutionLog{
		RuleID:    matchedRule.ID,
		AccountID: w.account.ID,
		MessageID: email.MessageID,
		Subject:   email.Subject,
		Sender:    email.SenderAddress,
		Action:    result.Action,
		Target:    result.Target,
		Success:   true,
	}
	if err := w.db.LogExecution(ctx, execLog); err != nil {
		logger.Warn().Err(err).Msg("failed to log execution")
	}

	// Publish to NATS
	if w.bus != nil {
		_ = w.bus.Publish(natsbus.SubjectRuleResult, &natsbus.RuleResultEvent{
			AccountID: w.account.ID,
			MessageID: email.MessageID,
			RuleID:    matchedRule.ID,
			RuleName:  matchedRule.Name,
			Action:    result.Action,
			Target:    result.Target,
			Subject:   email.Subject,
			Sender:    email.SenderAddress,
		})
	}

	// Handle deferred actions
	if result.Action == "defer" && result.Delay > 0 {
		deferred := &models.DeferredAction{
			RuleID:    matchedRule.ID,
			AccountID: w.account.ID,
			MessageID: email.MessageID,
			Action:    result.Target,
			Target:    result.Target,
			ExecuteAt: time.Now().Add(time.Duration(result.Delay) * time.Second),
		}
		if err := w.db.CreateDeferredAction(ctx, deferred); err != nil {
			logger.Error().Err(err).Msg("failed to create deferred action")
		}
		return nil
	}

	// Execute immediate actions
	uidSet := imap.UIDSetNum(uid)
	var actionErr error

	switch result.Action {
	case "delete":
		actionErr = w.executeDelete(client, uidSet)
	case "move":
		actionErr = w.executeMove(client, uidSet, result.Target)
	case "archive":
		actionErr = w.executeMove(client, uidSet, "Archive")
	case "flag":
		actionErr = w.executeFlag(client, uidSet, result.Target)
	case "notify":
		actionErr = w.executeNotify(ctx, email, matchedRule.Name, result.Target)
	case "keep":
		// Do nothing
	}

	if actionErr != nil {
		execLog.Success = false
		execLog.Error = actionErr.Error()
		_ = w.db.LogExecution(ctx, execLog)
		return actionErr
	}

	// Mark as processed
	_ = w.db.MarkMessageProcessed(ctx, w.account.ID, fmt.Sprintf("%d", uid), matchedRule.ID, result.Action)

	return nil
}

// executeDelete adds the \Deleted flag and expunges.
func (w *Worker) executeDelete(client *imapclient.Client, uidSet imap.UIDSet) error {
	storeCmd := client.Store(uidSet, &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagDeleted},
	}, nil)
	if err := storeCmd.Close(); err != nil {
		return fmt.Errorf("store deleted flag: %w", err)
	}

	expungeCmd := client.UIDExpunge(uidSet)
	if err := expungeCmd.Close(); err != nil {
		// Fallback to regular expunge
		expungeCmd2 := client.Expunge()
		if err2 := expungeCmd2.Close(); err2 != nil {
			return fmt.Errorf("expunge: %w", err2)
		}
	}

	log.Info().Str("uids", uidSet.String()).Msg("deleted message(s)")
	return nil
}

// executeMove moves messages to a target folder, creating it if needed.
func (w *Worker) executeMove(client *imapclient.Client, uidSet imap.UIDSet, folder string) error {
	// Try to create the folder (ignore error if it already exists)
	createCmd := client.Create(folder, nil)
	_ = createCmd.Wait() // Ignore "already exists" errors

	moveCmd := client.Move(uidSet, folder)
	if _, err := moveCmd.Wait(); err != nil {
		return fmt.Errorf("move to %s: %w", folder, err)
	}

	log.Info().Str("uids", uidSet.String()).Str("folder", folder).Msg("moved message(s)")
	return nil
}

func (w *Worker) executeNotify(ctx context.Context, email *models.EmailContext, ruleName, message string) error {
	if w.notifier == nil || !w.notifier.Enabled() {
		return nil
	}

	text := message
	if text == "" {
		text = fmt.Sprintf("📧 New email from %s: %s", email.SenderAddress, email.Subject)
	}

	text = strings.ReplaceAll(text, "{sender}", email.SenderAddress)
	text = strings.ReplaceAll(text, "{subject}", email.Subject)
	text = strings.ReplaceAll(text, "{sender_name}", email.SenderName)

	return w.notifier.SendMessage(ctx, text)
}

// executeFlag sets an IMAP flag on messages.
func (w *Worker) executeFlag(client *imapclient.Client, uidSet imap.UIDSet, flagName string) error {
	imapFlag := flagNameToIMAP(flagName)

	storeCmd := client.Store(uidSet, &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imapFlag},
	}, nil)
	if err := storeCmd.Close(); err != nil {
		return fmt.Errorf("store flag %s: %w", flagName, err)
	}

	log.Info().Str("uids", uidSet.String()).Str("flag", string(imapFlag)).Msg("flagged message(s)")
	return nil
}

// executeFlagByMessageID searches for a message by Message-ID and sets a flag on it.
func (w *Worker) executeFlagByMessageID(client *imapclient.Client, messageID, flagName string) error {
	criteria := &imap.SearchCriteria{
		Header: []imap.SearchCriteriaHeaderField{
			{Key: "Message-ID", Value: messageID},
		},
	}
	searchCmd := client.UIDSearch(criteria, nil)
	searchData, err := searchCmd.Wait()
	if err != nil {
		return fmt.Errorf("search for message %s: %w", messageID, err)
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		log.Warn().Str("message_id", messageID).Msg("message not found for flag action")
		return nil
	}

	uidSet := imap.UIDSetNum(uids[0])
	return w.executeFlag(client, uidSet, flagName)
}

// flagNameToIMAP converts a human-friendly flag name to an IMAP flag.
func flagNameToIMAP(name string) imap.Flag {
	switch strings.ToLower(name) {
	case "flagged", "starred":
		return imap.FlagFlagged
	case "seen", "read":
		return imap.FlagSeen
	case "answered", "replied":
		return imap.FlagAnswered
	case "draft":
		return imap.FlagDraft
	case "deleted":
		return imap.FlagDeleted
	case "junk", "spam":
		return "$Junk"
	case "notjunk":
		return "$NotJunk"
	default:
		return imap.Flag(name)
	}
}

// ExecuteDeferredAction executes a deferred action event by finding the message
// in IMAP by its Message-ID header and performing the requested action.
func (w *Worker) ExecuteDeferredAction(ctx context.Context, event *natsbus.DeferredActionEvent) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Int64("action_id", event.ActionID).
		Str("message_id", event.MessageID).
		Str("action", event.Action).
		Logger()

	logger.Info().Msg("executing deferred action")

	client, err := w.connect()
	if err != nil {
		return fmt.Errorf("connect for deferred action: %w", err)
	}

	// Select INBOX to search for the message
	selectCmd := client.Select("INBOX", nil)
	if _, err := selectCmd.Wait(); err != nil {
		return fmt.Errorf("select INBOX for deferred action: %w", err)
	}

	// Search for the message by Message-ID header
	criteria := &imap.SearchCriteria{
		Header: []imap.SearchCriteriaHeaderField{
			{Key: "Message-ID", Value: event.MessageID},
		},
	}
	searchCmd := client.UIDSearch(criteria, nil)
	searchData, err := searchCmd.Wait()
	if err != nil {
		return fmt.Errorf("search for message %s: %w", event.MessageID, err)
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		logger.Warn().Msg("message not found in INBOX for deferred action (may have been moved or deleted)")
		return nil
	}

	uidSet := imap.UIDSetNum(uids[0])

	switch event.Action {
	case "delete":
		if err := w.executeDelete(client, uidSet); err != nil {
			return fmt.Errorf("deferred delete: %w", err)
		}
		logger.Info().Msg("deferred delete executed")
	case "move":
		if err := w.executeMove(client, uidSet, event.Target); err != nil {
			return fmt.Errorf("deferred move to %s: %w", event.Target, err)
		}
		logger.Info().Str("target", event.Target).Msg("deferred move executed")
	case "archive":
		if err := w.executeMove(client, uidSet, "Archive"); err != nil {
			return fmt.Errorf("deferred archive: %w", err)
		}
		logger.Info().Msg("deferred archive executed")
	case "notify":
		if err := w.executeNotify(ctx, &models.EmailContext{
			MessageID:     event.MessageID,
			SenderAddress: "deferred",
			Subject:       "Deferred action",
		}, "deferred", event.Target); err != nil {
			return fmt.Errorf("deferred notify: %w", err)
		}
		logger.Info().Msg("deferred notify executed")
	case "flag":
		if err := w.executeFlagByMessageID(client, event.MessageID, event.Target); err != nil {
			return fmt.Errorf("deferred flag: %w", err)
		}
		logger.Info().Str("flag", event.Target).Msg("deferred flag executed")
	default:
		logger.Warn().Str("action", event.Action).Msg("unknown deferred action type")
	}

	return nil
}



// generateAndCreateRuleFromMove creates a rule based on the move heuristics.
func (w *Worker) generateAndCreateRuleFromMove(ctx context.Context, sender, subject, targetFolder string) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("sender", sender).
		Str("target_folder", targetFolder).
		Logger()

	var ruleName string
	var luaCode string

	// Determine if sender is generic or complicated
	if isGenericSender(sender) {
		// Generic sender - create rule based on sender address
		ruleName = fmt.Sprintf("Auto-learned: %s → %s", sender, targetFolder)
		luaCode = fmt.Sprintf(`if contains(email.sender, "%s") then move("%s") end`, sender, targetFolder)
	} else {
		// Complicated sender - extract keywords from subject
		keywords := extractSubjectKeywords(subject)
		if len(keywords) == 0 {
			// Fallback to sender domain if no keywords
			domain := extractDomain(sender)
			ruleName = fmt.Sprintf("Auto-learned: %s → %s", domain, targetFolder)
			luaCode = fmt.Sprintf(`if contains(email.sender_domain, "%s") then move("%s") end`, domain, targetFolder)
		} else {
			// Create rule based on sender domain + subject keywords
			domain := extractDomain(sender)
			ruleName = fmt.Sprintf("Auto-learned: %s + keywords → %s", domain, targetFolder)

			// Build Lua code with domain and keywords
			luaCode = fmt.Sprintf(`if contains(email.sender_domain, "%s")`, domain)
			for _, kw := range keywords {
				luaCode += fmt.Sprintf(` and contains(email.subject, "%s")`, kw)
			}
			luaCode += fmt.Sprintf(` then move("%s") end`, targetFolder)
		}
	}

	// Create the rule in the database
	rule := &models.Rule{
		TenantID:    w.account.TenantID,
		Name:        ruleName,
		LuaCode:     luaCode,
		Active:      false, // Auto-learned rules start as inactive
		Source:      "auto-learned",
		Approved:    false,
	}

	if err := w.db.CreateRule(ctx, rule); err != nil {
		return fmt.Errorf("create rule: %w", err)
	}

	logger.Info().
		Int64("rule_id", rule.ID).
		Str("rule_name", ruleName).
		Msg("created auto-learned rule")

	return nil
}

// isGenericSender checks if a sender address appears to be generic (noreply@, billing@, etc.).
func isGenericSender(sender string) bool {
	genericPrefixes := []string{
		"noreply@",
		"no-reply@",
		"billing@",
		"support@",
		"info@",
		"notifications@",
		"alerts@",
		"admin@",
		"contact@",
		"hello@",
		"team@",
		"service@",
		"automated@",
	}

	lowerSender := strings.ToLower(sender)
	for _, prefix := range genericPrefixes {
		if strings.HasPrefix(lowerSender, prefix) {
			return true
		}
	}
	return false
}

// extractDomain extracts the domain from an email address.
func extractDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) == 2 {
		return parts[1]
	}
	return email
}

// extractSubjectKeywords extracts meaningful keywords from a subject line.
func extractSubjectKeywords(subject string) []string {
	// Simple keyword extraction - split on common delimiters and filter
	stopwords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
		"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
		"is": true, "are": true, "was": true, "were": true, "be": true,
		"your": true, "my": true, "our": true, "their": true,
	}

	// Remove common subject prefixes
	subject = strings.TrimPrefix(subject, "Re: ")
	subject = strings.TrimPrefix(subject, "Fwd: ")
	subject = strings.TrimPrefix(subject, "[")

	// Extract bracketed tags like [MARKETING], [ANNOUNCE], etc.
	var keywords []string
	if idx := strings.Index(subject, "["); idx >= 0 {
		if endIdx := strings.Index(subject[idx:], "]"); endIdx >= 0 {
			tag := subject[idx+1 : idx+endIdx]
			if len(tag) > 0 && len(tag) < 50 {
				keywords = append(keywords, tag)
			}
		}
	}

	// Split on common delimiters and extract meaningful words
	words := strings.FieldsFunc(subject, func(r rune) bool {
		return r == ' ' || r == '-' || r == ':' || r == '|'
	})

	for _, word := range words {
		word = strings.ToLower(word)
		word = strings.Trim(word, ".,!?;()[]{}")

		// Skip stopwords and very short words
		if len(word) > 3 && !stopwords[word] {
			keywords = append(keywords, word)
			if len(keywords) >= 3 { // Limit to 3 keywords
				break
			}
		}
	}

	return keywords
}

// Ensure mail and io imports are used (for header parsing)
var _ = mail.Address{}
var _ = io.EOF
