package db

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/crypto"
	"github.com/djpadz/email-automation/internal/models"
)

// DB wraps a pgx connection pool and provides data access methods.
type DB struct {
	Pool      *pgxpool.Pool
	Encryptor *crypto.Encryptor // nil = no encryption
}

// New creates a new database connection pool.
func New(ctx context.Context, databaseURL string) (*DB, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	config.MaxConns = 20
	config.MinConns = 2
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// SetEncryptor sets the password encryptor for account operations.
func (db *DB) SetEncryptor(enc *crypto.Encryptor) {
	db.Encryptor = enc
}

// encryptPassword encrypts a password if an encryptor is configured.
func (db *DB) encryptPassword(password string, accountID int64) (string, error) {
	if db.Encryptor == nil || password == "" {
		return password, nil
	}
	return db.Encryptor.Encrypt(password, accountID)
}

// decryptPassword decrypts a password if an encryptor is configured.
func (db *DB) decryptPassword(password string, accountID int64) (string, error) {
	if db.Encryptor == nil || password == "" {
		return password, nil
	}
	return db.Encryptor.Decrypt(password, accountID)
}

// Close shuts down the connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}

// RunMigrations applies SQL migration files from the given directory.
func (db *DB) RunMigrations(ctx context.Context, migrationsDir string) error {
	// Create migrations tracking table
	_, err := db.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	// Read migration files
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var upFiles []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			upFiles = append(upFiles, e.Name())
		}
	}
	sort.Strings(upFiles)

	for _, fname := range upFiles {
		version := strings.TrimSuffix(fname, ".up.sql")

		// Check if already applied
		var count int
		err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = $1", version).Scan(&count)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if count > 0 {
			continue
		}

		// Read and execute
		sql, err := os.ReadFile(filepath.Join(migrationsDir, fname))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", fname, err)
		}

		log.Info().Str("migration", version).Msg("applying migration")

		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("execute migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}

		log.Info().Str("migration", version).Msg("migration applied")
	}

	return nil
}

// --- Tenant operations ---

func (db *DB) CreateTenant(ctx context.Context, t *models.Tenant) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO tenants (name, slug, api_key) VALUES ($1, $2, $3)
		 RETURNING id, created_at, updated_at`,
		t.Name, t.Slug, t.APIKey,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

func (db *DB) GetTenant(ctx context.Context, id int64) (*models.Tenant, error) {
	t := &models.Tenant{}
	err := db.Pool.QueryRow(ctx,
		`SELECT id, name, slug, api_key, created_at, updated_at FROM tenants WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.Slug, &t.APIKey, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (db *DB) GetTenantByAPIKey(ctx context.Context, apiKey string) (*models.Tenant, error) {
	t := &models.Tenant{}
	err := db.Pool.QueryRow(ctx,
		`SELECT id, name, slug, api_key, created_at, updated_at FROM tenants WHERE api_key = $1`, apiKey,
	).Scan(&t.ID, &t.Name, &t.Slug, &t.APIKey, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (db *DB) ListTenants(ctx context.Context) ([]models.Tenant, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, name, slug, api_key, created_at, updated_at FROM tenants ORDER BY id`)
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

// --- Exempt folder operations ---

// GetExemptFolders returns the list of exempt folders for an account.
func (db *DB) GetExemptFolders(ctx context.Context, accountID int64) ([]string, error) {
	var exemptFoldersJSON []byte
	err := db.Pool.QueryRow(ctx,
		`SELECT exempt_folders FROM accounts WHERE id = $1`, accountID,
	).Scan(&exemptFoldersJSON)
	if err != nil {
		return nil, err
	}
	var folders []string
	if exemptFoldersJSON != nil {
		if err := json.Unmarshal(exemptFoldersJSON, &folders); err != nil {
			return nil, fmt.Errorf("unmarshal exempt_folders: %w", err)
		}
	}
	return folders, nil
}

// SetExemptFolders replaces the entire exempt folders list for an account.
func (db *DB) SetExemptFolders(ctx context.Context, accountID int64, folders []string) error {
	foldersJSON, err := json.Marshal(folders)
	if err != nil {
		return fmt.Errorf("marshal exempt_folders: %w", err)
	}
	_, err = db.Pool.Exec(ctx,
		`UPDATE accounts SET exempt_folders = $1, updated_at = NOW() WHERE id = $2`,
		foldersJSON, accountID)
	return err
}

// AddExemptFolder adds a folder to the exempt list for an account if not already present.
func (db *DB) AddExemptFolder(ctx context.Context, accountID int64, folder string) ([]string, error) {
	folders, err := db.GetExemptFolders(ctx, accountID)
	if err != nil {
		return nil, err
	}
	// Check if already present (case-insensitive)
	for _, f := range folders {
		if strings.EqualFold(f, folder) {
			return folders, nil // already exempt
		}
	}
	folders = append(folders, folder)
	if err := db.SetExemptFolders(ctx, accountID, folders); err != nil {
		return nil, err
	}
	return folders, nil
}

// RemoveExemptFolder removes a folder from the exempt list for an account.
func (db *DB) RemoveExemptFolder(ctx context.Context, accountID int64, folder string) ([]string, error) {
	folders, err := db.GetExemptFolders(ctx, accountID)
	if err != nil {
		return nil, err
	}
	var updated []string
	for _, f := range folders {
		if !strings.EqualFold(f, folder) {
			updated = append(updated, f)
		}
	}
	if len(updated) == len(folders) {
		return nil, fmt.Errorf("folder %q not found in exempt list", folder)
	}
	if updated == nil {
		updated = []string{}
	}
	if err := db.SetExemptFolders(ctx, accountID, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

// IsFolderExempt checks if a folder name is in the account's exempt list (case-insensitive).
func (db *DB) IsFolderExempt(ctx context.Context, accountID int64, folder string) (bool, error) {
	folders, err := db.GetExemptFolders(ctx, accountID)
	if err != nil {
		return false, err
	}
	for _, f := range folders {
		if strings.EqualFold(f, folder) {
			return true, nil
		}
	}
	return false, nil
}

// --- Account operations ---

func (db *DB) CreateAccount(ctx context.Context, a *models.Account) error {
	// We need the ID for encryption, so insert first with empty password,
	// then encrypt and update. Or use a two-step approach.
	// Actually, we can insert with plaintext first, get the ID, then encrypt+update.
	// Better: insert with placeholder, get ID, encrypt, update.
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO accounts (tenant_id, name, email, provider, imap_host, imap_port, imap_tls, username, password, oauth_token, oauth_refresh_token, oauth_token_expiry, oauth_provider, active)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING id, created_at, updated_at`,
		a.TenantID, a.Name, a.Email, a.Provider, a.IMAPHost, a.IMAPPort, a.IMAPTLS, a.Username, a.Password, a.OAuthToken, a.OAuthRefreshToken, a.OAuthTokenExpiry, a.OAuthProvider, a.Active,
	).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return err
	}

	// Now encrypt the password with the account ID and update
	if db.Encryptor != nil && a.Password != "" {
		encrypted, encErr := db.encryptPassword(a.Password, a.ID)
		if encErr != nil {
			return fmt.Errorf("encrypt password: %w", encErr)
		}
		_, err = db.Pool.Exec(ctx,
			`UPDATE accounts SET password = $1 WHERE id = $2`, encrypted, a.ID)
		if err != nil {
			return fmt.Errorf("update encrypted password: %w", err)
		}
	}

	// Encrypt OAuth tokens
	if db.Encryptor != nil {
		var needsUpdate bool
		updates := make(map[string]string)
		if a.OAuthToken != "" {
			if enc, encErr := db.encryptPassword(a.OAuthToken, a.ID); encErr == nil {
				updates["oauth_token"] = enc
				needsUpdate = true
			}
		}
		if a.OAuthRefreshToken != "" {
			if enc, encErr := db.encryptPassword(a.OAuthRefreshToken, a.ID); encErr == nil {
				updates["oauth_refresh_token"] = enc
				needsUpdate = true
			}
		}
		if needsUpdate {
			for col, val := range updates {
				_, _ = db.Pool.Exec(ctx,
					fmt.Sprintf(`UPDATE accounts SET %s = $1 WHERE id = $2`, col), val, a.ID)
			}
		}
	}
	return nil
}

func (db *DB) GetAccount(ctx context.Context, tenantID, id int64) (*models.Account, error) {
	a := &models.Account{}
	err := db.Pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, email, provider, imap_host, imap_port, imap_tls, username, password, oauth_token, oauth_refresh_token, oauth_token_expiry, oauth_provider, active, last_sync_at, created_at, updated_at
		 FROM accounts WHERE id = $1 AND tenant_id = $2`, id, tenantID,
	).Scan(&a.ID, &a.TenantID, &a.Name, &a.Email, &a.Provider, &a.IMAPHost, &a.IMAPPort, &a.IMAPTLS, &a.Username, &a.Password, &a.OAuthToken, &a.OAuthRefreshToken, &a.OAuthTokenExpiry, &a.OAuthProvider, &a.Active, &a.LastSyncAt, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	// Decrypt password
	if decrypted, decErr := db.decryptPassword(a.Password, a.ID); decErr == nil {
		a.Password = decrypted
	}
	// Decrypt OAuth tokens
	if decrypted, decErr := db.decryptPassword(a.OAuthToken, a.ID); decErr == nil {
		a.OAuthToken = decrypted
	}
	if decrypted, decErr := db.decryptPassword(a.OAuthRefreshToken, a.ID); decErr == nil {
		a.OAuthRefreshToken = decrypted
	}
	return a, nil
}

func (db *DB) ListAccounts(ctx context.Context, tenantID int64) ([]models.Account, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, tenant_id, name, email, provider, imap_host, imap_port, imap_tls, username, oauth_provider, active, last_sync_at, last_connection_test_at, last_connection_status, last_connection_error, created_at, updated_at
		 FROM accounts WHERE tenant_id = $1 ORDER BY id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []models.Account
	for rows.Next() {
		var a models.Account
		var connStatus, connError *string
		if err := rows.Scan(&a.ID, &a.TenantID, &a.Name, &a.Email, &a.Provider, &a.IMAPHost, &a.IMAPPort, &a.IMAPTLS, &a.Username, &a.OAuthProvider, &a.Active, &a.LastSyncAt, &a.LastConnectionTestAt, &connStatus, &connError, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		if connStatus != nil {
			a.LastConnectionStatus = *connStatus
		}
		if connError != nil {
			a.LastConnectionError = *connError
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func (db *DB) ListActiveAccounts(ctx context.Context) ([]models.Account, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, tenant_id, name, email, provider, imap_host, imap_port, imap_tls, username, password, oauth_token, oauth_refresh_token, oauth_token_expiry, oauth_provider, active, last_sync_at, created_at, updated_at
		 FROM accounts WHERE active = true ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []models.Account
	for rows.Next() {
		var a models.Account
		if err := rows.Scan(&a.ID, &a.TenantID, &a.Name, &a.Email, &a.Provider, &a.IMAPHost, &a.IMAPPort, &a.IMAPTLS, &a.Username, &a.Password, &a.OAuthToken, &a.OAuthRefreshToken, &a.OAuthTokenExpiry, &a.OAuthProvider, &a.Active, &a.LastSyncAt, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		// Decrypt password
		if decrypted, decErr := db.decryptPassword(a.Password, a.ID); decErr == nil {
			a.Password = decrypted
		}
		// Decrypt OAuth tokens
		if decrypted, decErr := db.decryptPassword(a.OAuthToken, a.ID); decErr == nil {
			a.OAuthToken = decrypted
		}
		if decrypted, decErr := db.decryptPassword(a.OAuthRefreshToken, a.ID); decErr == nil {
			a.OAuthRefreshToken = decrypted
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func (db *DB) UpdateAccount(ctx context.Context, a *models.Account) error {
	// Encrypt password before storing
	password := a.Password
	if encrypted, err := db.encryptPassword(a.Password, a.ID); err == nil {
		password = encrypted
	}
	// Encrypt OAuth tokens
	oauthToken := a.OAuthToken
	if db.Encryptor != nil && a.OAuthToken != "" {
		if encrypted, err := db.encryptPassword(a.OAuthToken, a.ID); err == nil {
			oauthToken = encrypted
		}
	}
	oauthRefreshToken := a.OAuthRefreshToken
	if db.Encryptor != nil && a.OAuthRefreshToken != "" {
		if encrypted, err := db.encryptPassword(a.OAuthRefreshToken, a.ID); err == nil {
			oauthRefreshToken = encrypted
		}
	}
	_, err := db.Pool.Exec(ctx,
		`UPDATE accounts SET name=$1, email=$2, provider=$3, imap_host=$4, imap_port=$5, imap_tls=$6, username=$7, password=$8, oauth_token=$9, oauth_refresh_token=$10, oauth_token_expiry=$11, oauth_provider=$12, active=$13, updated_at=NOW()
		 WHERE id=$14 AND tenant_id=$15`,
		a.Name, a.Email, a.Provider, a.IMAPHost, a.IMAPPort, a.IMAPTLS, a.Username, password, oauthToken, oauthRefreshToken, a.OAuthTokenExpiry, a.OAuthProvider, a.Active, a.ID, a.TenantID)
	return err
}

func (db *DB) DeleteAccount(ctx context.Context, tenantID, id int64) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM accounts WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return err
}

// UpdateAccountOAuthTokens updates only the OAuth2 tokens for an account.
// Used during token refresh to avoid overwriting other fields.
func (db *DB) UpdateAccountOAuthTokens(ctx context.Context, accountID int64, accessToken, refreshToken string, expiry *time.Time) error {
	// Encrypt tokens
	encAccessToken := accessToken
	if db.Encryptor != nil && accessToken != "" {
		if encrypted, err := db.encryptPassword(accessToken, accountID); err == nil {
			encAccessToken = encrypted
		}
	}
	encRefreshToken := refreshToken
	if db.Encryptor != nil && refreshToken != "" {
		if encrypted, err := db.encryptPassword(refreshToken, accountID); err == nil {
			encRefreshToken = encrypted
		}
	}
	_, err := db.Pool.Exec(ctx,
		`UPDATE accounts SET oauth_token=$1, oauth_refresh_token=$2, oauth_token_expiry=$3, updated_at=NOW() WHERE id=$4`,
		encAccessToken, encRefreshToken, expiry, accountID)
	return err
}

// GetActiveAccountByID retrieves a single active account by ID (no tenant filter).
// Used by IMAP workers to refresh credentials without knowing the tenant.
func (db *DB) GetActiveAccountByID(ctx context.Context, accountID int64) (*models.Account, error) {
	a := &models.Account{}
	err := db.Pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, email, provider, imap_host, imap_port, imap_tls, username, password, oauth_token, oauth_refresh_token, oauth_token_expiry, oauth_provider, active, last_sync_at, created_at, updated_at
		 FROM accounts WHERE id = $1 AND active = true`, accountID,
	).Scan(&a.ID, &a.TenantID, &a.Name, &a.Email, &a.Provider, &a.IMAPHost, &a.IMAPPort, &a.IMAPTLS, &a.Username, &a.Password, &a.OAuthToken, &a.OAuthRefreshToken, &a.OAuthTokenExpiry, &a.OAuthProvider, &a.Active, &a.LastSyncAt, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	// Decrypt password
	if decrypted, decErr := db.decryptPassword(a.Password, a.ID); decErr == nil {
		a.Password = decrypted
	}
	// Decrypt OAuth tokens
	if decrypted, decErr := db.decryptPassword(a.OAuthToken, a.ID); decErr == nil {
		a.OAuthToken = decrypted
	}
	if decrypted, decErr := db.decryptPassword(a.OAuthRefreshToken, a.ID); decErr == nil {
		a.OAuthRefreshToken = decrypted
	}
	return a, nil
}

func (db *DB) UpdateAccountSyncTime(ctx context.Context, accountID int64) error {
	_, err := db.Pool.Exec(ctx, `UPDATE accounts SET last_sync_at = NOW() WHERE id = $1`, accountID)
	return err
}

// GetLastUIDProcessed returns the last UID processed for an account.
func (db *DB) GetLastUIDProcessed(ctx context.Context, accountID int64) (uint32, error) {
	var uid int64
	err := db.Pool.QueryRow(ctx,
		`SELECT COALESCE(last_uid_processed, 0) FROM accounts WHERE id = $1`, accountID,
	).Scan(&uid)
	if err != nil {
		return 0, err
	}
	return uint32(uid), nil
}

// UpdateLastUIDProcessed sets the last UID processed for an account.
func (db *DB) UpdateLastUIDProcessed(ctx context.Context, accountID int64, uid uint32) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE accounts SET last_uid_processed = $1, last_sync_at = NOW() WHERE id = $2`,
		int64(uid), accountID)
	return err
}

// --- Rule operations ---

func (db *DB) CreateRule(ctx context.Context, r *models.Rule) error {
	if r.Source == "" {
		r.Source = "manual"
	}
	return db.Pool.QueryRow(ctx,
		`INSERT INTO rules (tenant_id, name, description, lua_code, priority, active, source, approved, uses_ai)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, created_at, updated_at`,
		r.TenantID, r.Name, r.Description, r.LuaCode, r.Priority, r.Active, r.Source, r.Approved, r.UsesAI,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
}

func (db *DB) GetRule(ctx context.Context, tenantID, id int64) (*models.Rule, error) {
	r := &models.Rule{}
	err := db.Pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, description, lua_code, priority, active, source, approved, uses_ai, created_at, updated_at
		 FROM rules WHERE id = $1 AND tenant_id = $2`, id, tenantID,
	).Scan(&r.ID, &r.TenantID, &r.Name, &r.Description, &r.LuaCode, &r.Priority, &r.Active, &r.Source, &r.Approved, &r.UsesAI, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (db *DB) ListRules(ctx context.Context, tenantID int64) ([]models.Rule, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, tenant_id, name, description, lua_code, priority, active, source, approved, uses_ai, created_at, updated_at
		 FROM rules WHERE tenant_id = $1 ORDER BY priority, id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []models.Rule
	for rows.Next() {
		var r models.Rule
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &r.Description, &r.LuaCode, &r.Priority, &r.Active, &r.Source, &r.Approved, &r.UsesAI, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (db *DB) ListActiveRules(ctx context.Context, tenantID int64) ([]models.Rule, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, tenant_id, name, description, lua_code, priority, active, source, approved, uses_ai, created_at, updated_at
		 FROM rules WHERE tenant_id = $1 AND active = true AND approved = true ORDER BY priority, id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []models.Rule
	for rows.Next() {
		var r models.Rule
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &r.Description, &r.LuaCode, &r.Priority, &r.Active, &r.Source, &r.Approved, &r.UsesAI, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (db *DB) UpdateRule(ctx context.Context, r *models.Rule) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE rules SET name=$1, description=$2, lua_code=$3, priority=$4, active=$5, source=$6, approved=$7, uses_ai=$8, updated_at=NOW()
		 WHERE id=$9 AND tenant_id=$10`,
		r.Name, r.Description, r.LuaCode, r.Priority, r.Active, r.Source, r.Approved, r.UsesAI, r.ID, r.TenantID)
	return err
}

func (db *DB) DeleteRule(ctx context.Context, tenantID, id int64) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM rules WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return err
}

// --- Execution log operations ---

func (db *DB) LogExecution(ctx context.Context, l *models.RuleExecutionLog) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO rule_execution_log (rule_id, account_id, message_id, subject, sender, action, target, success, error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, executed_at`,
		l.RuleID, l.AccountID, l.MessageID, l.Subject, l.Sender, l.Action, l.Target, l.Success, l.Error,
	).Scan(&l.ID, &l.ExecutedAt)
}

func (db *DB) ListExecutionLogs(ctx context.Context, tenantID int64, limit int) ([]models.RuleExecutionLog, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT l.id, l.rule_id, l.account_id, l.message_id, l.subject, l.sender, l.action, l.target, l.success, l.error, l.executed_at
		 FROM rule_execution_log l
		 JOIN rules r ON r.id = l.rule_id
		 WHERE r.tenant_id = $1
		 ORDER BY l.executed_at DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.RuleExecutionLog
	for rows.Next() {
		var l models.RuleExecutionLog
		if err := rows.Scan(&l.ID, &l.RuleID, &l.AccountID, &l.MessageID, &l.Subject, &l.Sender, &l.Action, &l.Target, &l.Success, &l.Error, &l.ExecutedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// --- Deferred action operations ---

func (db *DB) CreateDeferredAction(ctx context.Context, d *models.DeferredAction) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO deferred_actions (rule_id, account_id, message_id, action, target, execute_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at`,
		d.RuleID, d.AccountID, d.MessageID, d.Action, d.Target, d.ExecuteAt,
	).Scan(&d.ID, &d.CreatedAt)
}

func (db *DB) GetPendingDeferredActions(ctx context.Context) ([]models.DeferredAction, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, rule_id, account_id, message_id, action, target, execute_at, created_at
		 FROM deferred_actions
		 WHERE status = 'pending' AND execute_at <= NOW()
		 ORDER BY execute_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []models.DeferredAction
	for rows.Next() {
		var d models.DeferredAction
		if err := rows.Scan(&d.ID, &d.RuleID, &d.AccountID, &d.MessageID, &d.Action, &d.Target, &d.ExecuteAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		actions = append(actions, d)
	}
	return actions, rows.Err()
}

func (db *DB) MarkDeferredActionDone(ctx context.Context, id int64, errMsg string) error {
	status := "executed"
	if errMsg != "" {
		status = "failed"
	}
	_, err := db.Pool.Exec(ctx,
		`UPDATE deferred_actions SET executed = true, status = $2, executed_at = NOW(), error = $3 WHERE id = $1`, id, status, errMsg)
	return err
}

// ListDeferredActions returns all pending (not yet executed) deferred actions for a tenant.
func (db *DB) ListDeferredActions(ctx context.Context, tenantID int64) ([]models.DeferredAction, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT d.id, d.rule_id, d.account_id, d.message_id, d.action, d.target, d.execute_at, d.executed, d.error, d.created_at
		 FROM deferred_actions d
		 JOIN accounts a ON a.id = d.account_id
		 WHERE a.tenant_id = $1 AND d.status = 'pending'
		 ORDER BY d.execute_at ASC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []models.DeferredAction
	for rows.Next() {
		var d models.DeferredAction
		if err := rows.Scan(&d.ID, &d.RuleID, &d.AccountID, &d.MessageID, &d.Action, &d.Target, &d.ExecuteAt, &d.Executed, &d.Error, &d.CreatedAt); err != nil {
			return nil, err
		}
		actions = append(actions, d)
	}
	return actions, rows.Err()
}

// CancelDeferredAction deletes a pending deferred action for a tenant.
func (db *DB) CancelDeferredAction(ctx context.Context, tenantID int64, actionID int64) error {
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM deferred_actions d
		 USING accounts a
		 WHERE d.id = $1 AND d.account_id = a.id AND a.tenant_id = $2 AND d.status = 'pending'`,
		actionID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("deferred action not found or already executed")
	}
	return nil
}

// ReorderRules updates rule priorities based on the provided order of rule IDs.
// The first ID gets priority 10, second gets 20, etc.
func (db *DB) ReorderRules(ctx context.Context, tenantID int64, ruleIDs []int64) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for i, id := range ruleIDs {
		priority := (i + 1) * 10
		tag, err := tx.Exec(ctx,
			`UPDATE rules SET priority = $1, updated_at = NOW() WHERE id = $2 AND tenant_id = $3`,
			priority, id, tenantID)
		if err != nil {
			return fmt.Errorf("update rule %d: %w", id, err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("rule %d not found for tenant", id)
		}
	}

	return tx.Commit(ctx)
}

// --- Processed message operations ---

func (db *DB) IsMessageProcessed(ctx context.Context, accountID int64, messageUID string) (bool, error) {
	var count int
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM processed_messages WHERE account_id = $1 AND message_uid = $2`,
		accountID, messageUID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// IsMessageMovedByRule checks if a message was specifically moved by the rules engine.
// This is more specific than IsMessageProcessed, which returns true for any action
// (including "flag"). Use this when deciding whether to skip rule creation from
// user-initiated moves — a flagged message that the user later moves manually
// should still trigger rule creation.
func (db *DB) IsMessageMovedByRule(ctx context.Context, accountID int64, messageUID string) (bool, error) {
	var count int
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM processed_messages WHERE account_id = $1 AND message_uid = $2 AND action = 'move'`,
		accountID, messageUID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (db *DB) MarkMessageProcessed(ctx context.Context, accountID int64, messageUID string, ruleID int64, action string) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO processed_messages (account_id, message_uid, rule_id, action)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (account_id, message_uid) DO NOTHING`,
		accountID, messageUID, ruleID, action)
	return err
}

// --- Suggested/auto-learned rule operations ---

// ApproveRule marks an auto-learned rule as approved so it becomes active.
func (db *DB) ApproveRule(ctx context.Context, tenantID, id int64) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE rules SET approved = true, updated_at = NOW() WHERE id = $1 AND tenant_id = $2`,
		id, tenantID)
	return err
}

// ListSuggestedRules returns unapproved auto-learned rules for a tenant.
func (db *DB) ListSuggestedRules(ctx context.Context, tenantID int64) ([]models.Rule, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, tenant_id, name, description, lua_code, priority, active, source, approved, uses_ai, created_at, updated_at
		 FROM rules WHERE tenant_id = $1 AND approved = false ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []models.Rule
	for rows.Next() {
		var r models.Rule
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &r.Description, &r.LuaCode, &r.Priority, &r.Active, &r.Source, &r.Approved, &r.UsesAI, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// --- Message location tracking ---

// UpsertMessageLocation records or updates a message's known folder location.
func (db *DB) UpsertMessageLocation(ctx context.Context, loc *models.MessageLocation) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO message_locations (account_id, message_uid, folder, message_id, sender, subject, seen_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())
		 ON CONFLICT (account_id, message_uid, folder) DO UPDATE SET seen_at = NOW()`,
		loc.AccountID, loc.MessageUID, loc.Folder, loc.MessageID, loc.Sender, loc.Subject)
	return err
}

// GetMessageLocations returns all known locations for a message.
func (db *DB) GetMessageLocations(ctx context.Context, accountID int64, messageUID string) ([]models.MessageLocation, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT account_id, message_uid, folder, message_id, sender, subject, seen_at
		 FROM message_locations WHERE account_id = $1 AND message_uid = $2 ORDER BY seen_at`,
		accountID, messageUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locs []models.MessageLocation
	for rows.Next() {
		var l models.MessageLocation
		if err := rows.Scan(&l.AccountID, &l.MessageUID, &l.Folder, &l.MessageID, &l.Sender, &l.Subject, &l.SeenAt); err != nil {
			return nil, err
		}
		locs = append(locs, l)
	}
	return locs, rows.Err()
}

// --- Detected move operations ---

// RecordDetectedMove logs a detected message move between folders.
func (db *DB) RecordDetectedMove(ctx context.Context, m *models.DetectedMove) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO detected_moves (account_id, message_uid, message_id, sender, subject, from_folder, to_folder)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, detected_at`,
		m.AccountID, m.MessageUID, m.MessageID, m.Sender, m.Subject, m.FromFolder, m.ToFolder,
	).Scan(&m.ID, &m.DetectedAt)
}

// ListDetectedMoves returns recent detected moves for a tenant's accounts.
func (db *DB) ListDetectedMoves(ctx context.Context, tenantID int64, limit int) ([]models.DetectedMove, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT d.id, d.account_id, d.message_uid, d.message_id, d.sender, d.subject, d.from_folder, d.to_folder, d.detected_at, d.rule_id
		 FROM detected_moves d
		 JOIN accounts a ON a.id = d.account_id
		 WHERE a.tenant_id = $1
		 ORDER BY d.detected_at DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var moves []models.DetectedMove
	for rows.Next() {
		var m models.DetectedMove
		if err := rows.Scan(&m.ID, &m.AccountID, &m.MessageUID, &m.MessageID, &m.Sender, &m.Subject, &m.FromFolder, &m.ToFolder, &m.DetectedAt, &m.RuleID); err != nil {
			return nil, err
		}
		moves = append(moves, m)
	}
	return moves, rows.Err()
}

// CleanOldMessageLocations removes message location records older than the given duration.
func (db *DB) CleanOldMessageLocations(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM message_locations WHERE seen_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// --- Rule-applied move tracking (feedback loop prevention) ---

// RecordRuleAppliedMove records that a rule moved a message to a folder.
// The watcher checks this table to avoid creating rules from rule-driven moves.
func (db *DB) RecordRuleAppliedMove(ctx context.Context, ruleID, accountID int64, messageID, folder string) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO rule_applied_moves (rule_id, message_id, account_id, folder)
		 VALUES ($1, $2, $3, $4)`,
		ruleID, messageID, accountID, folder)
	return err
}

// IsRuleAppliedMove checks if a message was recently moved by a rule (within 24 hours).
// Returns true if the move was rule-driven and should NOT trigger new rule creation.
func (db *DB) IsRuleAppliedMove(ctx context.Context, accountID int64, messageID string) (bool, error) {
	var count int
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM rule_applied_moves
		 WHERE account_id = $1 AND message_id = $2 AND created_at > NOW() - INTERVAL '24 hours'`,
		accountID, messageID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// UpdateConnectionTestResult stores the result of a connection test for an account.
func (db *DB) UpdateConnectionTestResult(ctx context.Context, accountID int64, status string, errMsg string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE accounts SET last_connection_test_at = NOW(), last_connection_status = $1, last_connection_error = $2, updated_at = NOW() WHERE id = $3`,
		status, errMsg, accountID)
	return err
}

// CleanOldRuleAppliedMoves removes rule_applied_moves entries older than 24 hours.
func (db *DB) CleanOldRuleAppliedMoves(ctx context.Context) (int64, error) {
	cutoff := time.Now().Add(-24 * time.Hour)
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM rule_applied_moves WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// --- Bulk rule operations ---

// BulkDeleteRules deletes multiple rules by ID within a transaction.
// Returns the number of rules actually deleted.
func (db *DB) BulkDeleteRules(ctx context.Context, tenantID int64, ruleIDs []int64) (int64, error) {
	if len(ruleIDs) == 0 {
		return 0, nil
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var totalDeleted int64
	for _, id := range ruleIDs {
		tag, err := tx.Exec(ctx,
			`DELETE FROM rules WHERE id = $1 AND tenant_id = $2`, id, tenantID)
		if err != nil {
			return 0, fmt.Errorf("delete rule %d: %w", id, err)
		}
		totalDeleted += tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return totalDeleted, nil
}

// RotateEncryptionKeys re-encrypts all account passwords with the current master key version.
// Returns the number of passwords rotated.
func (db *DB) RotateEncryptionKeys(ctx context.Context) (int, error) {
	if db.Encryptor == nil {
		return 0, fmt.Errorf("no encryptor configured")
	}

	rows, err := db.Pool.Query(ctx,
		`SELECT id, password FROM accounts WHERE password != '' AND password IS NOT NULL`)
	if err != nil {
		return 0, fmt.Errorf("query accounts: %w", err)
	}
	defer rows.Close()

	type accountPassword struct {
		ID       int64
		Password string
	}

	var toRotate []accountPassword
	for rows.Next() {
		var ap accountPassword
		if err := rows.Scan(&ap.ID, &ap.Password); err != nil {
			return 0, fmt.Errorf("scan account: %w", err)
		}
		if db.Encryptor.NeedsRotation(ap.Password) {
			toRotate = append(toRotate, ap)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate accounts: %w", err)
	}

	rotated := 0
	for _, ap := range toRotate {
		// Decrypt with old key
		plaintext, err := db.Encryptor.Decrypt(ap.Password, ap.ID)
		if err != nil {
			log.Error().Err(err).Int64("account_id", ap.ID).Msg("failed to decrypt password during rotation")
			continue
		}

		// Re-encrypt with current key
		encrypted, err := db.Encryptor.Encrypt(plaintext, ap.ID)
		if err != nil {
			log.Error().Err(err).Int64("account_id", ap.ID).Msg("failed to re-encrypt password during rotation")
			continue
		}

		_, err = db.Pool.Exec(ctx,
			`UPDATE accounts SET password = $1, updated_at = NOW() WHERE id = $2`,
			encrypted, ap.ID)
		if err != nil {
			log.Error().Err(err).Int64("account_id", ap.ID).Msg("failed to update rotated password")
			continue
		}

		rotated++
		log.Info().Int64("account_id", ap.ID).Msg("rotated encryption key for account")
	}

	return rotated, nil
}

// --- Email metadata operations ---

// AttachMetadata attaches a key-value metadata tag to an email.
// If the key already exists for this message, the value is updated.
func (db *DB) AttachMetadata(ctx context.Context, tenantID int64, messageID, key, value string) (*models.MetadataRecord, error) {
	m := &models.MetadataRecord{}
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO email_metadata (tenant_id, message_id, key, value)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (tenant_id, message_id, key) DO UPDATE SET value = $4, updated_at = NOW()
		 RETURNING id, tenant_id, message_id, key, value, created_at, updated_at`,
		tenantID, messageID, key, value,
	).Scan(&m.ID, &m.TenantID, &m.MessageID, &m.Key, &m.Value, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("attach metadata: %w", err)
	}
	return m, nil
}

// GetMetadataForMessage returns all metadata tags for a specific email.
func (db *DB) GetMetadataForMessage(ctx context.Context, tenantID int64, messageID string) ([]models.MetadataRecord, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, tenant_id, message_id, key, value, created_at, updated_at
		 FROM email_metadata WHERE tenant_id = $1 AND message_id = $2
		 ORDER BY key`, tenantID, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []models.MetadataRecord
	for rows.Next() {
		var r models.MetadataRecord
		if err := rows.Scan(&r.ID, &r.TenantID, &r.MessageID, &r.Key, &r.Value, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// UpdateMetadata updates the value of a specific metadata key for an email.
func (db *DB) UpdateMetadata(ctx context.Context, tenantID int64, messageID, key, value string) (*models.MetadataRecord, error) {
	m := &models.MetadataRecord{}
	err := db.Pool.QueryRow(ctx,
		`UPDATE email_metadata SET value = $1, updated_at = NOW()
		 WHERE tenant_id = $2 AND message_id = $3 AND key = $4
		 RETURNING id, tenant_id, message_id, key, value, created_at, updated_at`,
		value, tenantID, messageID, key,
	).Scan(&m.ID, &m.TenantID, &m.MessageID, &m.Key, &m.Value, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update metadata: %w", err)
	}
	return m, nil
}

// RemoveMetadata removes a specific metadata key from an email.
func (db *DB) RemoveMetadata(ctx context.Context, tenantID int64, messageID, key string) error {
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM email_metadata WHERE tenant_id = $1 AND message_id = $2 AND key = $3`,
		tenantID, messageID, key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("metadata key not found")
	}
	return nil
}

// QueryByMetadata returns message IDs that have a specific metadata key-value pair.
// Optionally excludes a specific message ID from results.
func (db *DB) QueryByMetadata(ctx context.Context, tenantID int64, key, value string, excludeMessageID string) ([]models.MetadataRecord, error) {
	var query string
	var args []interface{}

	if excludeMessageID != "" {
		query = `SELECT id, tenant_id, message_id, key, value, created_at, updated_at
			 FROM email_metadata
			 WHERE tenant_id = $1 AND key = $2 AND value = $3 AND message_id != $4
			 ORDER BY created_at DESC`
		args = []interface{}{tenantID, key, value, excludeMessageID}
	} else {
		query = `SELECT id, tenant_id, message_id, key, value, created_at, updated_at
			 FROM email_metadata
			 WHERE tenant_id = $1 AND key = $2 AND value = $3
			 ORDER BY created_at DESC`
		args = []interface{}{tenantID, key, value}
	}

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []models.MetadataRecord
	for rows.Next() {
		var r models.MetadataRecord
		if err := rows.Scan(&r.ID, &r.TenantID, &r.MessageID, &r.Key, &r.Value, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// BatchAttachMetadata attaches the same key-value metadata to multiple emails.
func (db *DB) BatchAttachMetadata(ctx context.Context, tenantID int64, messageIDs []string, key, value string) ([]models.MetadataRecord, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var records []models.MetadataRecord
	for _, msgID := range messageIDs {
		var m models.MetadataRecord
		err := tx.QueryRow(ctx,
			`INSERT INTO email_metadata (tenant_id, message_id, key, value)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (tenant_id, message_id, key) DO UPDATE SET value = $4, updated_at = NOW()
			 RETURNING id, tenant_id, message_id, key, value, created_at, updated_at`,
			tenantID, msgID, key, value,
		).Scan(&m.ID, &m.TenantID, &m.MessageID, &m.Key, &m.Value, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("attach metadata to %s: %w", msgID, err)
		}
		records = append(records, m)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return records, nil
}

// BatchQueryMetadata queries multiple metadata keys at once for a tenant.
func (db *DB) BatchQueryMetadata(ctx context.Context, tenantID int64, queries []struct{ Key, Value string }) (map[string][]models.MetadataRecord, error) {
	results := make(map[string][]models.MetadataRecord)

	for _, q := range queries {
		rows, err := db.Pool.Query(ctx,
			`SELECT id, tenant_id, message_id, key, value, created_at, updated_at
			 FROM email_metadata
			 WHERE tenant_id = $1 AND key = $2 AND value = $3
			 ORDER BY created_at DESC`,
			tenantID, q.Key, q.Value)
		if err != nil {
			return nil, err
		}

		mapKey := q.Key + "=" + q.Value
		for rows.Next() {
			var r models.MetadataRecord
			if err := rows.Scan(&r.ID, &r.TenantID, &r.MessageID, &r.Key, &r.Value, &r.CreatedAt, &r.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			results[mapKey] = append(results[mapKey], r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	return results, nil
}
