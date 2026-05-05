package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/config"
	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/models"
	"github.com/djpadz/email-automation/internal/oauth2"
)

// OAuth2Handlers holds dependencies for OAuth2 HTTP handlers.
type OAuth2Handlers struct {
	DB        *db.DB
	Config    *config.Config
	providers map[string]oauth2.Provider
	// In-memory state store (maps state → tenantID)
	// In production, use Redis or DB-backed sessions.
	states map[string]oauthState
}

type oauthState struct {
	TenantID  int64
	Provider  string
	CreatedAt time.Time
}

// NewOAuth2Handlers creates new OAuth2 handlers with configured providers.
func NewOAuth2Handlers(database *db.DB, cfg *config.Config) *OAuth2Handlers {
	h := &OAuth2Handlers{
		DB:        database,
		Config:    cfg,
		providers: make(map[string]oauth2.Provider),
		states:    make(map[string]oauthState),
	}

	baseURL := cfg.OAuth2RedirectBaseURL

	// Register Microsoft 365 provider
	if cfg.OAuth2Microsoft365ClientID != "" && cfg.OAuth2Microsoft365ClientSecret != "" {
		redirectURI := baseURL + "/api/oauth2/callback/microsoft365"
		h.providers["microsoft365"] = oauth2.NewMicrosoft365Provider(
			cfg.OAuth2Microsoft365ClientID,
			cfg.OAuth2Microsoft365ClientSecret,
			cfg.OAuth2Microsoft365TenantID,
			redirectURI,
		)
		log.Info().Msg("OAuth2: Microsoft 365 provider configured")
	}

	// Register Gmail provider
	if cfg.OAuth2GmailClientID != "" && cfg.OAuth2GmailClientSecret != "" {
		redirectURI := baseURL + "/api/oauth2/callback/gmail"
		h.providers["gmail"] = oauth2.NewGmailProvider(
			cfg.OAuth2GmailClientID,
			cfg.OAuth2GmailClientSecret,
			redirectURI,
		)
		log.Info().Msg("OAuth2: Gmail provider configured")
	}

	return h
}

// ListProviders returns the available OAuth2 providers.
func (h *OAuth2Handlers) ListProviders(c *fiber.Ctx) error {
	var providers []fiber.Map
	for name := range h.providers {
		providers = append(providers, fiber.Map{
			"name": name,
		})
	}
	return c.JSON(fiber.Map{"providers": providers})
}

// Connect initiates the OAuth2 flow for a provider.
// GET /api/v1/oauth2/connect/:provider
func (h *OAuth2Handlers) Connect(c *fiber.Ctx) error {
	providerName := c.Params("provider")
	provider, ok := h.providers[providerName]
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fmt.Sprintf("unsupported OAuth2 provider: %s", providerName),
		})
	}

	tenantID := h.tenantID(c)
	if tenantID == 0 {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	// Generate state token
	state, err := generateState()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to generate state"})
	}

	// Store state → tenant mapping
	h.states[state] = oauthState{
		TenantID:  tenantID,
		Provider:  providerName,
		CreatedAt: time.Now(),
	}

	// Clean up old states (older than 10 minutes)
	h.cleanupStates()

	authURL := provider.AuthURL(state)
	return c.JSON(fiber.Map{
		"auth_url": authURL,
		"state":    state,
	})
}

// Callback handles the OAuth2 callback from the provider.
// GET /api/v1/oauth2/callback/:provider
func (h *OAuth2Handlers) Callback(c *fiber.Ctx) error {
	providerName := c.Params("provider")
	provider, ok := h.providers[providerName]
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fmt.Sprintf("unsupported OAuth2 provider: %s", providerName),
		})
	}

	code := c.Query("code")
	state := c.Query("state")
	errorParam := c.Query("error")

	if errorParam != "" {
		errorDesc := c.Query("error_description")
		log.Error().
			Str("provider", providerName).
			Str("error", errorParam).
			Str("description", errorDesc).
			Msg("OAuth2 callback error")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":       errorParam,
			"description": errorDesc,
		})
	}

	if code == "" || state == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "missing code or state parameter",
		})
	}

	// Validate state
	stateData, ok := h.states[state]
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid or expired state",
		})
	}
	delete(h.states, state)

	// Check state age (max 10 minutes)
	if time.Since(stateData.CreatedAt) > 10*time.Minute {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "state expired",
		})
	}

	// Verify provider matches
	if stateData.Provider != providerName {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "provider mismatch",
		})
	}

	// Exchange code for tokens
	tokenResp, err := provider.ExchangeCode(c.Context(), code)
	if err != nil {
		log.Error().Err(err).Str("provider", providerName).Msg("OAuth2 token exchange failed")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("token exchange failed: %v", err),
		})
	}

	// Get user email
	email, err := provider.UserEmail(c.Context(), tokenResp.AccessToken)
	if err != nil {
		log.Error().Err(err).Str("provider", providerName).Msg("failed to get user email")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("failed to get user email: %v", err),
		})
	}

	// Calculate token expiry
	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	// Create or update account
	account := &models.Account{
		TenantID:          stateData.TenantID,
		Name:              fmt.Sprintf("%s (%s)", email, providerName),
		Email:             email,
		Provider:          "imap",
		IMAPHost:          provider.IMAPHost(),
		IMAPPort:          provider.IMAPPort(),
		IMAPTLS:           true,
		Username:          email,
		OAuthToken:        tokenResp.AccessToken,
		OAuthRefreshToken: tokenResp.RefreshToken,
		OAuthTokenExpiry:  &expiry,
		OAuthProvider:     providerName,
		Active:            true,
	}

	if err := h.DB.CreateAccount(c.Context(), account); err != nil {
		log.Error().Err(err).Str("email", email).Msg("failed to create OAuth2 account")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("failed to create account: %v", err),
		})
	}

	log.Info().
		Int64("account_id", account.ID).
		Str("email", email).
		Str("provider", providerName).
		Msg("OAuth2 account connected successfully")

	// Redact sensitive fields in response
	account.OAuthToken = ""
	account.OAuthRefreshToken = ""
	account.Password = ""

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "account connected successfully",
		"account": account,
	})
}

// RefreshToken manually triggers a token refresh for an account.
// POST /api/v1/oauth2/refresh/:id
func (h *OAuth2Handlers) RefreshToken(c *fiber.Ctx) error {
	accountID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid account ID"})
	}

	tenantID := h.tenantID(c)
	account, err := h.DB.GetAccount(c.Context(), tenantID, accountID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "account not found"})
	}

	if account.OAuthProvider == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "account is not an OAuth2 account"})
	}

	provider, ok := h.providers[account.OAuthProvider]
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fmt.Sprintf("provider %s not configured", account.OAuthProvider),
		})
	}

	if account.OAuthRefreshToken == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no refresh token available"})
	}

	tokenResp, err := provider.RefreshToken(c.Context(), account.OAuthRefreshToken)
	if err != nil {
		log.Error().Err(err).Int64("account_id", accountID).Msg("OAuth2 token refresh failed")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("token refresh failed: %v", err),
		})
	}

	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	// Use new refresh token if provided, otherwise keep the old one
	refreshToken := tokenResp.RefreshToken
	if refreshToken == "" {
		refreshToken = account.OAuthRefreshToken
	}

	if err := h.DB.UpdateAccountOAuthTokens(c.Context(), accountID, tokenResp.AccessToken, refreshToken, &expiry); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("failed to update tokens: %v", err),
		})
	}

	log.Info().
		Int64("account_id", accountID).
		Str("provider", account.OAuthProvider).
		Time("expiry", expiry).
		Msg("OAuth2 token refreshed successfully")

	return c.JSON(fiber.Map{
		"message":      "token refreshed successfully",
		"expires_at":   expiry,
		"expires_in":   tokenResp.ExpiresIn,
	})
}

// Disconnect removes OAuth2 tokens from an account and deactivates it.
// POST /api/v1/oauth2/disconnect/:id
func (h *OAuth2Handlers) Disconnect(c *fiber.Ctx) error {
	accountID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid account ID"})
	}

	tenantID := h.tenantID(c)
	account, err := h.DB.GetAccount(c.Context(), tenantID, accountID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "account not found"})
	}

	if account.OAuthProvider == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "account is not an OAuth2 account"})
	}

	// Clear OAuth tokens and deactivate
	account.OAuthToken = ""
	account.OAuthRefreshToken = ""
	account.OAuthTokenExpiry = nil
	account.Active = false

	if err := h.DB.UpdateAccount(c.Context(), account); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("failed to disconnect account: %v", err),
		})
	}

	log.Info().
		Int64("account_id", accountID).
		Str("provider", account.OAuthProvider).
		Msg("OAuth2 account disconnected")

	return c.JSON(fiber.Map{"message": "account disconnected"})
}

func (h *OAuth2Handlers) tenantID(c *fiber.Ctx) int64 {
	if tid, ok := c.Locals("tenant_id").(int64); ok {
		return tid
	}
	return 0
}

func (h *OAuth2Handlers) cleanupStates() {
	cutoff := time.Now().Add(-10 * time.Minute)
	for state, data := range h.states {
		if data.CreatedAt.Before(cutoff) {
			delete(h.states, state)
		}
	}
}

func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
