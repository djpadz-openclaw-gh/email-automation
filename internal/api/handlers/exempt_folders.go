package handlers

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/djpadz/email-automation/internal/db"
)

// ExemptFoldersHandlers holds dependencies for exempt folder HTTP handlers.
type ExemptFoldersHandlers struct {
	DB *db.DB
}

func (h *ExemptFoldersHandlers) tenantID(c *fiber.Ctx) int64 {
	if tid, ok := c.Locals("tenant_id").(int64); ok {
		return tid
	}
	return 0
}

func (h *ExemptFoldersHandlers) accountID(c *fiber.Ctx) (int64, error) {
	return strconv.ParseInt(c.Params("accountId"), 10, 64)
}

// ListExemptFolders returns the current list of exempt folders for an account.
// GET /api/v1/accounts/:accountId/settings/exempt-folders
func (h *ExemptFoldersHandlers) ListExemptFolders(c *fiber.Ctx) error {
	accountID, err := h.accountID(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid account ID"})
	}

	// Verify the account belongs to this tenant
	tenantID := h.tenantID(c)
	if _, err := h.DB.GetAccount(c.Context(), tenantID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "account not found"})
	}

	folders, err := h.DB.GetExemptFolders(c.Context(), accountID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if folders == nil {
		folders = []string{}
	}
	return c.JSON(fiber.Map{"exempt_folders": folders})
}

// AddExemptFolder adds a folder to the exempt list for an account.
// POST /api/v1/accounts/:accountId/settings/exempt-folders
func (h *ExemptFoldersHandlers) AddExemptFolder(c *fiber.Ctx) error {
	accountID, err := h.accountID(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid account ID"})
	}

	// Verify the account belongs to this tenant
	tenantID := h.tenantID(c)
	if _, err := h.DB.GetAccount(c.Context(), tenantID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "account not found"})
	}

	var body struct {
		Folder string `json:"folder"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	if body.Folder == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "folder name is required"})
	}

	folders, err := h.DB.AddExemptFolder(c.Context(), accountID, body.Folder)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"exempt_folders": folders})
}

// RemoveExemptFolder removes a folder from the exempt list for an account.
// DELETE /api/v1/accounts/:accountId/settings/exempt-folders/:folder
func (h *ExemptFoldersHandlers) RemoveExemptFolder(c *fiber.Ctx) error {
	accountID, err := h.accountID(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid account ID"})
	}

	// Verify the account belongs to this tenant
	tenantID := h.tenantID(c)
	if _, err := h.DB.GetAccount(c.Context(), tenantID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "account not found"})
	}

	folder := c.Params("folder")
	if folder == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "folder name is required"})
	}

	folders, err := h.DB.RemoveExemptFolder(c.Context(), accountID, folder)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"exempt_folders": folders})
}
