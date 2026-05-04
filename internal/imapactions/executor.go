// Package imapactions provides shared IMAP action execution (move, delete, archive)
// used by both the rules engine and scheduler components.
package imapactions

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

// Executor manages IMAP connections and executes actions on messages.
type Executor struct {
	db       *db.DB
	notifier *notifier.Telegram

	// Connection pool: accountID → *imapConn
	conns map[int64]*imapConn
	mu    sync.Mutex
}

type imapConn struct {
	client  *imapclient.Client
	account models.Account
	mu      sync.Mutex
}

// New creates a new IMAP action executor.
func New(database *db.DB, telegram *notifier.Telegram) *Executor {
	return &Executor{
		db:       database,
		notifier: telegram,
		conns:    make(map[int64]*imapConn),
	}
}

// Close disconnects all IMAP connections.
func (e *Executor) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, c := range e.conns {
		c.mu.Lock()
		if c.client != nil {
			logoutCmd := c.client.Logout()
			_ = logoutCmd.Wait()
			c.client.Close()
			c.client = nil
		}
		c.mu.Unlock()
		delete(e.conns, id)
	}
}

// getConnection returns an IMAP connection for the given account, creating one if needed.
func (e *Executor) getConnection(ctx context.Context, accountID int64) (*imapConn, error) {
	e.mu.Lock()
	conn, exists := e.conns[accountID]
	e.mu.Unlock()

	if exists {
		return conn, nil
	}

	// Load account from DB
	accounts, err := e.db.ListActiveAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}

	var account *models.Account
	for i := range accounts {
		if accounts[i].ID == accountID {
			account = &accounts[i]
			break
		}
	}
	if account == nil {
		return nil, fmt.Errorf("account %d not found or inactive", accountID)
	}

	conn = &imapConn{account: *account}

	e.mu.Lock()
	e.conns[accountID] = conn
	e.mu.Unlock()

	return conn, nil
}

// connect establishes an IMAP connection for the given imapConn.
func (c *imapConn) connect() (*imapclient.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		return c.client, nil
	}

	host := c.account.IMAPHost
	port := c.account.IMAPPort
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

	if c.account.IMAPTLS {
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

	username := c.account.Username
	if username == "" {
		username = c.account.Email
	}

	loginCmd := client.Login(username, c.account.Password)
	if err := loginCmd.Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("login to %s: %w", addr, err)
	}

	c.client = client
	return client, nil
}

// disconnect closes the IMAP connection.
func (c *imapConn) disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		logoutCmd := c.client.Logout()
		_ = logoutCmd.Wait()
		c.client.Close()
		c.client = nil
	}
}

// ExecuteAction performs an IMAP action on a message identified by UID.
func (e *Executor) ExecuteAction(ctx context.Context, accountID int64, uid uint32, action, target string) error {
	conn, err := e.getConnection(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get connection: %w", err)
	}

	client, err := conn.connect()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Select INBOX
	selectCmd := client.Select("INBOX", nil)
	if _, err := selectCmd.Wait(); err != nil {
		conn.disconnect()
		return fmt.Errorf("select INBOX: %w", err)
	}

	uidSet := imap.UIDSetNum(imap.UID(uid))

	switch action {
	case "delete":
		return executeDelete(client, uidSet)
	case "move":
		return executeMove(client, uidSet, target)
	case "archive":
		return executeMove(client, uidSet, "Archive")
	case "flag":
		return executeFlag(client, uidSet, target)
	case "notify":
		return e.executeNotify(ctx, target)
	case "keep":
		return nil
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}

// ExecuteActionByMessageID performs an IMAP action on a message found by Message-ID header.
func (e *Executor) ExecuteActionByMessageID(ctx context.Context, accountID int64, messageID, action, target string) error {
	conn, err := e.getConnection(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get connection: %w", err)
	}

	client, err := conn.connect()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Select INBOX
	selectCmd := client.Select("INBOX", nil)
	if _, err := selectCmd.Wait(); err != nil {
		conn.disconnect()
		return fmt.Errorf("select INBOX: %w", err)
	}

	// Search by Message-ID header
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
		log.Warn().Str("message_id", messageID).Msg("message not found in INBOX")
		return nil
	}

	uidSet := imap.UIDSetNum(uids[0])

	switch action {
	case "delete":
		return executeDelete(client, uidSet)
	case "move":
		return executeMove(client, uidSet, target)
	case "archive":
		return executeMove(client, uidSet, "Archive")
	case "flag":
		return executeFlag(client, uidSet, target)
	case "notify":
		return e.executeNotify(ctx, target)
	case "keep":
		return nil
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}

func executeDelete(client *imapclient.Client, uidSet imap.UIDSet) error {
	storeCmd := client.Store(uidSet, &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagDeleted},
	}, nil)
	if err := storeCmd.Close(); err != nil {
		return fmt.Errorf("store deleted flag: %w", err)
	}

	expungeCmd := client.UIDExpunge(uidSet)
	if err := expungeCmd.Close(); err != nil {
		expungeCmd2 := client.Expunge()
		if err2 := expungeCmd2.Close(); err2 != nil {
			return fmt.Errorf("expunge: %w", err2)
		}
	}

	log.Info().Str("uids", uidSet.String()).Msg("deleted message(s)")
	return nil
}

func executeMove(client *imapclient.Client, uidSet imap.UIDSet, folder string) error {
	createCmd := client.Create(folder, nil)
	_ = createCmd.Wait()

	moveCmd := client.Move(uidSet, folder)
	if _, err := moveCmd.Wait(); err != nil {
		return fmt.Errorf("move to %s: %w", folder, err)
	}

	log.Info().Str("uids", uidSet.String()).Str("folder", folder).Msg("moved message(s)")
	return nil
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
		// Custom keyword flag
		return imap.Flag(name)
	}
}

func executeFlag(client *imapclient.Client, uidSet imap.UIDSet, flagName string) error {
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

func (e *Executor) executeNotify(ctx context.Context, message string) error {
	if e.notifier == nil || !e.notifier.Enabled() {
		return nil
	}

	text := message
	if text == "" {
		text = "📧 Email notification triggered"
	}

	return e.notifier.SendMessage(ctx, text)
}

// NotifyRuleAction sends a formatted notification about a rule action.
func (e *Executor) NotifyRuleAction(ctx context.Context, ruleName, action, subject, sender, target string) error {
	if e.notifier == nil || !e.notifier.Enabled() {
		return nil
	}

	var emoji string
	switch action {
	case "delete":
		emoji = "🗑"
	case "move":
		emoji = "📁"
	case "archive":
		emoji = "📦"
	case "keep":
		emoji = "📌"
	case "flag":
		emoji = "🏷"
	default:
		emoji = "📧"
	}

	text := fmt.Sprintf(
		"%s <b>%s</b>\n\n"+
			"<b>Rule:</b> %s\n"+
			"<b>Action:</b> %s\n"+
			"<b>From:</b> %s\n"+
			"<b>Subject:</b> %s",
		emoji, action, ruleName, action, sender, subject,
	)

	if target != "" {
		text += fmt.Sprintf("\n<b>Target:</b> %s", target)
	}

	return e.notifier.SendMessage(ctx, text)
}

// FormatNotifyMessage replaces template variables in a notification message.
func FormatNotifyMessage(message, senderAddr, subject, senderName string) string {
	text := message
	text = strings.ReplaceAll(text, "{sender}", senderAddr)
	text = strings.ReplaceAll(text, "{subject}", subject)
	text = strings.ReplaceAll(text, "{sender_name}", senderName)
	return text
}
