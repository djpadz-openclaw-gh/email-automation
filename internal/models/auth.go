package models

import (
	"time"
)

// User represents an authenticated user.
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	TOTPSecret   *string   `json:"-"`
	TOTPEnabled  bool      `json:"totp_enabled"`
	AIEnabled    bool      `json:"ai_enabled"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// IsAdmin returns true if the user has the admin role.
func (u *User) IsAdmin() bool {
	return u.Role == "admin"
}

// Passkey represents a WebAuthn credential.
type Passkey struct {
	ID              int64      `json:"id"`
	UserID          int64      `json:"user_id"`
	CredentialID    string     `json:"credential_id"`
	PublicKey       string     `json:"public_key"`
	SignCount       uint32     `json:"sign_count"`
	Transports      []string   `json:"transports"`
	BackupEligible  bool       `json:"backup_eligible"`
	BackupState     bool       `json:"backup_state"`
	Name            string     `json:"name"`
	CreatedAt       time.Time  `json:"created_at"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
}

// APIKey represents a programmatic access key.
type APIKeyRecord struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	KeyHash    string     `json:"-"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// LoginAttempt records a login attempt for rate limiting.
type LoginAttempt struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	IPAddress   string    `json:"ip_address"`
	Success     bool      `json:"success"`
	AttemptedAt time.Time `json:"attempted_at"`
}
