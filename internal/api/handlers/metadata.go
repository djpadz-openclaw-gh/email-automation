package handlers

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/models"
)

// MetadataHandlers holds dependencies for metadata HTTP handlers.
type MetadataHandlers struct {
	DB *db.DB
}

func (h *MetadataHandlers) tenantID(c *fiber.Ctx) int64 {
	if tid, ok := c.Locals("tenant_id").(int64); ok {
		return tid
	}
	return 0
}

// AttachMetadata attaches a key-value metadata tag to an email.
// POST /api/v1/emails/:messageId/metadata
func (h *MetadataHandlers) AttachMetadata(c *fiber.Ctx) error {
	messageID := c.Params("messageId")
	if messageID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "messageId is required"})
	}

	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	if req.Key == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "key is required"})
	}

	record, err := h.DB.AttachMetadata(c.Context(), h.tenantID(c), messageID, req.Key, req.Value)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(record)
}

// GetMetadata returns all metadata tags for a specific email.
// GET /api/v1/emails/:messageId/metadata
func (h *MetadataHandlers) GetMetadata(c *fiber.Ctx) error {
	messageID := c.Params("messageId")
	if messageID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "messageId is required"})
	}

	records, err := h.DB.GetMetadataForMessage(c.Context(), h.tenantID(c), messageID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if records == nil {
		records = []models.MetadataRecord{}
	}

	return c.JSON(fiber.Map{"metadata": records})
}

// UpdateMetadata updates the value of a specific metadata key for an email.
// PATCH /api/v1/emails/:messageId/metadata/:key
func (h *MetadataHandlers) UpdateMetadata(c *fiber.Ctx) error {
	messageID := c.Params("messageId")
	key := c.Params("key")
	if messageID == "" || key == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "messageId and key are required"})
	}

	var req struct {
		Value string `json:"value"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	record, err := h.DB.UpdateMetadata(c.Context(), h.tenantID(c), messageID, key, req.Value)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "metadata key not found"})
	}

	return c.JSON(record)
}

// DeleteMetadata removes a specific metadata key from an email.
// DELETE /api/v1/emails/:messageId/metadata/:key
func (h *MetadataHandlers) DeleteMetadata(c *fiber.Ctx) error {
	messageID := c.Params("messageId")
	key := c.Params("key")
	if messageID == "" || key == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "messageId and key are required"})
	}

	if err := h.DB.RemoveMetadata(c.Context(), h.tenantID(c), messageID, key); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// SearchByMetadata queries emails by metadata key-value pair.
// GET /api/v1/metadata/search?key=X&value=Y&exclude_id=Z
func (h *MetadataHandlers) SearchByMetadata(c *fiber.Ctx) error {
	key := c.Query("key")
	value := c.Query("value")
	excludeID := c.Query("exclude_id")

	if key == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "key query parameter is required"})
	}

	records, err := h.DB.QueryByMetadata(c.Context(), h.tenantID(c), key, value, excludeID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if records == nil {
		records = []models.MetadataRecord{}
	}

	return c.JSON(fiber.Map{
		"results": records,
		"total":   len(records),
	})
}

// BatchAttachMetadata attaches the same metadata to multiple emails.
// POST /api/v1/metadata/batch-attach
func (h *MetadataHandlers) BatchAttachMetadata(c *fiber.Ctx) error {
	var req struct {
		MessageIDs []string `json:"message_ids"`
		Key        string   `json:"key"`
		Value      string   `json:"value"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	if req.Key == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "key is required"})
	}
	if len(req.MessageIDs) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "message_ids is required"})
	}

	records, err := h.DB.BatchAttachMetadata(c.Context(), h.tenantID(c), req.MessageIDs, req.Key, req.Value)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"attached": len(records),
		"results":  records,
	})
}

// BatchQueryMetadata queries multiple metadata keys at once.
// POST /api/v1/metadata/batch-query
func (h *MetadataHandlers) BatchQueryMetadata(c *fiber.Ctx) error {
	var req struct {
		Queries []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"queries"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	if len(req.Queries) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "queries is required"})
	}

	// Convert to the format expected by the DB method
	dbQueries := make([]struct{ Key, Value string }, len(req.Queries))
	for i, q := range req.Queries {
		if q.Key == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fmt.Sprintf("query %d: key is required", i),
			})
		}
		dbQueries[i] = struct{ Key, Value string }{Key: q.Key, Value: q.Value}
	}

	results, err := h.DB.BatchQueryMetadata(c.Context(), h.tenantID(c), dbQueries)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"results": results})
}
