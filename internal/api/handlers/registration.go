package handlers

import (
	"bytes"
	"encoding/base64"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	internalAuth "github.com/djpadz/email-automation/internal/auth"
	"github.com/djpadz/email-automation/internal/models"
)

// RegisterUser handles POST /auth/register/passkey — create a user account (no password required).
// This is the first step of the passkey-only registration flow.
func (h *AuthHandlers) RegisterUser(c *fiber.Ctx) error {
	if !h.Config.RegistrationEnabled {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "registration is currently disabled"})
	}

	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		TenantID    *int64 `json:"tenant_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.Username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	if len(req.Username) < 3 || len(req.Username) > 64 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username must be 3-64 characters"})
	}

	// Create user with no password (passkey-only account)
	user := &models.User{
		Username:     req.Username,
		PasswordHash: "", // No password — passkey-only
	}

	if err := h.DB.CreateUser(c.Context(), user); err != nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "registration failed"})
	}

	// If tenant_id provided, assign user to that tenant
	if req.TenantID != nil {
		if err := h.DB.AssignTenantToUser(c.Context(), *req.TenantID, user.ID); err != nil {
			log.Warn().Err(err).Int64("tenant_id", *req.TenantID).Int64("user_id", user.ID).
				Msg("failed to assign tenant to user")
		}
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"user_id":  user.ID,
		"username": user.Username,
	})
}

// PasskeyEnrollBegin handles POST /auth/passkey/enroll/begin — start passkey enrollment (public).
// This is the second step: after creating a user, begin the WebAuthn registration ceremony.
func (h *AuthHandlers) PasskeyEnrollBegin(c *fiber.Ctx) error {
	if h.WebAuthn == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "passkey enrollment not available"})
	}

	var req struct {
		Username string `json:"username"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.Username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	user, err := h.DB.GetUserByUsername(c.Context(), req.Username)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	// Get existing credentials to exclude (prevent re-enrollment of same authenticator)
	existingPasskeys, err := h.DB.GetPasskeysByUserID(c.Context(), user.ID)
	if err != nil {
		log.Error().Err(err).Msg("failed to get passkeys")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
	}

	var credentials []protocol.CredentialDescriptor
	for _, p := range existingPasskeys {
		credID, err := base64.RawURLEncoding.DecodeString(p.CredentialID)
		if err != nil {
			continue
		}
		var transports []protocol.AuthenticatorTransport
		for _, t := range p.Transports {
			transports = append(transports, protocol.AuthenticatorTransport(t))
		}
		credentials = append(credentials, protocol.CredentialDescriptor{
			Type:            protocol.PublicKeyCredentialType,
			CredentialID:    credID,
			Transport:       transports,
		})
	}

	wanUser := &internalAuth.WebAuthnUser{
		ID:       user.ID,
		Username: user.Username,
	}

	options, session, err := h.WebAuthn.BeginRegistration(wanUser,
		webauthnExcludeCredentials(credentials),
	)
	if err != nil {
		log.Error().Err(err).Msg("failed to begin passkey enrollment")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to begin enrollment"})
	}

	// Store session keyed by username for the public flow
	sessionKey := fmt.Sprintf("enroll_%s", user.Username)
	h.storeSession(sessionKey, session)

	return c.JSON(options)
}

// PasskeyEnrollFinish handles POST /auth/passkey/enroll/finish — complete passkey enrollment (public).
// This is the final step: verify the attestation and store the credential.
func (h *AuthHandlers) PasskeyEnrollFinish(c *fiber.Ctx) error {
	if h.WebAuthn == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "passkey enrollment not available"})
	}

	// Username comes from query param since the body is the raw WebAuthn attestation response
	username := c.Query("username", "")
	if username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username query parameter is required"})
	}

	user, err := h.DB.GetUserByUsername(c.Context(), username)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	sessionKey := fmt.Sprintf("enroll_%s", user.Username)
	session, ok := h.getSession(sessionKey)
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no enrollment in progress"})
	}
	defer h.deleteSession(sessionKey)

	wanUser := &internalAuth.WebAuthnUser{
		ID:       user.ID,
		Username: user.Username,
	}

	// Parse the WebAuthn attestation response
	// NOTE: Use bytes.NewReader(c.Body()) — see PasskeyRegisterComplete for explanation
	// about why BodyStream() can't be used with Fiber/fasthttp.
	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(c.Body()))
	if err != nil {
		log.Error().Err(err).Msg("failed to parse credential creation response")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid attestation response"})
	}

	credential, err := h.WebAuthn.CreateCredential(wanUser, *session, parsedResponse)
	if err != nil {
		log.Error().Err(err).Msg("failed to create credential")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "failed to verify attestation"})
	}

	// Store the credential
	var transports []string
	for _, t := range credential.Transport {
		transports = append(transports, string(t))
	}

	// Get existing passkeys to generate a default name
	existingPasskeys, _ := h.DB.GetPasskeysByUserID(c.Context(), user.ID)
	name := c.Query("name", "")
	if name == "" {
		name = fmt.Sprintf("Passkey %d", len(existingPasskeys)+1)
	}

	passkey := &models.Passkey{
		UserID:       user.ID,
		CredentialID: base64.RawURLEncoding.EncodeToString(credential.ID),
		PublicKey:    base64.RawURLEncoding.EncodeToString(credential.PublicKey),
		SignCount:    credential.Authenticator.SignCount,
		Transports:   transports,
		Name:         name,
	}

	if err := h.DB.CreatePasskey(c.Context(), passkey); err != nil {
		log.Error().Err(err).Msg("failed to store passkey")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to store passkey"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"passkey": fiber.Map{
			"id":   passkey.ID,
			"name": passkey.Name,
		},
	})
}

// webauthnExcludeCredentials returns a registration option that excludes existing credentials.
func webauthnExcludeCredentials(creds []protocol.CredentialDescriptor) func(*protocol.PublicKeyCredentialCreationOptions) {
	return func(opts *protocol.PublicKeyCredentialCreationOptions) {
		opts.CredentialExcludeList = creds
	}
}
