package imap

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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

	// Initial account load
	if err := p.refreshWorkers(ctx); err != nil {
		return fmt.Errorf("initial worker refresh: %w", err)
	}

	// Periodically refresh workers (accounts may be added/removed)
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

func (p *Pool) refreshWorkers(ctx context.Context) error {
	accounts, err := p.db.ListActiveAccounts(ctx)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Track which accounts are still active
	activeIDs := make(map[int64]bool)
	for _, acc := range accounts {
		activeIDs[acc.ID] = true

		if _, exists := p.workers[acc.ID]; !exists {
			// Start new worker
			w := NewWorker(acc, p.db, p.engine, p.bus, p.notifier, p.idleTimeout, p.pollInterval)
			p.workers[acc.ID] = w
			go w.Run(ctx)
			log.Info().Int64("account_id", acc.ID).Str("email", acc.Email).Msg("started IMAP worker")
		}
	}

	// Stop workers for deactivated accounts
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
	}
}

// Run starts the worker's main loop.
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
			return
		case <-w.stopCh:
			logger.Info().Msg("IMAP worker stopping (stop signal)")
			return
		default:
		}

		if err := w.poll(ctx); err != nil {
			logger.Error().Err(err).Msg("poll cycle failed")
		}

		// Wait before next poll (or use IDLE if supported)
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-time.After(w.pollInterval):
		}
	}
}

// Stop signals the worker to stop.
func (w *Worker) Stop() {
	close(w.stopCh)
}

// poll checks for new messages and processes them through the rule engine.
func (w *Worker) poll(ctx context.Context) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("email", w.account.Email).
		Logger()

	// TODO: Implement actual IMAP connection using go-imap/v2
	// For now, this is a placeholder that demonstrates the architecture.
	//
	// The real implementation would:
	// 1. Connect to IMAP server (w.account.IMAPHost:w.account.IMAPPort)
	// 2. Login with credentials
	// 3. SELECT INBOX
	// 4. SEARCH for UNSEEN messages
	// 5. FETCH each message's envelope + body structure
	// 6. Build EmailContext from IMAP data
	// 7. Run through rule engine
	// 8. Execute resulting actions (MOVE, DELETE, etc.)
	// 9. Use IDLE for real-time monitoring between polls

	logger.Debug().Msg("polling for new messages")

	// Update sync time
	if err := w.db.UpdateAccountSyncTime(ctx, w.account.ID); err != nil {
		logger.Warn().Err(err).Msg("failed to update sync time")
	}

	return nil
}

// processMessage evaluates a single message against all active rules.
func (w *Worker) processMessage(ctx context.Context, email *models.EmailContext) error {
	logger := log.With().
		Int64("account_id", w.account.ID).
		Str("message_id", email.MessageID).
		Str("subject", email.Subject).
		Logger()

	// Get active rules for this tenant
	rules, err := w.db.ListActiveRules(ctx, w.account.TenantID)
	if err != nil {
		return fmt.Errorf("list rules: %w", err)
	}

	// Evaluate all rules
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
			Action:    result.Target, // The actual action to perform later
			Target:    result.Target,
			ExecuteAt: time.Now().Add(time.Duration(result.Delay) * time.Second),
		}
		if err := w.db.CreateDeferredAction(ctx, deferred); err != nil {
			logger.Error().Err(err).Msg("failed to create deferred action")
		}
		return nil
	}

	// Execute immediate actions
	switch result.Action {
	case "delete":
		return w.executeDelete(ctx, email)
	case "move":
		return w.executeMove(ctx, email, result.Target)
	case "archive":
		return w.executeMove(ctx, email, "Archive")
	case "notify":
		return w.executeNotify(ctx, email, matchedRule.Name, result.Target)
	case "keep":
		// Do nothing — explicitly keep in inbox
		return nil
	}

	return nil
}

func (w *Worker) executeDelete(ctx context.Context, email *models.EmailContext) error {
	// TODO: Implement IMAP delete (add \Deleted flag + EXPUNGE)
	log.Info().Str("message_id", email.MessageID).Msg("would delete message")
	return nil
}

func (w *Worker) executeMove(ctx context.Context, email *models.EmailContext, folder string) error {
	// TODO: Implement IMAP move (COPY to folder + delete from source)
	// Ensure target folder exists (CREATE if needed)
	log.Info().Str("message_id", email.MessageID).Str("folder", folder).Msg("would move message")
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

	// Replace template variables
	text = strings.ReplaceAll(text, "{sender}", email.SenderAddress)
	text = strings.ReplaceAll(text, "{subject}", email.Subject)
	text = strings.ReplaceAll(text, "{sender_name}", email.SenderName)

	return w.notifier.SendMessage(ctx, text)
}
