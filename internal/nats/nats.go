package natsbus

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// Subjects for NATS messaging.
const (
	SubjectNewEmail      = "email.new"
	SubjectRuleResult    = "email.rule.result"
	SubjectDeferredAction = "email.deferred"
	SubjectNotification  = "email.notify"
)

// Bus wraps a NATS connection for pub/sub messaging.
type Bus struct {
	conn *nats.Conn
}

// New creates a new NATS bus connection.
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

	log.Info().Str("url", url).Msg("connected to NATS")
	return &Bus{conn: conn}, nil
}

// Close drains and closes the NATS connection.
func (b *Bus) Close() {
	if b.conn != nil {
		_ = b.conn.Drain()
	}
}

// Publish sends a JSON-encoded message to a subject.
func (b *Bus) Publish(subject string, data interface{}) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return b.conn.Publish(subject, payload)
}

// Subscribe registers a handler for a subject.
func (b *Bus) Subscribe(subject string, handler func(data []byte)) (*nats.Subscription, error) {
	return b.conn.Subscribe(subject, func(msg *nats.Msg) {
		handler(msg.Data)
	})
}

// QueueSubscribe registers a handler for a subject with a queue group.
func (b *Bus) QueueSubscribe(subject, queue string, handler func(data []byte)) (*nats.Subscription, error) {
	return b.conn.QueueSubscribe(subject, queue, func(msg *nats.Msg) {
		handler(msg.Data)
	})
}

// NewEmailEvent is published when a new email arrives.
type NewEmailEvent struct {
	AccountID int64  `json:"account_id"`
	MessageID string `json:"message_id"`
	UID       uint32 `json:"uid"`
	Subject   string `json:"subject"`
	Sender    string `json:"sender"`
	Folder    string `json:"folder"`
}

// RuleResultEvent is published when a rule produces a result.
type RuleResultEvent struct {
	AccountID int64  `json:"account_id"`
	MessageID string `json:"message_id"`
	RuleID    int64  `json:"rule_id"`
	RuleName  string `json:"rule_name"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Subject   string `json:"subject"`
	Sender    string `json:"sender"`
}

// DeferredActionEvent is published when a deferred action is ready.
type DeferredActionEvent struct {
	ActionID  int64  `json:"action_id"`
	AccountID int64  `json:"account_id"`
	MessageID string `json:"message_id"`
	Action    string `json:"action"`
	Target    string `json:"target"`
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
