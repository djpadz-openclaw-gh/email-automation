package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/djpadz/email-automation/internal/models"
)

// --- User operations ---

func (db *DB) CreateUser(ctx context.Context, u *models.User) error {
	// Default ai_enabled to true for new users
	if !u.AIEnabled {
		u.AIEnabled = true
	}
	// Default role to 'user' if not set
	if u.Role == "" {
		u.Role = "user"
	}
	return db.Pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash, totp_secret, totp_enabled, ai_enabled, role)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at, updated_at`,
		u.Username, u.PasswordHash, u.TOTPSecret, u.TOTPEnabled, u.AIEnabled, u.Role,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
}

func (db *DB) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	u := &models.User{}
	err := db.Pool.QueryRow(ctx,
		`SELECT id, username, password_hash, totp_secret, totp_enabled, ai_enabled, role, created_at, updated_at
		 FROM users WHERE username = $1`, username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TOTPSecret, &u.TOTPEnabled, &u.AIEnabled, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	u := &models.User{}
	err := db.Pool.QueryRow(ctx,
		`SELECT id, username, password_hash, totp_secret, totp_enabled, ai_enabled, role, created_at, updated_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TOTPSecret, &u.TOTPEnabled, &u.AIEnabled, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`,
		passwordHash, userID)
	return err
}

func (db *DB) UpdateUserTOTP(ctx context.Context, userID int64, secret *string, enabled bool) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE users SET totp_secret = $1, totp_enabled = $2, updated_at = NOW() WHERE id = $3`,
		secret, enabled, userID)
	return err
}

// --- Passkey operations ---

func (db *DB) CreatePasskey(ctx context.Context, p *models.Passkey) error {
	transportsJSON, _ := json.Marshal(p.Transports)
	return db.Pool.QueryRow(ctx,
		`INSERT INTO passkeys (user_id, credential_id, public_key, sign_count, transports, backup_eligible, backup_state, name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, created_at`,
		p.UserID, p.CredentialID, p.PublicKey, p.SignCount, string(transportsJSON), p.BackupEligible, p.BackupState, p.Name,
	).Scan(&p.ID, &p.CreatedAt)
}

func (db *DB) GetPasskeysByUserID(ctx context.Context, userID int64) ([]models.Passkey, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, user_id, credential_id, public_key, sign_count, transports, backup_eligible, backup_state, name, created_at, last_used_at
		 FROM passkeys WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var passkeys []models.Passkey
	for rows.Next() {
		var p models.Passkey
		var transportsJSON string
		if err := rows.Scan(&p.ID, &p.UserID, &p.CredentialID, &p.PublicKey, &p.SignCount, &transportsJSON, &p.BackupEligible, &p.BackupState, &p.Name, &p.CreatedAt, &p.LastUsedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(transportsJSON), &p.Transports)
		passkeys = append(passkeys, p)
	}
	return passkeys, rows.Err()
}

func (db *DB) GetPasskeyByCredentialID(ctx context.Context, credentialID string) (*models.Passkey, error) {
	p := &models.Passkey{}
	var transportsJSON string
	err := db.Pool.QueryRow(ctx,
		`SELECT id, user_id, credential_id, public_key, sign_count, transports, backup_eligible, backup_state, name, created_at, last_used_at
		 FROM passkeys WHERE credential_id = $1`, credentialID,
	).Scan(&p.ID, &p.UserID, &p.CredentialID, &p.PublicKey, &p.SignCount, &transportsJSON, &p.BackupEligible, &p.BackupState, &p.Name, &p.CreatedAt, &p.LastUsedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(transportsJSON), &p.Transports)
	return p, nil
}

func (db *DB) UpdatePasskeySignCount(ctx context.Context, id int64, signCount uint32) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE passkeys SET sign_count = $1, last_used_at = NOW() WHERE id = $2`,
		signCount, id)
	return err
}

func (db *DB) DeletePasskey(ctx context.Context, id, userID int64) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM passkeys WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// --- API Key operations ---

func (db *DB) CreateAPIKey(ctx context.Context, k *models.APIKeyRecord) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO api_keys (user_id, key_hash, name, prefix)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		k.UserID, k.KeyHash, k.Name, k.Prefix,
	).Scan(&k.ID, &k.CreatedAt)
}

func (db *DB) GetAPIKeysByUserID(ctx context.Context, userID int64) ([]models.APIKeyRecord, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, user_id, name, prefix, created_at, last_used_at
		 FROM api_keys WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []models.APIKeyRecord
	for rows.Next() {
		var k models.APIKeyRecord
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.CreatedAt, &k.LastUsedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (db *DB) GetUserByAPIKeyHash(ctx context.Context, keyHash string) (*models.User, error) {
	u := &models.User{}
	err := db.Pool.QueryRow(ctx,
		`SELECT u.id, u.username, u.password_hash, u.totp_secret, u.totp_enabled, u.ai_enabled, u.role, u.created_at, u.updated_at
		 FROM users u
		 JOIN api_keys ak ON ak.user_id = u.id
		 WHERE ak.key_hash = $1`, keyHash,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TOTPSecret, &u.TOTPEnabled, &u.AIEnabled, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	// Update last_used_at
	_, _ = db.Pool.Exec(ctx, `UPDATE api_keys SET last_used_at = NOW() WHERE key_hash = $1`, keyHash)
	return u, nil
}

func (db *DB) DeleteAPIKey(ctx context.Context, id, userID int64) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// --- Token blacklist operations ---

func (db *DB) BlacklistToken(ctx context.Context, tokenHash string, expiresAt time.Time) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO token_blacklist (token_hash, expires_at) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		tokenHash, expiresAt)
	return err
}

func (db *DB) IsTokenBlacklisted(ctx context.Context, tokenHash string) (bool, error) {
	var count int
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM token_blacklist WHERE token_hash = $1 AND expires_at > NOW()`,
		tokenHash,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (db *DB) CleanExpiredTokens(ctx context.Context) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM token_blacklist WHERE expires_at < NOW()`)
	return err
}

// --- Login attempt / rate limiting operations ---

func (db *DB) RecordLoginAttempt(ctx context.Context, username, ip string, success bool) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO login_attempts (username, ip_address, success, attempted_at) VALUES ($1, $2, $3, NOW())`,
		username, ip, success)
	return err
}

func (db *DB) CountRecentFailedAttempts(ctx context.Context, username, ip string, window time.Duration) (int, error) {
	var count int
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM login_attempts
		 WHERE (username = $1 OR ip_address = $2)
		 AND success = false
		 AND attempted_at > $3`,
		username, ip, time.Now().Add(-window),
	).Scan(&count)
	return count, err
}

// --- Admin user operations ---

// ListAllUsers returns all users (for admin use).
func (db *DB) ListAllUsers(ctx context.Context) ([]models.User, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, username, password_hash, totp_secret, totp_enabled, ai_enabled, role, created_at, updated_at
		 FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TOTPSecret, &u.TOTPEnabled, &u.AIEnabled, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// UpdateUserRole sets the role for a user.
func (db *DB) UpdateUserRole(ctx context.Context, userID int64, role string) error {
	tag, err := db.Pool.Exec(ctx,
		`UPDATE users SET role = $1, updated_at = NOW() WHERE id = $2`,
		role, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// UpdateUserAIEnabled sets the ai_enabled flag for a user.
func (db *DB) UpdateUserAIEnabled(ctx context.Context, userID int64, aiEnabled bool) error {
	tag, err := db.Pool.Exec(ctx,
		`UPDATE users SET ai_enabled = $1, updated_at = NOW() WHERE id = $2`,
		aiEnabled, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// GetUserAIEnabled returns whether AI is enabled for a user.
func (db *DB) GetUserAIEnabled(ctx context.Context, userID int64) (bool, error) {
	var aiEnabled bool
	err := db.Pool.QueryRow(ctx,
		`SELECT ai_enabled FROM users WHERE id = $1`, userID,
	).Scan(&aiEnabled)
	if err != nil {
		return false, err
	}
	return aiEnabled, nil
}

// GetUserIDByTenantID returns the user_id associated with a tenant.
func (db *DB) GetUserIDByTenantID(ctx context.Context, tenantID int64) (int64, error) {
	var userID *int64
	err := db.Pool.QueryRow(ctx,
		`SELECT user_id FROM tenants WHERE id = $1`, tenantID,
	).Scan(&userID)
	if err != nil {
		return 0, err
	}
	if userID == nil {
		return 0, fmt.Errorf("tenant has no associated user")
	}
	return *userID, nil
}

// --- Tenant-user association ---

func (db *DB) GetTenantsByUserID(ctx context.Context, userID int64) ([]models.Tenant, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, name, slug, api_key, created_at, updated_at FROM tenants WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []models.Tenant
	for rows.Next() {
		var t models.Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.APIKey, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tenants = append(tenants, t)
	}
	return tenants, rows.Err()
}

func (db *DB) AssignTenantToUser(ctx context.Context, tenantID, userID int64) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE tenants SET user_id = $1 WHERE id = $2`, userID, tenantID)
	return err
}

// CreateTenantForUser creates a default tenant for a user who doesn't have one.
func (db *DB) CreateTenantForUser(ctx context.Context, userID int64, username string) (*models.Tenant, error) {
	t := &models.Tenant{}
	slug := fmt.Sprintf("user-%d", userID)
	apiKey := fmt.Sprintf("auto_%d_%d", userID, time.Now().UnixNano())
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO tenants (name, slug, api_key, user_id) VALUES ($1, $2, $3, $4)
		 RETURNING id, name, slug, api_key, created_at, updated_at`,
		username+"'s workspace", slug, apiKey, userID,
	).Scan(&t.ID, &t.Name, &t.Slug, &t.APIKey, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}
