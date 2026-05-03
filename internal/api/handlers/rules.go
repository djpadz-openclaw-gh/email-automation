package handlers

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/engine"
	"github.com/djpadz/email-automation/internal/models"
)

// RuleHandlers holds dependencies for rule HTTP handlers.
type RuleHandlers struct {
	DB     *db.DB
	Engine *engine.Engine
}

func (h *RuleHandlers) tenantID(c *fiber.Ctx) int64 {
	if tid, ok := c.Locals("tenant_id").(int64); ok {
		return tid
	}
	return 0
}

// ListRules returns all rules for the tenant.
func (h *RuleHandlers) ListRules(c *fiber.Ctx) error {
	rules, err := h.DB.ListRules(c.Context(), h.tenantID(c))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if rules == nil {
		rules = []models.Rule{}
	}
	return c.JSON(rules)
}

// GetRule returns a single rule by ID.
func (h *RuleHandlers) GetRule(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid rule ID"})
	}

	rule, err := h.DB.GetRule(c.Context(), h.tenantID(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "rule not found"})
	}
	return c.JSON(rule)
}

// CreateRule creates a new rule.
func (h *RuleHandlers) CreateRule(c *fiber.Ctx) error {
	var rule models.Rule
	if err := c.BodyParser(&rule); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	rule.TenantID = h.tenantID(c)

	// Validate Lua syntax
	if err := h.Engine.ValidateLua(rule.LuaCode); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "invalid Lua code",
			"details": err.Error(),
		})
	}

	if err := h.DB.CreateRule(c.Context(), &rule); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(rule)
}

// UpdateRule updates an existing rule.
func (h *RuleHandlers) UpdateRule(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid rule ID"})
	}

	var rule models.Rule
	if err := c.BodyParser(&rule); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	rule.ID = id
	rule.TenantID = h.tenantID(c)

	// Validate Lua syntax
	if err := h.Engine.ValidateLua(rule.LuaCode); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "invalid Lua code",
			"details": err.Error(),
		})
	}

	if err := h.DB.UpdateRule(c.Context(), &rule); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(rule)
}

// DeleteRule deletes a rule.
func (h *RuleHandlers) DeleteRule(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid rule ID"})
	}

	if err := h.DB.DeleteRule(c.Context(), h.tenantID(c), id); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// TestRule evaluates a rule against a test email context.
func (h *RuleHandlers) TestRule(c *fiber.Ctx) error {
	var req struct {
		LuaCode string              `json:"lua_code"`
		Email   models.EmailContext  `json:"email"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	rule := &models.Rule{
		Name:    "test",
		LuaCode: req.LuaCode,
	}

	result, err := h.Engine.TestRule(rule, &req.Email)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "rule evaluation failed",
			"details": err.Error(),
		})
	}

	return c.JSON(result)
}

// ValidateRule checks Lua syntax without executing.
func (h *RuleHandlers) ValidateRule(c *fiber.Ctx) error {
	var req struct {
		LuaCode string `json:"lua_code"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if err := h.Engine.ValidateLua(req.LuaCode); err != nil {
		return c.JSON(fiber.Map{
			"valid":   false,
			"error":   err.Error(),
		})
	}

	return c.JSON(fiber.Map{"valid": true})
}

// ListExecutionLogs returns recent rule execution logs.
func (h *RuleHandlers) ListExecutionLogs(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 50)
	if limit > 500 {
		limit = 500
	}

	logs, err := h.DB.ListExecutionLogs(c.Context(), h.tenantID(c), limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if logs == nil {
		logs = []models.RuleExecutionLog{}
	}
	return c.JSON(logs)
}
