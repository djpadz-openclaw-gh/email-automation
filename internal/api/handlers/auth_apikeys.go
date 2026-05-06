package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/auth"
	"github.com/djpadz/email-automation/internal/models"
)

// APIKeyCreate handles POST /auth/apikeys
func (h *AuthHandlers) APIKeyCreate(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int64)

	var req struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name is required"})
	}

	rawKey, err := auth.GenerateAPIKey()
	if err != nil {
		log.Error().Err(err).Msg("failed to generate API key")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to generate API key"})
	}

	keyHash := auth.HashAPIKey(rawKey)
	prefix := rawKey[:10] + "..."

	record := &models.APIKeyRecord{
		UserID:  userID,
		KeyHash: keyHash,
		Name:    req.Name,
		Prefix:  prefix,
	}

	if err := h.DB.CreateAPIKey(c.Context(), record); err != nil {
		log.Error().Err(err).Msg("failed to store API key")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create API key"})
	}

	// Return the raw key only once - it can't be retrieved later
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id":     record.ID,
		"name":   record.Name,
		"key":    rawKey,
		"prefix": prefix,
		"message": "Save this key now. It cannot be retrieved later.",
	})
}

// APIKeyList handles GET /auth/apikeys
func (h *AuthHandlers) APIKeyList(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int64)

	keys, err := h.DB.GetAPIKeysByUserID(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to list API keys"})
	}

	if keys == nil {
		keys = []models.APIKeyRecord{}
	}

	result := make([]fiber.Map, len(keys))
	for i, k := range keys {
		result[i] = fiber.Map{
			"id":           k.ID,
			"name":         k.Name,
			"prefix":       k.Prefix,
			"created_at":   k.CreatedAt,
			"last_used_at": k.LastUsedAt,
		}
	}

	return c.JSON(result)
}

// APIKeyDelete handles DELETE /auth/apikeys/:id
func (h *AuthHandlers) APIKeyDelete(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int64)
	keyID, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid API key ID"})
	}

	if err := h.DB.DeleteAPIKey(c.Context(), int64(keyID), userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to delete API key"})
	}

	return c.JSON(fiber.Map{"message": "API key deleted"})
}

// ChangePassword handles POST /auth/password/change
func (h *AuthHandlers) ChangePassword(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int64)

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "current_password and new_password are required"})
	}

	if err := auth.ValidatePassword(req.NewPassword); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	user, err := h.DB.GetUserByID(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	if !auth.CheckPassword(req.CurrentPassword, user.PasswordHash) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid current password"})
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to update password"})
	}

	if err := h.DB.UpdateUserPassword(c.Context(), userID, hash); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to update password"})
	}

	return c.JSON(fiber.Map{"message": "password changed successfully"})
}

// GetProfile handles GET /auth/profile
func (h *AuthHandlers) GetProfile(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int64)

	user, err := h.DB.GetUserByID(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	passkeys, err := h.DB.GetPasskeysByUserID(c.Context(), userID)
	if err != nil {
		passkeys = nil
	}

	passkeyCount := 0
	if passkeys != nil {
		passkeyCount = len(passkeys)
	}

	// User #1 is always admin
	role := user.Role
	if user.ID == 1 {
		role = "admin"
	}

	return c.JSON(fiber.Map{
		"id":            user.ID,
		"username":      user.Username,
		"totp_enabled":  user.TOTPEnabled,
		"passkey_count": passkeyCount,
		"role":          role,
		"created_at":    user.CreatedAt,
	})
}
