package handlers

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/db"
)

// AdminUserHandlers holds dependencies for admin user management endpoints.
type AdminUserHandlers struct {
	DB *db.DB
}

// AdminUserResponse is the JSON response for a user in admin context.
type AdminUserResponse struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	AIEnabled   bool   `json:"ai_enabled"`
	TOTPEnabled bool   `json:"totp_enabled"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// ListUsers returns all users with their AI permission status.
func (h *AdminUserHandlers) ListUsers(c *fiber.Ctx) error {
	users, err := h.DB.ListAllUsers(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	var response []AdminUserResponse
	for _, u := range users {
		response = append(response, AdminUserResponse{
			ID:          u.ID,
			Username:    u.Username,
			AIEnabled:   u.AIEnabled,
			TOTPEnabled: u.TOTPEnabled,
			CreatedAt:   u.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:   u.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	if response == nil {
		response = []AdminUserResponse{}
	}

	return c.JSON(fiber.Map{"users": response})
}

// GetUser returns a single user's AI permission status.
func (h *AdminUserHandlers) GetUser(c *fiber.Ctx) error {
	userID, err := strconv.ParseInt(c.Params("userId"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid user ID"})
	}

	user, err := h.DB.GetUserByID(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	return c.JSON(AdminUserResponse{
		ID:          user.ID,
		Username:    user.Username,
		AIEnabled:   user.AIEnabled,
		TOTPEnabled: user.TOTPEnabled,
		CreatedAt:   user.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   user.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// UpdateAIEnabled updates a user's AI permission.
func (h *AdminUserHandlers) UpdateAIEnabled(c *fiber.Ctx) error {
	userID, err := strconv.ParseInt(c.Params("userId"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid user ID"})
	}

	var req struct {
		AIEnabled bool `json:"ai_enabled"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if err := h.DB.UpdateUserAIEnabled(c.Context(), userID, req.AIEnabled); err != nil {
		if err.Error() == "user not found" {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	log.Info().
		Int64("user_id", userID).
		Bool("ai_enabled", req.AIEnabled).
		Msg("admin updated user AI permission")

	return c.JSON(fiber.Map{
		"message":    "AI permission updated",
		"user_id":    userID,
		"ai_enabled": req.AIEnabled,
	})
}
