package models

import (
	"time"
)

// Tenant represents a top-level tenant (multi-tenant support).
type Tenant struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	APIKey    string    `json:"api_key,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Account represents an email account belonging to a tenant.
type Account struct {
	ID           int64     `json:"id"`
	TenantID     int64     `json:"tenant_id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Provider     string    `json:"provider"` // imap, graph, mapi
	IMAPHost     string    `json:"imap_host,omitempty"`
	IMAPPort     int       `json:"imap_port,omitempty"`
	IMAPTLS      bool      `json:"imap_tls"`
	Username     string    `json:"username,omitempty"`
	Password     string    `json:"password,omitempty"`
	OAuthToken   string    `json:"oauth_token,omitempty"`
	Active       bool      `json:"active"`
	LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Rule represents a Lua rule associated with a tenant.
type Rule struct {
	ID          int64     `json:"id"`
	TenantID    int64     `json:"tenant_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	LuaCode     string    `json:"lua_code"`
	Priority    int       `json:"priority"`
	Active      bool      `json:"active"`
	Source      string    `json:"source"`              // "manual" or "auto-learned"
	Approved    bool      `json:"approved"`             // auto-learned rules start unapproved
	UsesAI      bool      `json:"uses_ai"`              // true if lua_code contains kiro.* calls
	AccountIDs  []int64   `json:"account_ids,omitempty"` // empty = all accounts
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// RuleExecutionLog records each time a rule fires on a message.
type RuleExecutionLog struct {
	ID          int64     `json:"id"`
	RuleID      int64     `json:"rule_id"`
	AccountID   int64     `json:"account_id"`
	MessageID   string    `json:"message_id"`
	Subject     string    `json:"subject"`
	Sender      string    `json:"sender"`
	Action      string    `json:"action"`
	Target      string    `json:"target,omitempty"`
	Success     bool      `json:"success"`
	Error       string    `json:"error,omitempty"`
	ExecutedAt  time.Time `json:"executed_at"`
}

// DeferredAction represents a scheduled action to execute later.
type DeferredAction struct {
	ID          int64     `json:"id"`
	RuleID      int64     `json:"rule_id"`
	AccountID   int64     `json:"account_id"`
	MessageID   string    `json:"message_id"`
	Action      string    `json:"action"`
	Target      string    `json:"target,omitempty"`
	ExecuteAt   time.Time `json:"execute_at"`
	Executed    bool      `json:"executed"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// ImageAttachment holds a base64-encoded image extracted from an email for
// vision-based analysis via the Anthropic API.
type ImageAttachment struct {
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"` // e.g. "image/jpeg", "image/png"
	Data      string `json:"data"`       // base64-encoded image bytes
}

// EmailContext is the data passed to Lua rules for evaluation.
type EmailContext struct {
	MessageID     string            `json:"message_id"`
	Subject       string            `json:"subject"`
	SenderName    string            `json:"sender_name"`
	SenderAddress string            `json:"sender_address"`
	Recipients    []string          `json:"recipients"`
	Date          time.Time         `json:"date"`
	AgeSeconds    float64           `json:"age_seconds"`
	BodyPreview   string            `json:"body_preview"`
	HasAttachments bool             `json:"has_attachments"`
	AttachmentNames []string        `json:"attachment_names"`
	AttachmentTypes []string        `json:"attachment_types"`
	Headers       map[string]string `json:"headers"`
	Folder        string            `json:"folder"`
	AccountID     int64             `json:"account_id"`
	// OCR text extracted from image attachments (invoices, receipts, etc.)
	OCRText       string            `json:"ocr_text,omitempty"`
	// Whether image attachments were detected
	HasImages     bool              `json:"has_images"`
	// Actual image attachment data for vision-based analysis
	ImageAttachments []ImageAttachment `json:"image_attachments,omitempty"`
}

// RuleResult is what a Lua rule returns after evaluation.
type RuleResult struct {
	Action  string `json:"action"`  // skip, delete, archive, move, keep, notify, defer
	Target  string `json:"target"`  // folder name for move, message for notify
	Delay   int    `json:"delay"`   // seconds to defer (for defer action)
	Reason  string `json:"reason"`  // human-readable reason
}

// DryRunMatch represents a single email that matched during a dry-run.
type DryRunMatch struct {
	MessageID     string `json:"message_id"`
	Subject       string `json:"subject"`
	SenderAddress string `json:"sender_address"`
	Action        string `json:"action"`
	Target        string `json:"target"`
	Reason        string `json:"reason"`
}

// DryRunResult is the response from a dry-run evaluation.
type DryRunResult struct {
	TotalScanned int           `json:"total_scanned"`
	TotalMatched int           `json:"total_matched"`
	Matches      []DryRunMatch `json:"matches"`
	Cancelled    bool          `json:"cancelled,omitempty"`
}

// ExecuteResult is the response from executing a rule against the mailbox.
type ExecuteResult struct {
	TotalScanned  int           `json:"total_scanned"`
	TotalExecuted int           `json:"total_executed"`
	TotalFailed   int           `json:"total_failed"`
	Results       []DryRunMatch `json:"results"`
	Errors        []string      `json:"errors,omitempty"`
	Cancelled     bool          `json:"cancelled,omitempty"`
}

// MessageLocation tracks where a message was last seen.
type MessageLocation struct {
	AccountID  int64     `json:"account_id"`
	MessageUID string    `json:"message_uid"`
	Folder     string    `json:"folder"`
	MessageID  string    `json:"message_id"`
	Sender     string    `json:"sender"`
	Subject    string    `json:"subject"`
	SeenAt     time.Time `json:"seen_at"`
}

// DetectedMove records when a message moves between folders.
type DetectedMove struct {
	ID         int64      `json:"id"`
	AccountID  int64      `json:"account_id"`
	MessageUID string     `json:"message_uid"`
	MessageID  string     `json:"message_id"`
	Sender     string     `json:"sender"`
	Subject    string     `json:"subject"`
	FromFolder string     `json:"from_folder"`
	ToFolder   string     `json:"to_folder"`
	DetectedAt time.Time  `json:"detected_at"`
	RuleID     *int64     `json:"rule_id,omitempty"`
}
