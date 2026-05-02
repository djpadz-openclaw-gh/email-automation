package handlers

import (
	"github.com/gofiber/fiber/v2"
)

// HealthCheck returns service health status.
func HealthCheck(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status":  "ok",
		"service": "email-automation",
	})
}

// ReadyCheck returns service readiness status.
func ReadyCheck(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status": "ready",
	})
}
