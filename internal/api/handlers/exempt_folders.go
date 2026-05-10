package handlers

import (
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

// ListExemptFolders returns the current list of exempt folders.
// GET /api/v1/settings/exempt-folders
func (h *ExemptFoldersHandlers) ListExemptFolders(c *fiber.Ctx) error {
	folders, err := h.DB.GetExemptFolders(c.Context(), h.tenantID(c))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if folders == nil {
		folders = []string{}
	}
	return c.JSON(fiber.Map{"exempt_folders": folders})
}

// AddExemptFolder adds a folder to the exempt list.
// POST /api/v1/settings/exempt-folders
func (h *ExemptFoldersHandlers) AddExemptFolder(c *fiber.Ctx) error {
	var body struct {
		Folder string `json:"folder"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	if body.Folder == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "folder name is required"})
	}

	folders, err := h.DB.AddExemptFolder(c.Context(), h.tenantID(c), body.Folder)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"exempt_folders": folders})
}

// RemoveExemptFolder removes a folder from the exempt list.
// DELETE /api/v1/settings/exempt-folders/:folder
func (h *ExemptFoldersHandlers) RemoveExemptFolder(c *fiber.Ctx) error {
	folder := c.Params("folder")
	if folder == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "folder name is required"})
	}

	folders, err := h.DB.RemoveExemptFolder(c.Context(), h.tenantID(c), folder)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"exempt_folders": folders})
}
