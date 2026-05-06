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
	"github.com/djpadz/email-automation/internal/oauth2"
)

// skipFolderAttrs are IMAP special-use attributes that indicate system folders
// where moves should NOT trigger rule creation.
var skipFolderAttrs = map[imap.MailboxAttr]bool{
	imap.MailboxAttrTrash:   true, // \Trash
	imap.MailboxAttrJunk:    true, // \Junk (spam)
	imap.MailboxAttrAll:     true, // \All (Gmail's All Mail)
	imap.MailboxAttrArchive: true, // \Archive
}

// sentOrDraftsAttrs are IMAP attributes for folders we skip during listing.
var sentOrDraftsAttrs = map[imap.MailboxAttr]bool{
	imap.MailboxAttrSent:   true,
	imap.MailboxAttrDrafts: true,
}

// sentOrDraftsNames is a fallback list of exact folder names (case-insensitive)
// for Sent/Drafts folders when the server doesn't report attributes.
var sentOrDraftsNames = map[string]bool{
	"drafts":           true,
	"sent":             true,
	"sent messages":    true,
	"sent items":       true,
	"[gmail]/sent mail": true,
	"[gmail]/drafts":   true,
}

// systemTrashNames is a fallback list of exact folder names (case-insensitive)
// that are known system trash/junk/spam folders. Used when the IMAP server
// doesn't report special-use attributes (RFC 6154).
// NOTE: Only exact matches — NOT substring matching. This ensures user-created
// folders like @3DayTrash are not incorrectly filtered.
var systemTrashNames = map[string]bool{
	"trash":            true,
	"deleted messages": true,
	"deleted items":    true,
	"[gmail]/trash":    true,
	"[gmail]/all mail": true,
	"[gmail]/spam":     true,
	"junk":             true,
	"spam":             true,
	"junk e-mail":      true,
	"bulk mail":        true,
}

// isSkipFolder returns true if the folder should not trigger rule creation.
// It checks IMAP special-use attributes first (authoritative), then falls back
// to exact name matching for servers that don't report attributes.
// User-created folders like @3DayTrash will NOT match because we use exact
// name comparison, not substring matching.
func isSkipFolder(name string, attrs []imap.MailboxAttr) bool {
	// First: check IMAP attributes (authoritative if present)
	for _, attr := range attrs {
		if skipFolderAttrs[attr] {
			return true
		}
	}

	// Fallback: exact name match for well-known system folder names
	lower := strings.ToLower(name)
	return systemTrashNames[lower]
}

// Detector monitors IMAP accounts for message moves between folders.
type Detector struct {
	db       *db.DB
	notifier *notifier.Telegram

	pollInterval   time.Duration
	oauthProviders map[string]oauth2.Provider
	workers        map[int64]*moveWorker
	mu             sync.RWMutex
	stopCh         chan struct{}
}

// New creates a new move detector.
func New(database *db.DB, telegram *notifier.Telegram, pollInterval time.Duration) *Detector {
	return &Detector{
		db:             database,
		notifier:       telegram,
		pollInterval:   pollInterval,
		oauthProviders: make(map[string]oauth2.Provider),
		workers:        make(map[int64]*moveWorker),
		stopCh:         make(chan struct{}),
	}
}

// SetOAuthProviders sets the OAuth2 providers for token refresh.
func (d *Detector) SetOAuthProviders(providers map[string]oauth2.Provider) {
	d.oauthProviders = providers
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
		if w, exists := d.workers[acc.ID]; exists {
			// Check if credentials changed — restart worker if so
			if moveCredentialsChanged(w.account, acc) {
				log.Info().Int64("account_id", acc.ID).Str("email", acc.Email).Msg("move detector: credentials changed, restarting worker")
				w.stop()
				delete(d.workers, acc.ID)
			} else {
				continue
			}
		}
		w := newMoveWorker(acc, d.db, d.notifier, d.pollInterval, d.oauthProviders)
		d.workers[acc.ID] = w
		go w.run(ctx)
		log.Info().Int64("account_id", acc.ID).Str("email", acc.Email).Msg("started move detector worker")
	}

	for id, w := range d.workers {
		if !activeIDs[id] {
			w.stop()
			delete(d.workers, id)
		}
	}

	return nil
}

// moveCredentialsChanged returns true if the account's authentication credentials
// differ between the cached worker copy and the freshly-loaded database copy.
func moveCredentialsChanged(cached, fresh models.Account) bool {
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

// moveWorker monitors a single IMAP account for message moves.
type moveWorker struct {
	account        models.Account
	db             *db.DB
	notifier       *notifier.Telegram
	oauthProviders map[string]oauth2.Provider
	pollInterval   time.Duration
	stopCh         chan struct{}

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

func newMoveWorker(account models.Account, database *db.DB, telegram *notifier.Telegram, pollInterval time.Duration, oauthProviders map[string]oauth2.Provider) *moveWorker {
	return &moveWorker{
		account:        account,
		db:             database,
		notifier:       telegram,
		oauthProviders: oauthProviders,
		pollInterval:   pollInterval,
		stopCh:         make(chan struct{}),
		inboxUIDs:      make(map[imap.UID]*messageInfo),
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

	// Build a set of Message-IDs we're looking for (batch approach)
	messageIDSet := make(map[string]imap.UID) // messageID → UID
	for uid, info := range missingMessages {
		if info != nil && info.MessageID != "" {
			messageIDSet[info.MessageID] = uid
		}
	}

	if len(messageIDSet) == 0 {
		logger.Debug().Msg("no messages with Message-IDs to search for")
		return nil
	}

	logger.Info().
		Int("search_count", len(messageIDSet)).
		Int("folder_count", len(folders)).
		Msg("batch searching for moved messages across folders")

	// Scan each folder once and match against all missing Message-IDs
	foundDestinations := w.findMessagesInFoldersBatch(client, messageIDSet, folders)

	// Process all found moves
	for messageID, destFolder := range foundDestinations {
		uid := messageIDSet[messageID]
		info := missingMessages[uid]

		// Skip rule creation if destination is a system trash/junk/archive folder (by IMAP attribute)
		if isSkipFolder(destFolder.Name, destFolder.Attrs) {
			logger.Info().
				Str("message_id", messageID).
				Str("destination", destFolder.Name).
				Msg("message moved to system trash/junk/archive folder (by attribute), skipping rule creation")
			continue
		}

		logger.Info().
			Str("message_id", messageID).
			Str("subject", info.Subject).
			Str("sender", info.Sender).
			Str("destination", destFolder.Name).
			Msg("detected move: INBOX → " + destFolder.Name)

		// Record the move
		move := &models.DetectedMove{
			AccountID:  w.account.ID,
			MessageUID: fmt.Sprintf("%d", uid),
			MessageID:  messageID,
			Sender:     info.Sender,
			Subject:    info.Subject,
			FromFolder: "INBOX",
			ToFolder:   destFolder.Name,
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

	// Log messages that were deleted (not found in any folder)
	for messageID := range messageIDSet {
		if _, found := foundDestinations[messageID]; !found {
			logger.Debug().
				Str("message_id", messageID).
				Msg("message disappeared from INBOX (likely deleted)")
		}
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

// folderInfo holds a folder name and its IMAP attributes from the LIST response.
type folderInfo struct {
	Name  string
	Attrs []imap.MailboxAttr
}

// listFolders returns all mailbox info, excluding skip folders (by IMAP attribute).
func (w *moveWorker) listFolders(client *imapclient.Client) ([]folderInfo, error) {
	listCmd := client.List("", "*", nil)

	var folders []folderInfo
	for {
		data := listCmd.Next()
		if data == nil {
			break
		}

		name := data.Mailbox
		if name == "INBOX" {
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
		// Skip Sent/Drafts folders (by IMAP attribute, with name fallback)
		isSentOrDrafts := false
		for _, attr := range data.Attrs {
			if sentOrDraftsAttrs[attr] {
				isSentOrDrafts = true
				break
			}
		}
		if !isSentOrDrafts {
			isSentOrDrafts = sentOrDraftsNames[strings.ToLower(name)]
		}
		if isSentOrDrafts {
			continue
		}
		folders = append(folders, folderInfo{Name: name, Attrs: data.Attrs})
	}

	if err := listCmd.Close(); err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}

	return folders, nil
}

// findMessageInFolders searches for a message by Message-ID in the given folders.
// Returns the folderInfo where found, or empty result if not found.
// DEPRECATED: Use findMessagesInFoldersBatch for processing multiple messages.
func (w *moveWorker) findMessageInFolders(client *imapclient.Client, messageID string, folders []folderInfo) folderInfo {
	for _, folder := range folders {
		selectCmd := client.Select(folder.Name, nil)
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

	return folderInfo{}
}

// findMessagesInFoldersBatch searches for multiple messages across folders in batch.
// Instead of searching each folder for each message individually (O(n*m)),
// it scans each folder once and fetches all Message-IDs, then matches against
// the target set in memory. This is O(n+m) where n=messages in folders, m=target messages.
//
// Returns a map of messageID → folderInfo for all found messages.
func (w *moveWorker) findMessagesInFoldersBatch(client *imapclient.Client, messageIDSet map[string]imap.UID, folders []folderInfo) map[string]folderInfo {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Logger()

	result := make(map[string]folderInfo)
	remaining := len(messageIDSet)

	for _, folder := range folders {
		if remaining == 0 {
			break // All messages found
		}

		selectCmd := client.Select(folder.Name, nil)
		mbox, err := selectCmd.Wait()
		if err != nil {
			logger.Debug().Err(err).Str("folder", folder.Name).Msg("failed to select folder, skipping")
			continue
		}
		if mbox.NumMessages == 0 {
			continue
		}

		// Fetch Message-IDs from all messages in this folder
		seqSet := imap.SeqSet{}
		seqSet.AddRange(1, mbox.NumMessages)

		fetchOptions := &imap.FetchOptions{
			Envelope: true,
		}

		fetchCmd := client.Fetch(seqSet, fetchOptions)
		for {
			msg := fetchCmd.Next()
			if msg == nil {
				break
			}

			buf, err := msg.Collect()
			if err != nil {
				continue
			}
			if buf.Envelope == nil || buf.Envelope.MessageID == "" {
				continue
			}

			// Check if this message's ID is in our target set
			if _, wanted := messageIDSet[buf.Envelope.MessageID]; wanted {
				// Only record the first folder we find it in
				if _, alreadyFound := result[buf.Envelope.MessageID]; !alreadyFound {
					result[buf.Envelope.MessageID] = folder
					remaining--
				}
			}
		}
		fetchCmd.Close()

		logger.Debug().
			Str("folder", folder.Name).
			Uint32("messages", mbox.NumMessages).
			Int("found_so_far", len(result)).
			Int("remaining", remaining).
			Msg("scanned folder for batch move detection")
	}

	logger.Info().
		Int("searched", len(messageIDSet)).
		Int("found", len(result)).
		Msg("batch move detection complete")

	return result
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

	// Authenticate: use XOAUTH2 for OAuth2 accounts, plain login otherwise
	if w.account.OAuthProvider != "" && w.account.OAuthToken != "" {
		// Refresh token if expired
		if oauth2.IsTokenExpired(w.account.OAuthTokenExpiry) {
			if err := w.refreshOAuthToken(); err != nil {
				client.Close()
				return nil, fmt.Errorf("refresh OAuth2 token: %w", err)
			}
		}

		xoauth2Client := &oauth2.XOAuth2Client{
			Username: username,
			Token:    w.account.OAuthToken,
		}
		if err := client.Authenticate(xoauth2Client); err != nil {
			// Token might have just expired, try one refresh
			log.Warn().Err(err).Int64("account_id", w.account.ID).Msg("move detector XOAUTH2 auth failed, attempting token refresh")
			if refreshErr := w.refreshOAuthToken(); refreshErr != nil {
				client.Close()
				return nil, fmt.Errorf("XOAUTH2 auth failed and refresh failed: auth=%w, refresh=%v", err, refreshErr)
			}
			client.Close()
			return nil, fmt.Errorf("XOAUTH2 auth failed, token refreshed, will reconnect: %w", err)
		}
		log.Info().Int64("account_id", w.account.ID).Str("addr", addr).Msg("move detector XOAUTH2 login successful")
	} else {
		loginCmd := client.Login(username, w.account.Password)
		if err := loginCmd.Wait(); err != nil {
			client.Close()
			return nil, fmt.Errorf("login to %s: %w", addr, err)
		}
	}

	return client, nil
}

// refreshOAuthToken refreshes the OAuth2 access token using the refresh token.
func (w *moveWorker) refreshOAuthToken() error {
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
		log.Error().Err(err).Int64("account_id", w.account.ID).Msg("move detector: failed to persist refreshed OAuth2 tokens")
	}

	return nil
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
