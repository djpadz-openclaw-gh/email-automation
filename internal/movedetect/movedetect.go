// Package movedetect monitors IMAP folders for manual message moves
// and infers rules based on the moved messages.
package movedetect

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/models"
	"github.com/djpadz/email-automation/internal/notifier"
)

// skipFolders are folders we don't track for move detection.
var skipFolders = map[string]bool{
	"Drafts":         true,
	"Sent":           true,
	"Sent Messages":  true,
	"Sent Items":     true,
	"[Gmail]/Sent Mail": true,
	"[Gmail]/Drafts":    true,
}

// trashOrJunkFolders are folders that should NOT trigger rule creation
// when a message is moved to them.
var trashOrJunkFolders = map[string]bool{
	"trash":              true,
	"deleted messages":   true,
	"deleted items":      true,
	"[gmail]/trash":      true,
	"[gmail]/all mail":   true,
	"[gmail]/spam":       true,
	"junk":              true,
	"spam":              true,
	"junk e-mail":       true,
	"bulk mail":         true,
	"archive":           true,
}

// isTrashOrJunkFolder returns true if the folder name matches a known
// trash/junk/spam/archive folder that should not trigger rule creation.
func isTrashOrJunkFolder(folder string) bool {
	lower := strings.ToLower(folder)
	if trashOrJunkFolders[lower] {
		return true
	}
	// Also check prefixes for Gmail-style folders
	if strings.HasPrefix(lower, "[gmail]/trash") {
		return true
	}
	if strings.HasPrefix(lower, "[gmail]/spam") {
		return true
	}
	return false
}

// Detector monitors IMAP accounts for message moves between folders.
type Detector struct {
	db       *db.DB
	notifier *notifier.Telegram

	pollInterval time.Duration
	workers      map[int64]*moveWorker
	mu           sync.RWMutex
	stopCh       chan struct{}
}

// New creates a new move detector.
func New(database *db.DB, telegram *notifier.Telegram, pollInterval time.Duration) *Detector {
	return &Detector{
		db:           database,
		notifier:     telegram,
		pollInterval: pollInterval,
		workers:      make(map[int64]*moveWorker),
		stopCh:       make(chan struct{}),
	}
}

// Start begins the move detector, spawning workers for all active accounts.
func (d *Detector) Start(ctx context.Context) error {
	log.Info().Msg("starting move detector")

	if err := d.refreshWorkers(ctx); err != nil {
		return fmt.Errorf("initial worker refresh: %w", err)
	}

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-d.stopCh:
				return
			case <-ticker.C:
				if err := d.refreshWorkers(ctx); err != nil {
					log.Error().Err(err).Msg("failed to refresh move detector workers")
				}
			}
		}
	}()

	return nil
}

// Stop shuts down all workers.
func (d *Detector) Stop() {
	close(d.stopCh)
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, w := range d.workers {
		w.stop()
		delete(d.workers, id)
	}
}

func (d *Detector) refreshWorkers(ctx context.Context) error {
	accounts, err := d.db.ListActiveAccounts(ctx)
	if err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	activeIDs := make(map[int64]bool)
	for _, acc := range accounts {
		activeIDs[acc.ID] = true
		if _, exists := d.workers[acc.ID]; !exists {
			w := newMoveWorker(acc, d.db, d.notifier, d.pollInterval)
			d.workers[acc.ID] = w
			go w.run(ctx)
			log.Info().Int64("account_id", acc.ID).Str("email", acc.Email).Msg("started move detector worker")
		}
	}

	for id, w := range d.workers {
		if !activeIDs[id] {
			w.stop()
			delete(d.workers, id)
		}
	}

	return nil
}

// moveWorker monitors a single IMAP account for message moves.
type moveWorker struct {
	account      models.Account
	db           *db.DB
	notifier     *notifier.Telegram
	pollInterval time.Duration
	stopCh       chan struct{}

	// inboxUIDs tracks UIDs currently in INBOX with their metadata
	inboxUIDs map[imap.UID]*messageInfo
	mu        sync.Mutex

	// initialized tracks whether we've done the first scan
	initialized bool
}

type messageInfo struct {
	UID       imap.UID
	MessageID string
	Sender    string
	Subject   string
}

func newMoveWorker(account models.Account, database *db.DB, telegram *notifier.Telegram, pollInterval time.Duration) *moveWorker {
	return &moveWorker{
		account:      account,
		db:           database,
		notifier:     telegram,
		pollInterval: pollInterval,
		stopCh:       make(chan struct{}),
		inboxUIDs:    make(map[imap.UID]*messageInfo),
	}
}

func (w *moveWorker) run(ctx context.Context) {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("email", w.account.Email).
		Logger()

	logger.Info().Msg("move detector worker starting")

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		default:
		}

		if err := w.scan(ctx); err != nil {
			logger.Error().Err(err).Msg("move detection scan failed")
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

func (w *moveWorker) stop() {
	close(w.stopCh)
}

// scan connects to IMAP, snapshots INBOX UIDs, and detects moves.
func (w *moveWorker) scan(ctx context.Context) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("email", w.account.Email).
		Logger()

	client, err := w.connect()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() {
		logoutCmd := client.Logout()
		_ = logoutCmd.Wait()
		client.Close()
	}()

	// Get current INBOX UIDs with metadata
	currentInbox, err := w.fetchFolderUIDs(client, "INBOX", true)
	if err != nil {
		return fmt.Errorf("fetch INBOX UIDs: %w", err)
	}

	if !w.initialized {
		// First scan: just record the state, don't detect moves
		w.mu.Lock()
		w.inboxUIDs = currentInbox
		w.initialized = true
		w.mu.Unlock()
		logger.Info().Int("inbox_count", len(currentInbox)).Msg("move detector initialized with INBOX snapshot")
		return nil
	}

	// Find UIDs that disappeared from INBOX
	w.mu.Lock()
	previousInbox := w.inboxUIDs
	w.inboxUIDs = currentInbox
	w.mu.Unlock()

	var missingUIDs []imap.UID
	missingMessages := make(map[imap.UID]*messageInfo)

	for uid, info := range previousInbox {
		if _, exists := currentInbox[uid]; !exists {
			missingUIDs = append(missingUIDs, uid)
			missingMessages[uid] = info
		}
	}

	if len(missingUIDs) == 0 {
		return nil
	}

	logger.Info().Int("missing_count", len(missingUIDs)).Msg("detected messages missing from INBOX")

	// List all folders to search for the moved messages
	folders, err := w.listFolders(client)
	if err != nil {
		return fmt.Errorf("list folders: %w", err)
	}

	// Search each folder for the missing UIDs (by Message-ID)
	for _, uid := range missingUIDs {
		info := missingMessages[uid]
		if info == nil || info.MessageID == "" {
			continue
		}

		destFolder := w.findMessageInFolders(client, info.MessageID, folders)
		if destFolder == "" {
			// Message was deleted, not moved
			logger.Debug().
				Str("message_id", info.MessageID).
				Msg("message disappeared from INBOX (likely deleted)")
			continue
		}

		// Skip rule creation if destination is a trash/junk/spam folder
		if isTrashOrJunkFolder(destFolder) {
			logger.Info().
				Str("message_id", info.MessageID).
				Str("destination", destFolder).
				Msg("message moved to trash/junk folder, skipping rule creation")
			continue
		}

		logger.Info().
			Str("message_id", info.MessageID).
			Str("subject", info.Subject).
			Str("sender", info.Sender).
			Str("destination", destFolder).
			Msg("detected move: INBOX → " + destFolder)

		// Record the move
		move := &models.DetectedMove{
			AccountID:  w.account.ID,
			MessageUID: fmt.Sprintf("%d", uid),
			MessageID:  info.MessageID,
			Sender:     info.Sender,
			Subject:    info.Subject,
			FromFolder: "INBOX",
			ToFolder:   destFolder,
		}

		if err := w.db.RecordDetectedMove(ctx, move); err != nil {
			logger.Error().Err(err).Msg("failed to record detected move")
			continue
		}

		// Infer and create a rule
		rule, err := w.inferAndCreateRule(ctx, move)
		if err != nil {
			logger.Debug().Err(err).Msg("skipped rule creation for move")
			continue
		}

		// Send Telegram notification
		w.notifyMove(ctx, move, rule)
	}

	return nil
}

// fetchFolderUIDs fetches all UIDs in a folder. If withMetadata is true,
// also fetches envelope data for each message.
func (w *moveWorker) fetchFolderUIDs(client *imapclient.Client, folder string, withMetadata bool) (map[imap.UID]*messageInfo, error) {
	selectCmd := client.Select(folder, nil)
	mbox, err := selectCmd.Wait()
	if err != nil {
		return nil, fmt.Errorf("select %s: %w", folder, err)
	}

	result := make(map[imap.UID]*messageInfo)

	if mbox.NumMessages == 0 {
		return result, nil
	}

	seqSet := imap.SeqSet{}
	seqSet.AddRange(1, mbox.NumMessages)

	fetchOptions := &imap.FetchOptions{
		UID: true,
	}
	if withMetadata {
		fetchOptions.Envelope = true
	}

	fetchCmd := client.Fetch(seqSet, fetchOptions)
	defer fetchCmd.Close()

	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}

		buf, err := msg.Collect()
		if err != nil {
			continue
		}

		info := &messageInfo{UID: buf.UID}

		if withMetadata && buf.Envelope != nil {
			info.MessageID = buf.Envelope.MessageID
			info.Subject = buf.Envelope.Subject
			if len(buf.Envelope.From) > 0 {
				info.Sender = buf.Envelope.From[0].Addr()
			}
		}

		result[buf.UID] = info
	}

	return result, nil
}

// listFolders returns all mailbox names, excluding skip folders.
func (w *moveWorker) listFolders(client *imapclient.Client) ([]string, error) {
	listCmd := client.List("", "*", nil)

	var folders []string
	for {
		data := listCmd.Next()
		if data == nil {
			break
		}

		name := data.Mailbox
		if name == "INBOX" {
			continue
		}
		if skipFolders[name] {
			continue
		}
		// Skip \Noselect mailboxes
		noSelect := false
		for _, attr := range data.Attrs {
			if attr == imap.MailboxAttrNoSelect {
				noSelect = true
				break
			}
		}
		if noSelect {
			continue
		}
		folders = append(folders, name)
	}

	if err := listCmd.Close(); err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}

	return folders, nil
}

// findMessageInFolders searches for a message by Message-ID in the given folders.
// Returns the folder name where found, or empty string if not found.
func (w *moveWorker) findMessageInFolders(client *imapclient.Client, messageID string, folders []string) string {
	for _, folder := range folders {
		selectCmd := client.Select(folder, nil)
		mbox, err := selectCmd.Wait()
		if err != nil {
			continue
		}
		if mbox.NumMessages == 0 {
			continue
		}

		criteria := &imap.SearchCriteria{
			Header: []imap.SearchCriteriaHeaderField{
				{Key: "Message-ID", Value: messageID},
			},
		}

		searchCmd := client.UIDSearch(criteria, nil)
		searchData, err := searchCmd.Wait()
		if err != nil {
			continue
		}

		uids := searchData.AllUIDs()
		if len(uids) > 0 {
			return folder
		}
	}

	return ""
}

// inferAndCreateRule analyzes the moved message and creates a rule.
func (w *moveWorker) inferAndCreateRule(ctx context.Context, move *models.DetectedMove) (*models.Rule, error) {
	// Check if this move was performed by the rules engine (avoid self-detection)
	processed, err := w.db.IsMessageProcessed(ctx, w.account.ID, move.MessageUID)
	if err != nil {
		log.Warn().Err(err).Msg("failed to check if message was processed by rules engine")
	}
	if processed {
		log.Debug().
			Str("message_id", move.MessageID).
			Msg("skipping rule creation: message was moved by rules engine")
		return nil, fmt.Errorf("message was moved by rules engine, not manually")
	}

	// Check if a similar rule already exists (same sender → same folder)
	if w.hasSimilarRule(ctx, move) {
		log.Debug().
			Str("sender", move.Sender).
			Str("folder", move.ToFolder).
			Msg("skipping rule creation: similar rule already exists")
		return nil, fmt.Errorf("similar rule already exists")
	}

	luaCode := InferRule(move.Sender, move.Subject, move.ToFolder)

	rule := &models.Rule{
		TenantID:    w.account.TenantID,
		Name:        fmt.Sprintf("Auto: %s → %s", truncate(move.Sender, 30), move.ToFolder),
		Description: fmt.Sprintf("Auto-detected from move: INBOX → %s (sender: %s, subject: %s)", move.ToFolder, move.Sender, truncate(move.Subject, 50)),
		LuaCode:     luaCode,
		Priority:    1000, // Low priority so manual rules take precedence
		Active:      true,
		Source:      "auto-learned",
		Approved:    true, // Auto-approve since user explicitly moved the message
	}

	if err := w.db.CreateRule(ctx, rule); err != nil {
		return nil, fmt.Errorf("create rule: %w", err)
	}

	log.Info().
		Int64("rule_id", rule.ID).
		Str("name", rule.Name).
		Str("lua_code", luaCode).
		Msg("created auto-learned rule from move detection")

	return rule, nil
}

// hasSimilarRule checks if a rule already exists that matches the same sender to the same folder.
func (w *moveWorker) hasSimilarRule(ctx context.Context, move *models.DetectedMove) bool {
	rules, err := w.db.ListRules(ctx, w.account.TenantID)
	if err != nil {
		return false
	}

	// Check if any existing rule already handles this sender → folder combination
	for _, rule := range rules {
		// Simple heuristic: check if the rule's Lua code contains both the sender and the destination folder
		if !isComplexSender(move.Sender) {
			if strings.Contains(rule.LuaCode, move.Sender) && strings.Contains(rule.LuaCode, move.ToFolder) {
				return true
			}
		}
	}

	return false
}

// notifyMove sends a Telegram notification about the detected move and created rule.
func (w *moveWorker) notifyMove(ctx context.Context, move *models.DetectedMove, rule *models.Rule) {
	if w.notifier == nil || !w.notifier.Enabled() {
		return
	}

	text := fmt.Sprintf(
		"🔄 <b>Move Detected</b>\n\n"+
			"<b>From:</b> INBOX → %s\n"+
			"<b>Sender:</b> %s\n"+
			"<b>Subject:</b> %s\n\n"+
			"<b>Created Rule:</b> %s\n"+
			"<pre>%s</pre>",
		move.ToFolder,
		escapeHTML(move.Sender),
		escapeHTML(truncate(move.Subject, 60)),
		escapeHTML(rule.Name),
		escapeHTML(rule.LuaCode),
	)

	if err := w.notifier.SendMessage(ctx, text); err != nil {
		log.Error().Err(err).Msg("failed to send move detection notification")
	}
}

func (w *moveWorker) connect() (*imapclient.Client, error) {
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

	return client, nil
}

// --- Helpers ---

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
