package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/auth"
)

// TOTPSetup handles POST /auth/totp/setup
func (h *AuthHandlers) TOTPSetup(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int64)

	user, err := h.DB.GetUserByID(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	if user.TOTPEnabled {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "TOTP is already enabled"})
	}

	result, err := auth.GenerateTOTP(user.Username)
	if err != nil {
		log.Error().Err(err).Msg("failed to generate TOTP")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to generate TOTP"})
	}

	// Store the secret (not yet enabled)
	if err := h.DB.UpdateUserTOTP(c.Context(), userID, &result.Secret, false); err != nil {
		log.Error().Err(err).Msg("failed to store TOTP secret")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to setup TOTP"})
	}

	return c.JSON(fiber.Map{
		"secret":  result.Secret,
		"url":     result.URL,
		"qr_code": result.QRCode,
	})
}

// TOTPVerify handles POST /auth/totp/verify - enables TOTP after verifying a token
func (h *AuthHandlers) TOTPVerify(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int64)

	var req struct {
		TOTPToken string `json:"totp_token"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.TOTPToken == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "totp_token is required"})
	}

	user, err := h.DB.GetUserByID(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	if user.TOTPEnabled {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "TOTP is already enabled"})
	}

	if user.TOTPSecret == nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "TOTP not set up yet, call /auth/totp/setup first"})
	}

	if !auth.ValidateTOTP(req.TOTPToken, *user.TOTPSecret) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid TOTP token"})
	}

	// Enable TOTP
	if err := h.DB.UpdateUserTOTP(c.Context(), userID, user.TOTPSecret, true); err != nil {
		log.Error().Err(err).Msg("failed to enable TOTP")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to enable TOTP"})
	}

	return c.JSON(fiber.Map{"message": "TOTP enabled successfully"})
}

// TOTPDisable handles POST /auth/totp/disable
func (h *AuthHandlers) TOTPDisable(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int64)

	var req struct {
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "password is required"})
	}

	user, err := h.DB.GetUserByID(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	if !auth.CheckPassword(req.Password, user.PasswordHash) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid password"})
	}

	if err := h.DB.UpdateUserTOTP(c.Context(), userID, nil, false); err != nil {
		log.Error().Err(err).Msg("failed to disable TOTP")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to disable TOTP"})
	}

	return c.JSON(fiber.Map{"message": "TOTP disabled successfully"})
}
