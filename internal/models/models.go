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
}

// RuleResult is what a Lua rule returns after evaluation.
type RuleResult struct {
	Action  string `json:"action"`  // skip, delete, archive, move, keep, notify, defer
	Target  string `json:"target"`  // folder name for move, message for notify
	Delay   int    `json:"delay"`   // seconds to defer (for defer action)
	Reason  string `json:"reason"`  // human-readable reason
}
