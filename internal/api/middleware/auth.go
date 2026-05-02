package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/djpadz/email-automation/internal/db"
)

// AuthMiddleware validates API key authentication and sets tenant context.
func AuthMiddleware(database *db.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Health check endpoints skip auth
		if c.Path() == "/health" || c.Path() == "/ready" {
			return c.Next()
		}

		apiKey := c.Get("X-API-Key")
		if apiKey == "" {
			// Try Bearer token
			auth := c.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				apiKey = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		if apiKey == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "missing API key",
			})
		}

		tenant, err := database.GetTenantByAPIKey(c.Context(), apiKey)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid API key",
			})
		}

		c.Locals("tenant_id", tenant.ID)
		c.Locals("tenant", tenant)
		return c.Next()
	}
}
