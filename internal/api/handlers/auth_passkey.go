package handlers

import (
	"encoding/base64"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	internalAuth "github.com/djpadz/email-automation/internal/auth"
	"github.com/djpadz/email-automation/internal/models"
)

// PasskeyRegisterBegin handles POST /auth/passkey/register/begin
func (h *AuthHandlers) PasskeyRegisterBegin(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int64)

	user, err := h.DB.GetUserByID(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	// Get existing credentials to exclude
	existingPasskeys, err := h.DB.GetPasskeysByUserID(c.Context(), userID)
	if err != nil {
		log.Error().Err(err).Msg("failed to get passkeys")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
	}

	var credentials []webauthn.Credential
	for _, p := range existingPasskeys {
		record := &internalAuth.PasskeyRecord{
			CredentialID: p.CredentialID,
			PublicKey:    p.PublicKey,
			SignCount:    p.SignCount,
			Transports:   p.Transports,
		}
		cred, err := record.ToWebAuthnCredential()
		if err != nil {
			continue
		}
		credentials = append(credentials, cred)
	}

	wanUser := &internalAuth.WebAuthnUser{
		ID:          user.ID,
		Username:    user.Username,
		Credentials: credentials,
	}

	options, session, err := h.WebAuthn.BeginRegistration(wanUser)
	if err != nil {
		log.Error().Err(err).Msg("failed to begin passkey registration")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to begin registration"})
	}

	// Store session for completion
	sessionKey := fmt.Sprintf("reg_%d", userID)
	h.storeSession(sessionKey, session)

	return c.JSON(options)
}

// PasskeyRegisterComplete handles POST /auth/passkey/register/complete
func (h *AuthHandlers) PasskeyRegisterComplete(c *fiber.Ctx) error {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("panic in PasskeyRegisterComplete")
		}
	}()

	userID := c.Locals("user_id").(int64)

	user, err := h.DB.GetUserByID(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	sessionKey := fmt.Sprintf("reg_%d", userID)
	session, ok := h.getSession(sessionKey)
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no registration in progress"})
	}
	defer h.deleteSession(sessionKey)

	// Get existing credentials
	existingPasskeys, err := h.DB.GetPasskeysByUserID(c.Context(), userID)
	if err != nil {
		log.Error().Err(err).Msg("failed to get passkeys")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
	}

	var credentials []webauthn.Credential
	for _, p := range existingPasskeys {
		record := &internalAuth.PasskeyRecord{
			CredentialID: p.CredentialID,
			PublicKey:    p.PublicKey,
			SignCount:    p.SignCount,
			Transports:   p.Transports,
		}
		cred, err := record.ToWebAuthnCredential()
		if err != nil {
			continue
		}
		credentials = append(credentials, cred)
	}

	wanUser := &internalAuth.WebAuthnUser{
		ID:          user.ID,
		Username:    user.Username,
		Credentials: credentials,
	}

	// Parse the response body
	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(c.Request().BodyStream())
	if err != nil {
		log.Error().Err(err).Msg("failed to parse credential creation response")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid attestation response"})
	}

	credential, err := h.WebAuthn.CreateCredential(wanUser, *session, parsedResponse)
	if err != nil {
		log.Error().Err(err).Msg("failed to create credential")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "failed to verify attestation"})
	}

	// Determine passkey name
	var req struct {
		Name string `json:"name"`
	}
	// Name might come from query param since body is the WebAuthn response
	name := c.Query("name", "")
	if name == "" {
		name = fmt.Sprintf("Passkey %d", len(existingPasskeys)+1)
	}
	_ = req

	// Store the credential
	var transports []string
	for _, t := range credential.Transport {
		transports = append(transports, string(t))
	}

	passkey := &models.Passkey{
		UserID:       userID,
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
		"message": "passkey registered successfully",
		"passkey": fiber.Map{
			"id":   passkey.ID,
			"name": passkey.Name,
		},
	})
}

// PasskeyAuthenticateBegin handles POST /auth/passkey/authenticate/begin
func (h *AuthHandlers) PasskeyAuthenticateBegin(c *fiber.Ctx) error {
	var req struct {
		Username string `json:"username"`
	}
	_ = c.BodyParser(&req)

	// If username provided, do user-specific auth; otherwise discoverable credential
	if req.Username != "" {
		user, err := h.DB.GetUserByUsername(c.Context(), req.Username)
		if err != nil {
			// Don't reveal whether user exists - return generic options
			options, session, err := h.WebAuthn.BeginDiscoverableLogin()
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to begin authentication"})
			}
			h.storeSession("auth_discoverable", session)
			return c.JSON(options)
		}

		passkeys, err := h.DB.GetPasskeysByUserID(c.Context(), user.ID)
		if err != nil || len(passkeys) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no passkeys registered"})
		}

		var credentials []webauthn.Credential
		for _, p := range passkeys {
			record := &internalAuth.PasskeyRecord{
				CredentialID: p.CredentialID,
				PublicKey:    p.PublicKey,
				SignCount:    p.SignCount,
				Transports:   p.Transports,
			}
			cred, err := record.ToWebAuthnCredential()
			if err != nil {
				continue
			}
			credentials = append(credentials, cred)
		}

		wanUser := &internalAuth.WebAuthnUser{
			ID:          user.ID,
			Username:    user.Username,
			Credentials: credentials,
		}

		options, session, err := h.WebAuthn.BeginLogin(wanUser)
		if err != nil {
			log.Error().Err(err).Msg("failed to begin passkey auth")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to begin authentication"})
		}

		sessionKey := fmt.Sprintf("auth_%d", user.ID)
		h.storeSession(sessionKey, session)

		return c.JSON(fiber.Map{
			"publicKey": options.Response,
			"user_id":   user.ID,
		})
	}

	// Discoverable login (no username)
	options, session, err := h.WebAuthn.BeginDiscoverableLogin()
	if err != nil {
		log.Error().Err(err).Msg("failed to begin discoverable login")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to begin authentication"})
	}

	h.storeSession("auth_discoverable", session)
	return c.JSON(options)
}

// PasskeyAuthenticateComplete handles POST /auth/passkey/authenticate/complete
func (h *AuthHandlers) PasskeyAuthenticateComplete(c *fiber.Ctx) error {
	parsedResponse, err := protocol.ParseCredentialRequestResponseBody(c.Request().BodyStream())
	if err != nil {
		log.Error().Err(err).Msg("failed to parse credential request response")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid assertion response"})
	}

	// Try to find the credential in our database
	credentialID := base64.RawURLEncoding.EncodeToString(parsedResponse.RawID)
	dbPasskey, err := h.DB.GetPasskeyByCredentialID(c.Context(), credentialID)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid credentials"})
	}

	user, err := h.DB.GetUserByID(c.Context(), dbPasskey.UserID)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid credentials"})
	}

	// Get all user passkeys for the WebAuthn user
	passkeys, err := h.DB.GetPasskeysByUserID(c.Context(), user.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
	}

	var credentials []webauthn.Credential
	for _, p := range passkeys {
		record := &internalAuth.PasskeyRecord{
			CredentialID: p.CredentialID,
			PublicKey:    p.PublicKey,
			SignCount:    p.SignCount,
			Transports:   p.Transports,
		}
		cred, err := record.ToWebAuthnCredential()
		if err != nil {
			continue
		}
		credentials = append(credentials, cred)
	}

	wanUser := &internalAuth.WebAuthnUser{
		ID:          user.ID,
		Username:    user.Username,
		Credentials: credentials,
	}

	// Try user-specific session first, then discoverable
	sessionKey := fmt.Sprintf("auth_%d", user.ID)
	session, ok := h.getSession(sessionKey)
	if !ok {
		session, ok = h.getSession("auth_discoverable")
		if !ok {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no authentication in progress"})
		}
		sessionKey = "auth_discoverable"
	}
	defer h.deleteSession(sessionKey)

	credential, err := h.WebAuthn.ValidateLogin(wanUser, *session, parsedResponse)
	if err != nil {
		log.Error().Err(err).Msg("failed to validate passkey login")
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "authentication failed"})
	}

	// Update sign count
	_ = h.DB.UpdatePasskeySignCount(c.Context(), dbPasskey.ID, credential.Authenticator.SignCount)

	return h.generateTokenResponse(c, user)
}

// PasskeyList handles POST /auth/passkey/list
func (h *AuthHandlers) PasskeyList(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int64)

	passkeys, err := h.DB.GetPasskeysByUserID(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to list passkeys"})
	}

	if passkeys == nil {
		passkeys = []models.Passkey{}
	}

	// Return without sensitive data
	result := make([]fiber.Map, len(passkeys))
	for i, p := range passkeys {
		result[i] = fiber.Map{
			"id":           p.ID,
			"name":         p.Name,
			"created_at":   p.CreatedAt,
			"last_used_at": p.LastUsedAt,
		}
	}

	return c.JSON(result)
}

// PasskeyDelete handles DELETE /auth/passkey/:id
func (h *AuthHandlers) PasskeyDelete(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int64)
	passkeyID, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid passkey ID"})
	}

	if err := h.DB.DeletePasskey(c.Context(), int64(passkeyID), userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to delete passkey"})
	}

	return c.JSON(fiber.Map{"message": "passkey deleted"})
}
