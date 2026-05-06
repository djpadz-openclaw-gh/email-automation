// Package sched processes deferred actions from the database and executes them
// via IMAP when their scheduled time arrives. It publishes execution results
// to the email.results JetStream stream.
package sched

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/imapactions"
	natsbus "github.com/djpadz/email-automation/internal/nats"
	"github.com/djpadz/email-automation/internal/notifier"
)

// Scheduler processes deferred actions on a timer and executes them via IMAP.
type Scheduler struct {
	db       *db.DB
	bus      *natsbus.Bus
	executor *imapactions.Executor
	notifier *notifier.Telegram
	interval time.Duration
	stopCh   chan struct{}
}

// New creates a new scheduler.
func New(database *db.DB, bus *natsbus.Bus, telegram *notifier.Telegram, interval time.Duration) *Scheduler {
	return &Scheduler{
		db:       database,
		bus:      bus,
		executor: imapactions.New(database, telegram),
		notifier: telegram,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the scheduler loop.
func (s *Scheduler) Start(ctx context.Context) {
	log.Info().Dur("interval", s.interval).Msg("scheduler started")

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Cleanup ticker runs every hour to remove stale rule_applied_moves entries
	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("scheduler stopping (context cancelled)")
			return
		case <-s.stopCh:
			log.Info().Msg("scheduler stopping (stop signal)")
			return
		case <-ticker.C:
			s.processPending(ctx)
		case <-cleanupTicker.C:
			s.cleanupStaleData(ctx)
		}
	}
}

// cleanupStaleData removes expired rule_applied_moves and old message locations.
func (s *Scheduler) cleanupStaleData(ctx context.Context) {
	deleted, err := s.db.CleanOldRuleAppliedMoves(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to clean old rule_applied_moves")
	} else if deleted > 0 {
		log.Info().Int64("deleted", deleted).Msg("cleaned old rule_applied_moves entries")
	}
}

// Stop signals the scheduler to stop.
func (s *Scheduler) Stop() {
	s.executor.Close()
	close(s.stopCh)
}

func (s *Scheduler) processPending(ctx context.Context) {
	actions, err := s.db.GetPendingDeferredActions(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to get pending deferred actions")
		return
	}

	if len(actions) == 0 {
		return
	}

	log.Info().Int("count", len(actions)).Msg("processing deferred actions")

	for _, action := range actions {
		logger := log.With().
			Int64("action_id", action.ID).
			Int64("account_id", action.AccountID).
			Str("message_id", action.MessageID).
			Str("action", action.Action).
			Logger()

		logger.Info().Msg("executing deferred action")

		// Execute the action via IMAP using Message-ID search
		actionErr := s.executor.ExecuteActionByMessageIDForRule(ctx, action.AccountID, action.MessageID, action.Action, action.Target, action.RuleID)

		errMsg := ""
		if actionErr != nil {
			errMsg = actionErr.Error()
			logger.Error().Err(actionErr).Msg("failed to execute deferred action")
		} else {
			logger.Info().Msg("deferred action executed successfully")
		}

		// Mark as done
		if err := s.db.MarkDeferredActionDone(ctx, action.ID, errMsg); err != nil {
			logger.Error().Err(err).Msg("failed to mark deferred action as done")
			continue
		}

		// Publish result to email.results stream
		if s.bus != nil {
			resultEvent := &natsbus.RuleResultEvent{
				AccountID: action.AccountID,
				MessageID: action.MessageID,
				RuleID:    action.RuleID,
				RuleName:  "deferred",
				Action:    action.Action,
				Target:    action.Target,
				Success:   actionErr == nil,
			}
			if actionErr != nil {
				resultEvent.Error = actionErr.Error()
			}
			resultSubject := fmt.Sprintf("email.results.%d", action.AccountID)
			if err := s.bus.PublishToStream(ctx, resultSubject, resultEvent); err != nil {
				logger.Warn().Err(err).Msg("failed to publish deferred action result")
			}
		}
	}
}
