package handlers

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/models"
)

// AccountHandlers holds dependencies for account HTTP handlers.
type AccountHandlers struct {
	DB *db.DB
}

func (h *AccountHandlers) tenantID(c *fiber.Ctx) int64 {
	if tid, ok := c.Locals("tenant_id").(int64); ok {
		return tid
	}
	return 0
}

// ListAccounts returns all accounts for the tenant.
func (h *AccountHandlers) ListAccounts(c *fiber.Ctx) error {
	accounts, err := h.DB.ListAccounts(c.Context(), h.tenantID(c))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if accounts == nil {
		accounts = []models.Account{}
	}
	// Redact passwords in response
	for i := range accounts {
		accounts[i].Password = ""
		accounts[i].OAuthToken = ""
	}
	return c.JSON(accounts)
}

// GetAccount returns a single account by ID.
func (h *AccountHandlers) GetAccount(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid account ID"})
	}

	account, err := h.DB.GetAccount(c.Context(), h.tenantID(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "account not found"})
	}

	// Redact sensitive fields
	account.Password = ""
	account.OAuthToken = ""
	return c.JSON(account)
}

// CreateAccount creates a new email account.
func (h *AccountHandlers) CreateAccount(c *fiber.Ctx) error {
	var account models.Account
	if err := c.BodyParser(&account); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	account.TenantID = h.tenantID(c)

	if account.Provider == "" {
		account.Provider = "imap"
	}

	if err := h.DB.CreateAccount(c.Context(), &account); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	account.Password = ""
	account.OAuthToken = ""
	return c.Status(fiber.StatusCreated).JSON(account)
}

// UpdateAccount updates an existing account.
func (h *AccountHandlers) UpdateAccount(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid account ID"})
	}

	var account models.Account
	if err := c.BodyParser(&account); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	account.ID = id
	account.TenantID = h.tenantID(c)

	if err := h.DB.UpdateAccount(c.Context(), &account); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	account.Password = ""
	account.OAuthToken = ""
	return c.JSON(account)
}

// DeleteAccount deletes an account.
func (h *AccountHandlers) DeleteAccount(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid account ID"})
	}

	if err := h.DB.DeleteAccount(c.Context(), h.tenantID(c), id); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}
