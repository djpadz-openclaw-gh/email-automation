package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

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
