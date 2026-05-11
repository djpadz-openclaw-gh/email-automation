package scheduler

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/db"
	natsbus "github.com/djpadz/email-automation/internal/nats"
	"github.com/djpadz/email-automation/internal/notifier"
)

// Scheduler processes deferred actions on a timer.
type Scheduler struct {
	db       *db.DB
	bus      *natsbus.Bus
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

// SetBus sets the NATS bus for publishing deferred action events.
func (s *Scheduler) SetBus(bus *natsbus.Bus) {
	s.bus = bus
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

		// Publish to NATS for the IMAP worker to execute
		if s.bus != nil {
			event := &natsbus.DeferredActionEvent{
				ActionID:  action.ID,
				AccountID: action.AccountID,
				MessageID: action.MessageID,
				Action:    action.Action,
				Target:    action.Target,
			}
			if err := s.bus.Publish(natsbus.SubjectDeferredAction, event); err != nil {
				logger.Error().Err(err).Msg("failed to publish deferred action event")
				continue
			}
			logger.Info().Msg("published deferred action to NATS")
		} else {
			logger.Warn().Msg("NATS bus not available, cannot execute deferred action")
			continue
		}

		// Mark as done
		if err := s.db.MarkDeferredActionDone(ctx, action.ID, ""); err != nil {
			logger.Error().Err(err).Msg("failed to mark deferred action as done")
			continue
		}

		logger.Info().Msg("deferred action queued for execution")
	}
}
