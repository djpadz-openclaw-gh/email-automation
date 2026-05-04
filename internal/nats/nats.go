package natsbus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

// Stream names for JetStream.
const (
	StreamIncoming = "EMAIL_INCOMING"
	StreamDeferred = "EMAIL_DEFERRED"
	StreamResults  = "EMAIL_RESULTS"
)

// Subject prefixes for JetStream streams.
const (
	SubjectIncoming      = "email.incoming"
	SubjectDeferred      = "email.deferred"
	SubjectResults       = "email.results"
	SubjectDeferredReady = "email.deferred.ready"
)

// Legacy subjects (kept for backward compatibility during migration).
const (
	SubjectNewEmail       = "email.new"
	SubjectRuleResult     = "email.rule.result"
	SubjectDeferredAction = "email.deferred"
	SubjectNotification   = "email.notify"
)

// Consumer names.
const (
	ConsumerRulesEngine = "rules-engine"
	ConsumerScheduler   = "scheduler"
	ConsumerAuditLog    = "audit-log"
)

// Bus wraps a NATS connection with JetStream support for pub/sub messaging.
type Bus struct {
	conn *nats.Conn
	js   jetstream.JetStream
}

// New creates a new NATS bus connection with JetStream enabled.
func New(url string) (*Bus, error) {
	opts := []nats.Option{
		nats.Name("email-automation"),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Warn().Err(err).Msg("nats disconnected")
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			log.Info().Msg("nats reconnected")
		}),
	}

	conn, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create jetstream context: %w", err)
	}

	log.Info().Str("url", url).Msg("connected to NATS with JetStream")
	return &Bus{conn: conn, js: js}, nil
}

// Close drains and closes the NATS connection.
func (b *Bus) Close() {
	if b.conn != nil {
		_ = b.conn.Drain()
	}
}

// JetStream returns the JetStream context for advanced operations.
func (b *Bus) JetStream() jetstream.JetStream {
	return b.js
}

// Conn returns the underlying NATS connection.
func (b *Bus) Conn() *nats.Conn {
	return b.conn
}

// EnsureStreams creates or updates all required JetStream streams.
func (b *Bus) EnsureStreams(ctx context.Context) error {
	streams := []jetstream.StreamConfig{
		{
			Name:        StreamIncoming,
			Description: "New email messages detected by IMAP listener",
			Subjects:    []string{"email.incoming.>"},
			Retention:   jetstream.LimitsPolicy,
			MaxAge:      7 * 24 * time.Hour, // 7 days
			MaxMsgs:     100000,
			Storage:     jetstream.FileStorage,
			Replicas:    1,
			Discard:     jetstream.DiscardOld,
		},
		{
			Name:        StreamDeferred,
			Description: "Deferred email actions scheduled for later execution",
			Subjects:    []string{"email.deferred.>"},
			Retention:   jetstream.LimitsPolicy,
			MaxAge:      30 * 24 * time.Hour, // 30 days
			Storage:     jetstream.FileStorage,
			Replicas:    1,
			Discard:     jetstream.DiscardOld,
		},
		{
			Name:        StreamResults,
			Description: "Rule execution results for audit logging",
			Subjects:    []string{"email.results.>"},
			Retention:   jetstream.LimitsPolicy,
			MaxAge:      7 * 24 * time.Hour, // 7 days
			MaxMsgs:     500000,
			Storage:     jetstream.FileStorage,
			Replicas:    1,
			Discard:     jetstream.DiscardOld,
		},
	}

	for _, cfg := range streams {
		_, err := b.js.CreateOrUpdateStream(ctx, cfg)
		if err != nil {
			return fmt.Errorf("create/update stream %s: %w", cfg.Name, err)
		}
		log.Info().Str("stream", cfg.Name).Msg("JetStream stream ready")
	}

	return nil
}

// PublishToStream publishes a JSON-encoded message to a JetStream subject.
func (b *Bus) PublishToStream(ctx context.Context, subject string, data interface{}) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	_, err = b.js.Publish(ctx, subject, payload)
	if err != nil {
		return fmt.Errorf("publish to %s: %w", subject, err)
	}
	return nil
}

// Publish sends a JSON-encoded message to a core NATS subject (non-JetStream).
// Kept for backward compatibility with components that don't need persistence.
func (b *Bus) Publish(subject string, data interface{}) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return b.conn.Publish(subject, payload)
}

// Subscribe registers a handler for a core NATS subject (non-JetStream).
func (b *Bus) Subscribe(subject string, handler func(data []byte)) (*nats.Subscription, error) {
	return b.conn.Subscribe(subject, func(msg *nats.Msg) {
		handler(msg.Data)
	})
}

// QueueSubscribe registers a handler for a subject with a queue group (non-JetStream).
func (b *Bus) QueueSubscribe(subject, queue string, handler func(data []byte)) (*nats.Subscription, error) {
	return b.conn.QueueSubscribe(subject, queue, func(msg *nats.Msg) {
		handler(msg.Data)
	})
}

// CreateOrUpdateConsumer creates or updates a durable JetStream consumer.
func (b *Bus) CreateOrUpdateConsumer(ctx context.Context, stream string, cfg jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	consumer, err := b.js.CreateOrUpdateConsumer(ctx, stream, cfg)
	if err != nil {
		return nil, fmt.Errorf("create/update consumer %s on %s: %w", cfg.Durable, stream, err)
	}
	log.Info().Str("stream", stream).Str("consumer", cfg.Durable).Msg("JetStream consumer ready")
	return consumer, nil
}

// --- Event types ---

// IncomingEmailEvent is published to email.incoming when a new email is detected.
type IncomingEmailEvent struct {
	AccountID  int64             `json:"account_id"`
	TenantID   int64             `json:"tenant_id"`
	MessageID  string            `json:"message_id"`
	UID        uint32            `json:"uid"`
	Subject    string            `json:"subject"`
	Sender     string            `json:"sender"`
	SenderName string            `json:"sender_name"`
	Recipients []string          `json:"recipients"`
	Date       time.Time         `json:"date"`
	AgeSeconds float64           `json:"age_seconds"`
	BodyPreview string           `json:"body_preview"`
	Folder     string            `json:"folder"`
	Headers    map[string]string `json:"headers"`
	// Attachment metadata
	HasAttachments  bool     `json:"has_attachments"`
	AttachmentNames []string `json:"attachment_names"`
	AttachmentTypes []string `json:"attachment_types"`
	// Image data for vision analysis
	HasImages        bool              `json:"has_images"`
	ImageAttachments []ImageAttachment `json:"image_attachments,omitempty"`
}

// ImageAttachment holds base64-encoded image data for transit via NATS.
type ImageAttachment struct {
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// RuleResultEvent is published to email.results when a rule produces a result.
type RuleResultEvent struct {
	AccountID int64  `json:"account_id"`
	TenantID  int64  `json:"tenant_id"`
	MessageID string `json:"message_id"`
	UID       uint32 `json:"uid"`
	RuleID    int64  `json:"rule_id"`
	RuleName  string `json:"rule_name"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Reason    string `json:"reason"`
	Subject   string `json:"subject"`
	Sender    string `json:"sender"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

// DeferredActionEvent is published to email.deferred when an action should be deferred.
type DeferredActionEvent struct {
	ActionID  int64  `json:"action_id"`
	AccountID int64  `json:"account_id"`
	TenantID  int64  `json:"tenant_id"`
	MessageID string `json:"message_id"`
	UID       uint32 `json:"uid"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Delay     int    `json:"delay"`
}

// NewEmailEvent is published when a new email arrives (legacy, kept for compatibility).
type NewEmailEvent struct {
	AccountID int64  `json:"account_id"`
	MessageID string `json:"message_id"`
	UID       uint32 `json:"uid"`
	Subject   string `json:"subject"`
	Sender    string `json:"sender"`
	Folder    string `json:"folder"`
}

// NotificationEvent is published when a notification should be sent.
type NotificationEvent struct {
	RuleName string `json:"rule_name"`
	Action   string `json:"action"`
	Subject  string `json:"subject"`
	Sender   string `json:"sender"`
	Target   string `json:"target"`
	Message  string `json:"message"`
}
