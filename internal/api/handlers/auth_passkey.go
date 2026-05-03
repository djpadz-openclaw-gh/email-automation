package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
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
	// NOTE: Use bytes.NewReader(c.Body()) instead of c.Request().BodyStream().
	// In Fiber/fasthttp, BodyStream() returns nil when the body is buffered in memory
	// (the default for small bodies), which causes a nil pointer dereference panic
	// inside json.NewDecoder(nil).Decode().
	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(c.Body()))
	if err != nil {
		log.Error().Err(err).Msg("failed to parse credential creation response")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid attestation response"})
	}

	if h.WebAuthn == nil {
		log.Error().Msg("WebAuthn is nil - initialization failed")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "passkey registration not available"})
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

	// If username provided, do user-specific auth
	if req.Username != "" {
		user, err := h.DB.GetUserByUsername(c.Context(), req.Username)
		if err != nil {
			// Don't reveal whether user exists — use discoverable login as fallback
			// so the response shape is indistinguishable from a real user
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
	// BeginDiscoverableLogin() creates a session with UserID = nil, which is required
	// by ValidateDiscoverableLogin(). The user is looked up during the complete step
	// via a DiscoverableUserHandler callback that decodes the userHandle from the
	// authenticator response.
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
	// NOTE: Use bytes.NewReader(c.Body()) — see PasskeyRegisterComplete for explanation.
	parsedResponse, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(c.Body()))
	if err != nil {
		log.Error().Err(err).Msg("failed to parse credential request response")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid assertion response"})
	}

	// Try user-specific session first (set when username was provided at begin)
	// Then fall back to discoverable session (set when no username was provided)
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

	log.Info().Int64("user_id", user.ID).Str("username", user.Username).Int("num_credentials", len(credentials)).Msg("validating passkey login")

	// Try user-specific session first
	sessionKey := fmt.Sprintf("auth_%d", user.ID)
	session, ok := h.getSession(sessionKey)
	if ok {
		// User-specific session found — use ValidateLogin which expects session.UserID to match
		log.Info().Str("session_key", sessionKey).Msg("using user-specific login session")
		defer h.deleteSession(sessionKey)

		credential, err := h.WebAuthn.ValidateLogin(wanUser, *session, parsedResponse)
		if err != nil {
			log.Error().Err(err).Int64("user_id", user.ID).Msg("failed to validate user-specific passkey login")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "authentication failed"})
		}

		_ = h.DB.UpdatePasskeySignCount(c.Context(), dbPasskey.ID, credential.Authenticator.SignCount)
		return h.generateTokenResponse(c, user)
	}

	// Try discoverable session — use ValidateDiscoverableLogin which expects session.UserID == nil
	discoverableSession, ok := h.getSession("auth_discoverable")
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no authentication in progress"})
	}
	log.Info().Msg("using discoverable login session with ValidateDiscoverableLogin")
	defer h.deleteSession("auth_discoverable")

	// DiscoverableUserHandler: called by the library to look up the user from the assertion.
	// rawID is the credential ID bytes, userHandle is the user ID bytes (big-endian encoded
	// by WebAuthnID() during registration).
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		if len(userHandle) < 8 {
			return nil, fmt.Errorf("invalid user handle length: %d", len(userHandle))
		}
		uid := int64(binary.BigEndian.Uint64(userHandle))
		log.Info().Int64("decoded_user_id", uid).Msg("discoverable login: decoded user handle")

		lookupUser, err := h.DB.GetUserByID(c.Context(), uid)
		if err != nil {
			return nil, fmt.Errorf("user not found for handle: %w", err)
		}

		lookupPasskeys, err := h.DB.GetPasskeysByUserID(c.Context(), lookupUser.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get passkeys: %w", err)
		}

		var creds []webauthn.Credential
		for _, p := range lookupPasskeys {
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
			creds = append(creds, cred)
		}

		return &internalAuth.WebAuthnUser{
			ID:          lookupUser.ID,
			Username:    lookupUser.Username,
			Credentials: creds,
		}, nil
	}

	credential, err := h.WebAuthn.ValidateDiscoverableLogin(handler, *discoverableSession, parsedResponse)
	if err != nil {
		log.Error().Err(err).Int64("user_id", user.ID).Msg("failed to validate discoverable passkey login")
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "authentication failed"})
	}

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

// RegisterUserWithPasskey handles POST /auth/register/passkey (PUBLIC - no auth required)
func (h *AuthHandlers) RegisterUserWithPasskey(c *fiber.Ctx) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.Username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	// Check if user already exists
	_, err := h.DB.GetUserByUsername(c.Context(), req.Username)
	if err == nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "username already exists"})
	}

	// Hash password if provided
	var passwordHash string
	if req.Password != "" {
		passwordHash, err = internalAuth.HashPassword(req.Password)
		if err != nil {
			log.Error().Err(err).Msg("failed to hash password")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create user"})
		}
	}

	// Create new user
	user := &models.User{
		Username:     req.Username,
		PasswordHash: passwordHash,
	}

	if err := h.DB.CreateUser(c.Context(), user); err != nil {
		log.Error().Err(err).Msg("failed to create user")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create user"})
	}

	return c.JSON(fiber.Map{
		"user_id":  user.ID,
		"username": user.Username,
	})
}

// PasskeyEnrollBegin handles POST /auth/passkey/enroll/begin (PUBLIC - no auth required)
func (h *AuthHandlers) PasskeyEnrollBegin(c *fiber.Ctx) error {
	var req struct {
		Username string `json:"username"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.Username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	// Look up user
	user, err := h.DB.GetUserByUsername(c.Context(), req.Username)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	// Get existing passkeys to exclude
	existingPasskeys, err := h.DB.GetPasskeysByUserID(c.Context(), user.ID)
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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to begin enrollment"})
	}

	// Store session keyed by username for public enrollment
	sessionKey := fmt.Sprintf("enroll_%s", req.Username)
	h.storeSession(sessionKey, session)

	return c.JSON(options)
}

// PasskeyEnrollFinish handles POST /auth/passkey/enroll/finish (PUBLIC - no auth required)
func (h *AuthHandlers) PasskeyEnrollFinish(c *fiber.Ctx) error {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("panic in PasskeyEnrollFinish")
		}
	}()

	// Get username from query param or body
	username := c.Query("username")
	if username == "" {
		var req struct {
			Username string `json:"username"`
		}
		if err := c.BodyParser(&req); err == nil && req.Username != "" {
			username = req.Username
		}
	}

	if username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	// Look up user
	user, err := h.DB.GetUserByUsername(c.Context(), username)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	// Retrieve session from enrollment begin
	sessionKey := fmt.Sprintf("enroll_%s", username)
	session, ok := h.getSession(sessionKey)
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no enrollment in progress"})
	}
	defer h.deleteSession(sessionKey)

	// Get existing passkeys
	existingPasskeys, err := h.DB.GetPasskeysByUserID(c.Context(), user.ID)
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

	// Parse the attestation response
	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(c.Body()))
	if err != nil {
		log.Error().Err(err).Msg("failed to parse credential creation response")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid attestation response"})
	}

	if h.WebAuthn == nil {
		log.Error().Msg("WebAuthn is nil - initialization failed")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "passkey enrollment not available"})
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

	passkey := &models.Passkey{
		UserID:       user.ID,
		CredentialID: base64.RawURLEncoding.EncodeToString(credential.ID),
		PublicKey:    base64.RawURLEncoding.EncodeToString(credential.PublicKey),
		SignCount:    credential.Authenticator.SignCount,
		Transports:   transports,
		Name:         fmt.Sprintf("Passkey %d", len(existingPasskeys)+1),
	}

	if err := h.DB.CreatePasskey(c.Context(), passkey); err != nil {
		log.Error().Err(err).Msg("failed to store passkey")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to store passkey"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "passkey enrolled successfully",
		"passkey": fiber.Map{
			"id":   passkey.ID,
			"name": passkey.Name,
		},
	})
}
