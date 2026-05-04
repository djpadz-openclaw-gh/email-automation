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

	consumeCtx jetstream.ConsumeContext
	stopCh     chan struct{}
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
	}
}

// Start begins consuming messages from the email.incoming stream.
func (c *Consumer) Start(ctx context.Context) error {
	// Create durable consumer with queue group for horizontal scaling
	consumer, err := c.bus.CreateOrUpdateConsumer(ctx, natsbus.StreamIncoming, jetstream.ConsumerConfig{
		Durable:       natsbus.ConsumerRulesEngine,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       2 * time.Minute, // Allow time for Kiro API calls + IMAP actions
		MaxDeliver:    3,               // Retry up to 3 times
		FilterSubject: "email.incoming.>",
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		c.handleMessage(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("start consuming: %w", err)
	}

	c.consumeCtx = consumeCtx
	log.Info().Msg("rules engine consumer started")
	return nil
}

// Stop gracefully shuts down the consumer.
func (c *Consumer) Stop() {
	if c.consumeCtx != nil {
		c.consumeCtx.Drain()
	}
	c.executor.Close()
	close(c.stopCh)
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

	// Evaluate all rules
	result, matchedRule, err := c.engine.EvaluateAll(rules, emailCtx)
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
