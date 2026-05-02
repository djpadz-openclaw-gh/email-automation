package handlers

import (
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/auth"
	"github.com/djpadz/email-automation/internal/config"
	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/models"
)

// AuthHandlers handles authentication endpoints.
type AuthHandlers struct {
	DB         *db.DB
	JWT        *auth.JWTManager
	WebAuthn   *webauthn.WebAuthn
	Config     *config.Config
	// In-memory session store for WebAuthn ceremonies
	sessions   map[string]*webauthn.SessionData
	sessionsMu sync.RWMutex
}

// NewAuthHandlers creates a new AuthHandlers instance.
func NewAuthHandlers(database *db.DB, jwtMgr *auth.JWTManager, wan *webauthn.WebAuthn, cfg *config.Config) *AuthHandlers {
	return &AuthHandlers{
		DB:       database,
		JWT:      jwtMgr,
		WebAuthn: wan,
		Config:   cfg,
		sessions: make(map[string]*webauthn.SessionData),
	}
}

func (h *AuthHandlers) storeSession(key string, session *webauthn.SessionData) {
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()
	h.sessions[key] = session
}

func (h *AuthHandlers) getSession(key string) (*webauthn.SessionData, bool) {
	h.sessionsMu.RLock()
	defer h.sessionsMu.RUnlock()
	s, ok := h.sessions[key]
	return s, ok
}

func (h *AuthHandlers) deleteSession(key string) {
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()
	delete(h.sessions, key)
}

// checkRateLimit returns true if the request should be rate-limited.
func (h *AuthHandlers) checkRateLimit(c *fiber.Ctx, username string) bool {
	ip := c.IP()
	count, err := h.DB.CountRecentFailedAttempts(c.Context(), username, ip, h.Config.RateLimitWindow)
	if err != nil {
		log.Error().Err(err).Msg("failed to check rate limit")
		return false
	}
	return count >= h.Config.RateLimitMaxFails
}

// generateTokenResponse creates a JWT and returns a standard auth response.
func (h *AuthHandlers) generateTokenResponse(c *fiber.Ctx, user *models.User) error {
	token, expiresAt, err := h.JWT.GenerateToken(user.ID, user.Username)
	if err != nil {
		log.Error().Err(err).Msg("failed to generate token")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to generate token",
		})
	}

	// Record successful login
	_ = h.DB.RecordLoginAttempt(c.Context(), user.Username, c.IP(), true)

	return c.JSON(fiber.Map{
		"token":      token,
		"expires_at": expiresAt.Format(time.RFC3339),
		"user": fiber.Map{
			"id":           user.ID,
			"username":     user.Username,
			"totp_enabled": user.TOTPEnabled,
		},
	})
}

// Register handles POST /auth/register
func (h *AuthHandlers) Register(c *fiber.Ctx) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.Username == "" || req.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username and password are required"})
	}

	if len(req.Username) < 3 || len(req.Username) > 64 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username must be 3-64 characters"})
	}

	if err := auth.ValidatePassword(req.Password); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to process registration"})
	}

	user := &models.User{
		Username:     req.Username,
		PasswordHash: hash,
	}

	if err := h.DB.CreateUser(c.Context(), user); err != nil {
		// Don't reveal whether username exists
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "registration failed"})
	}

	return h.generateTokenResponse(c, user)
}

// Login handles POST /auth/login
func (h *AuthHandlers) Login(c *fiber.Ctx) error {
	var req struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		TOTPToken string `json:"totp_token"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.Username == "" || req.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username and password are required"})
	}

	// Rate limiting
	if h.checkRateLimit(c, req.Username) {
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"error": "too many failed attempts, please try again later",
		})
	}

	user, err := h.DB.GetUserByUsername(c.Context(), req.Username)
	if err != nil {
		_ = h.DB.RecordLoginAttempt(c.Context(), req.Username, c.IP(), false)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid credentials"})
	}

	if !auth.CheckPassword(req.Password, user.PasswordHash) {
		_ = h.DB.RecordLoginAttempt(c.Context(), req.Username, c.IP(), false)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid credentials"})
	}

	// Check TOTP if enabled
	if user.TOTPEnabled {
		if req.TOTPToken == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error":        "TOTP token required",
				"totp_required": true,
			})
		}
		if user.TOTPSecret == nil || !auth.ValidateTOTP(req.TOTPToken, *user.TOTPSecret) {
			_ = h.DB.RecordLoginAttempt(c.Context(), req.Username, c.IP(), false)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid TOTP token"})
		}
	}

	return h.generateTokenResponse(c, user)
}

// Logout handles POST /auth/logout
func (h *AuthHandlers) Logout(c *fiber.Ctx) error {
	tokenString := extractToken(c)
	if tokenString == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no token provided"})
	}

	claims, err := h.JWT.ValidateToken(tokenString)
	if err != nil {
		// Token already invalid, that's fine
		return c.JSON(fiber.Map{"message": "logged out"})
	}

	tokenHash := auth.HashToken(tokenString)
	expiresAt := claims.ExpiresAt.Time
	if err := h.DB.BlacklistToken(c.Context(), tokenHash, expiresAt); err != nil {
		log.Error().Err(err).Msg("failed to blacklist token")
	}

	return c.JSON(fiber.Map{"message": "logged out"})
}

// extractToken gets the Bearer token from the Authorization header.
func extractToken(c *fiber.Ctx) string {
	authHeader := c.Get("Authorization")
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		return authHeader[7:]
	}
	return ""
}
