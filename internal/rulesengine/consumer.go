// Package rulesengine consumes messages from the email.incoming JetStream stream,
// evaluates rules against each message, executes immediate actions, and publishes
// results and deferred actions to their respective streams.
package rulesengine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/engine"
	"github.com/djpadz/email-automation/internal/imapactions"
	"github.com/djpadz/email-automation/internal/models"
	natsbus "github.com/djpadz/email-automation/internal/nats"
	"github.com/djpadz/email-automation/internal/notifier"
)

// Consumer subscribes to the email.incoming stream and processes messages
// through the rule engine.
type Consumer struct {
	db       *db.DB
	engine   *engine.Engine
	bus      *natsbus.Bus
	executor *imapactions.Executor
	notifier *notifier.Telegram

	iter   jetstream.MessagesContext
	cancel context.CancelFunc
	stopCh chan struct{}
	doneCh chan struct{}
}

// New creates a new rules engine consumer.
func New(database *db.DB, eng *engine.Engine, bus *natsbus.Bus, telegram *notifier.Telegram) *Consumer {
	return &Consumer{
		db:       database,
		engine:   eng,
		bus:      bus,
		executor: imapactions.New(database, telegram),
		notifier: telegram,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start begins consuming messages from the email.incoming stream.
func (c *Consumer) Start(ctx context.Context) error {
	// Create durable consumer with deliver group for horizontal scaling across pods
	// Use a new consumer name to force creation from the beginning of the stream
	consumer, err := c.bus.CreateOrUpdateConsumer(ctx, natsbus.StreamIncoming, jetstream.ConsumerConfig{
		Durable:        natsbus.ConsumerRulesEngine + "-v2",
		DeliverPolicy:  jetstream.DeliverAllPolicy, // Start from the beginning of the stream
		AckPolicy:      jetstream.AckExplicitPolicy,
		AckWait:        2 * time.Minute, // Allow time for Kiro API calls + IMAP actions
		MaxDeliver:     3,               // Retry up to 3 times
		MaxAckPending:  256,             // Limit in-flight messages per consumer
		FilterSubject:  "email.incoming.>",
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	// Use Messages() pull-based iterator instead of Consume() push callback.
	// Messages() gives explicit control over the fetch loop and is more resilient
	// to silent stalls that can occur with Consume() after draining the initial backlog.
	iter, err := consumer.Messages(
		jetstream.PullMaxMessages(50),
		jetstream.PullHeartbeat(5*time.Second),
	)
	if err != nil {
		return fmt.Errorf("start messages iterator: %w", err)
	}
	c.iter = iter

	// Create a cancellable context for the processing loop
	loopCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Start the message processing loop in a goroutine
	go c.processLoop(loopCtx, iter)

	log.Info().Msg("rules engine consumer started")
	return nil
}

// processLoop continuously pulls and processes messages from the iterator.
func (c *Consumer) processLoop(ctx context.Context, iter jetstream.MessagesContext) {
	defer close(c.doneCh)

	log.Info().Msg("rules engine process loop started")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("rules engine process loop stopping (context cancelled)")
			return
		case <-c.stopCh:
			log.Info().Msg("rules engine process loop stopping (stop signal)")
			return
		default:
		}

		msg, err := iter.Next()
		if err != nil {
			if err == jetstream.ErrMsgIteratorClosed {
				log.Info().Msg("message iterator closed, stopping process loop")
				return
			}
			log.Error().Err(err).Msg("error fetching next message, retrying in 1s")
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-time.After(1 * time.Second):
				continue
			}
		}

		c.handleMessage(ctx, msg)
	}
}

// Stop gracefully shuts down the consumer.
func (c *Consumer) Stop() {
	// Stop the iterator first to unblock any pending Next() call
	if c.iter != nil {
		c.iter.Stop()
	}
	if c.cancel != nil {
		c.cancel()
	}
	close(c.stopCh)
	// Wait for the process loop to finish (with timeout)
	select {
	case <-c.doneCh:
	case <-time.After(30 * time.Second):
		log.Warn().Msg("rules engine process loop did not stop within 30s")
	}
	c.executor.Close()
}

// handleMessage processes a single incoming email event.
func (c *Consumer) handleMessage(ctx context.Context, msg jetstream.Msg) {
	var event natsbus.IncomingEmailEvent
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		log.Error().Err(err).Msg("failed to unmarshal incoming email event")
		// Don't retry malformed messages
		_ = msg.Term()
		return
	}

	logger := log.With().
		Int64("account_id", event.AccountID).
		Str("message_id", event.MessageID).
		Str("subject", event.Subject).
		Str("sender", event.Sender).
		Logger()

	logger.Info().Msg("processing incoming email")

	// Check if already processed (idempotency)
	processed, err := c.db.IsMessageProcessed(ctx, event.AccountID, fmt.Sprintf("%d", event.UID))
	if err != nil {
		logger.Warn().Err(err).Msg("failed to check processed status")
	}
	if processed {
		logger.Debug().Msg("message already processed, skipping")
		_ = msg.Ack()
		return
	}

	// Convert event to EmailContext for rule evaluation
	emailCtx := eventToEmailContext(&event)

	// Load active rules for this tenant
	rules, err := c.db.ListActiveRules(ctx, event.TenantID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list active rules")
		_ = msg.Nak()
		return
	}

	// Check if the tenant's user has AI enabled
	aiEnabled := true
	userID, err := c.db.GetUserIDByTenantID(ctx, event.TenantID)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get user for tenant, defaulting AI to enabled")
	} else {
		aiEnabled, err = c.db.GetUserAIEnabled(ctx, userID)
		if err != nil {
			logger.Warn().Err(err).Int64("user_id", userID).Msg("failed to check user AI permission, defaulting to enabled")
			aiEnabled = true
		}
		if !aiEnabled {
			logger.Info().Int64("user_id", userID).Msg("AI disabled for user, kiro.* calls will be skipped")
		}
	}

	// Evaluate all rules with AI permission context
	opts := &engine.EvaluateOptions{AIEnabled: aiEnabled}
	result, matchedRule, err := c.engine.EvaluateAllWithOptions(rules, emailCtx, opts)
	if err != nil {
		logger.Error().Err(err).Msg("failed to evaluate rules")
		_ = msg.Nak()
		return
	}

	if result.Action == "skip" {
		logger.Debug().Msg("no rules matched")
		_ = msg.Ack()
		return
	}

	logger.Info().
		Str("action", result.Action).
		Str("target", result.Target).
		Str("rule", matchedRule.Name).
		Msg("rule matched")

	// Log execution
	execLog := &models.RuleExecutionLog{
		RuleID:    matchedRule.ID,
		AccountID: event.AccountID,
		MessageID: event.MessageID,
		Subject:   event.Subject,
		Sender:    event.Sender,
		Action:    result.Action,
		Target:    result.Target,
		Success:   true,
	}
	if err := c.db.LogExecution(ctx, execLog); err != nil {
		logger.Warn().Err(err).Msg("failed to log execution")
	}

	// Publish result to email.results stream
	resultEvent := &natsbus.RuleResultEvent{
		AccountID: event.AccountID,
		TenantID:  event.TenantID,
		MessageID: event.MessageID,
		UID:       event.UID,
		RuleID:    matchedRule.ID,
		RuleName:  matchedRule.Name,
		Action:    result.Action,
		Target:    result.Target,
		Reason:    result.Reason,
		Subject:   event.Subject,
		Sender:    event.Sender,
		Success:   true,
	}
	resultSubject := fmt.Sprintf("email.results.%d", event.AccountID)
	if err := c.bus.PublishToStream(ctx, resultSubject, resultEvent); err != nil {
		logger.Warn().Err(err).Msg("failed to publish rule result")
	}

	// Handle deferred actions
	if result.Action == "defer" && result.Delay > 0 {
		deferred := &models.DeferredAction{
			RuleID:    matchedRule.ID,
			AccountID: event.AccountID,
			MessageID: event.MessageID,
			Action:    result.Target,
			Target:    result.Target,
			ExecuteAt: time.Now().Add(time.Duration(result.Delay) * time.Second),
		}
		if err := c.db.CreateDeferredAction(ctx, deferred); err != nil {
			logger.Error().Err(err).Msg("failed to create deferred action")
			_ = msg.Nak()
			return
		}

		// Also publish to deferred stream for visibility
		deferredEvent := &natsbus.DeferredActionEvent{
			ActionID:  deferred.ID,
			AccountID: event.AccountID,
			TenantID:  event.TenantID,
			MessageID: event.MessageID,
			UID:       event.UID,
			Action:    result.Target,
			Target:    result.Target,
			Delay:     result.Delay,
		}
		deferredSubject := fmt.Sprintf("email.deferred.%d", event.AccountID)
		if err := c.bus.PublishToStream(ctx, deferredSubject, deferredEvent); err != nil {
			logger.Warn().Err(err).Msg("failed to publish deferred action event")
		}

		_ = msg.Ack()
		return
	}

	// Execute immediate actions via IMAP
	var actionErr error
	switch result.Action {
	case "delete", "move", "archive", "flag":
		actionErr = c.executor.ExecuteAction(ctx, event.AccountID, event.UID, result.Action, result.Target)
	case "notify":
		notifyMsg := result.Target
		if notifyMsg == "" {
			notifyMsg = fmt.Sprintf("📧 New email from %s: %s", event.Sender, event.Subject)
		}
		notifyMsg = imapactions.FormatNotifyMessage(notifyMsg, event.Sender, event.Subject, event.SenderName)
		actionErr = c.executor.NotifyRuleAction(ctx, matchedRule.Name, result.Action, event.Subject, event.Sender, result.Target)
	case "keep":
		// Do nothing
	}

	if actionErr != nil {
		logger.Error().Err(actionErr).Str("action", result.Action).Msg("failed to execute action")
		execLog.Success = false
		execLog.Error = actionErr.Error()
		_ = c.db.LogExecution(ctx, execLog)

		// Update result event with error
		resultEvent.Success = false
		resultEvent.Error = actionErr.Error()
		_ = c.bus.PublishToStream(ctx, resultSubject, resultEvent)

		_ = msg.Nak()
		return
	}

	// Mark as processed
	_ = c.db.MarkMessageProcessed(ctx, event.AccountID, fmt.Sprintf("%d", event.UID), matchedRule.ID, result.Action)

	_ = msg.Ack()
	logger.Info().Str("action", result.Action).Msg("message processed successfully")
}

// eventToEmailContext converts an IncomingEmailEvent to an EmailContext for rule evaluation.
func eventToEmailContext(event *natsbus.IncomingEmailEvent) *models.EmailContext {
	// Convert image attachments
	var imageAttachments []models.ImageAttachment
	for _, img := range event.ImageAttachments {
		imageAttachments = append(imageAttachments, models.ImageAttachment{
			Filename:  img.Filename,
			MediaType: img.MediaType,
			Data:      img.Data,
		})
	}

	return &models.EmailContext{
		MessageID:        event.MessageID,
		Subject:          event.Subject,
		SenderName:       event.SenderName,
		SenderAddress:    event.Sender,
		Recipients:       event.Recipients,
		Date:             event.Date,
		AgeSeconds:       event.AgeSeconds,
		BodyPreview:      event.BodyPreview,
		HasAttachments:   event.HasAttachments,
		AttachmentNames:  event.AttachmentNames,
		AttachmentTypes:  event.AttachmentTypes,
		Headers:          event.Headers,
		Folder:           event.Folder,
		AccountID:        event.AccountID,
		HasImages:        event.HasImages,
		ImageAttachments: imageAttachments,
	}
}

// hasImageAttachments checks if any attachment types are images.
func hasImageAttachments(attachmentTypes []string) bool {
	for _, ct := range attachmentTypes {
		if strings.HasPrefix(strings.ToLower(ct), "image/") {
			return true
		}
	}
	return false
}
