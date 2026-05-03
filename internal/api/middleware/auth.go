package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/djpadz/email-automation/internal/auth"
	"github.com/djpadz/email-automation/internal/db"
)

// JWTAuthMiddleware validates JWT tokens or API keys and sets user/tenant context.
func JWTAuthMiddleware(database *db.DB, jwtMgr *auth.JWTManager) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Health check endpoints skip auth
		if c.Path() == "/health" || c.Path() == "/ready" {
			return c.Next()
		}

		// Try JWT Bearer token first
		authHeader := c.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenString := strings.TrimPrefix(authHeader, "Bearer ")

			// Check if it looks like a JWT (contains dots)
			if strings.Count(tokenString, ".") == 2 {
				claims, err := jwtMgr.ValidateToken(tokenString)
				if err != nil {
					return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
						"error": "invalid or expired token",
					})
				}

				// Check token blacklist
				tokenHash := auth.HashToken(tokenString)
				blacklisted, err := database.IsTokenBlacklisted(c.Context(), tokenHash)
				if err == nil && blacklisted {
					return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
						"error": "token has been revoked",
					})
				}

				c.Locals("user_id", claims.UserID)
				c.Locals("username", claims.Username)

				// Find tenant for this user, auto-create if none exists
				tenants, err := database.GetTenantsByUserID(c.Context(), claims.UserID)
				if err == nil && len(tenants) > 0 {
					c.Locals("tenant_id", tenants[0].ID)
					c.Locals("tenant", &tenants[0])
				} else if err == nil && len(tenants) == 0 {
					// Auto-create a default tenant for this user
					newTenant, createErr := database.CreateTenantForUser(c.Context(), claims.UserID, claims.Username)
					if createErr == nil {
						c.Locals("tenant_id", newTenant.ID)
						c.Locals("tenant", newTenant)
					}
				}

				return c.Next()
			}

			// Not a JWT - try as API key
			return tryAPIKeyAuth(c, database, tokenString)
		}

		// Try X-API-Key header
		apiKey := c.Get("X-API-Key")
		if apiKey != "" {
			// First try as a user API key (ea_ prefix)
			if strings.HasPrefix(apiKey, "ea_") {
				return tryUserAPIKeyAuth(c, database, apiKey)
			}
			// Fall back to legacy tenant API key
			return tryAPIKeyAuth(c, database, apiKey)
		}

		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "authentication required",
		})
	}
}

// tryAPIKeyAuth attempts authentication using a legacy tenant API key.
func tryAPIKeyAuth(c *fiber.Ctx, database *db.DB, apiKey string) error {
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

// tryUserAPIKeyAuth attempts authentication using a user-generated API key.
func tryUserAPIKeyAuth(c *fiber.Ctx, database *db.DB, apiKey string) error {
	keyHash := auth.HashAPIKey(apiKey)
	user, err := database.GetUserByAPIKeyHash(c.Context(), keyHash)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "invalid API key",
		})
	}

	c.Locals("user_id", user.ID)
	c.Locals("username", user.Username)

	// Find tenant for this user, auto-create if none exists
	tenants, err := database.GetTenantsByUserID(c.Context(), user.ID)
	if err == nil && len(tenants) > 0 {
		c.Locals("tenant_id", tenants[0].ID)
		c.Locals("tenant", &tenants[0])
	} else if err == nil && len(tenants) == 0 {
		// Auto-create a default tenant for this user
		newTenant, createErr := database.CreateTenantForUser(c.Context(), user.ID, user.Username)
		if createErr == nil {
			c.Locals("tenant_id", newTenant.ID)
			c.Locals("tenant", newTenant)
		}
	}

	return c.Next()
}

// RequireAuth is a middleware that ensures user_id is set in context (JWT or user API key auth).
func RequireAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Locals("user_id") == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "authentication required",
			})
		}
		return c.Next()
	}
}

// AuthMiddleware validates API key authentication and sets tenant context.
// Kept for backward compatibility.
func AuthMiddleware(database *db.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Path() == "/health" || c.Path() == "/ready" {
			return c.Next()
		}

		apiKey := c.Get("X-API-Key")
		if apiKey == "" {
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
