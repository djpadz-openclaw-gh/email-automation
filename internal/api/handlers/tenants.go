package handlers

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gofiber/fiber/v2"

	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/models"
)

// TenantHandlers holds dependencies for tenant HTTP handlers.
type TenantHandlers struct {
	DB *db.DB
}

// ListTenants returns all tenants.
func (h *TenantHandlers) ListTenants(c *fiber.Ctx) error {
	tenants, err := h.DB.ListTenants(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if tenants == nil {
		tenants = []models.Tenant{}
	}
	// Redact API keys in list
	for i := range tenants {
		tenants[i].APIKey = maskKey(tenants[i].APIKey)
	}
	return c.JSON(tenants)
}

// CreateTenant creates a new tenant with a generated API key.
func (h *TenantHandlers) CreateTenant(c *fiber.Ctx) error {
	var tenant models.Tenant
	if err := c.BodyParser(&tenant); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if tenant.Name == "" || tenant.Slug == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name and slug are required"})
	}

	// Generate API key
	key, err := generateAPIKey()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to generate API key"})
	}
	tenant.APIKey = key

	if err := h.DB.CreateTenant(c.Context(), &tenant); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// Return full API key only on creation
	return c.Status(fiber.StatusCreated).JSON(tenant)
}

func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ea_" + hex.EncodeToString(b), nil
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:7] + "..." + key[len(key)-4:]
}
