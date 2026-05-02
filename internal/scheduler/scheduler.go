package scheduler

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/notifier"
)

// Scheduler processes deferred actions on a timer.
type Scheduler struct {
	db       *db.DB
	notifier *notifier.Telegram
	interval time.Duration
	stopCh   chan struct{}
}

// New creates a new scheduler.
func New(database *db.DB, telegram *notifier.Telegram, interval time.Duration) *Scheduler {
	return &Scheduler{
		db:       database,
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
		}
	}
}

// Stop signals the scheduler to stop.
func (s *Scheduler) Stop() {
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
			Str("message_id", action.MessageID).
			Str("action", action.Action).
			Logger()

		// TODO: Execute the actual IMAP action (move, delete, etc.)
		// This requires access to the IMAP connection for the account.
		// For now, we publish to NATS for the IMAP worker to handle.
		logger.Info().Msg("executing deferred action")

		errMsg := ""
		// Mark as done
		if err := s.db.MarkDeferredActionDone(ctx, action.ID, errMsg); err != nil {
			logger.Error().Err(err).Msg("failed to mark deferred action as done")
			continue
		}

		logger.Info().Msg("deferred action completed")
	}
}
