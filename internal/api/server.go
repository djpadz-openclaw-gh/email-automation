package api

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/djpadz/email-automation/internal/api/handlers"
	"github.com/djpadz/email-automation/internal/api/middleware"
	"github.com/djpadz/email-automation/internal/config"
	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/engine"
)

// Server is the HTTP API server.
type Server struct {
	app    *fiber.App
	config *config.Config
	db     *db.DB
	engine *engine.Engine
}

// NewServer creates a new API server.
func NewServer(cfg *config.Config, database *db.DB, eng *engine.Engine) *Server {
	app := fiber.New(fiber.Config{
		AppName:      "email-automation",
		ErrorHandler: errorHandler,
	})

	s := &Server{
		app:    app,
		config: cfg,
		db:     database,
		engine: eng,
	}

	s.setupMiddleware()
	s.setupRoutes()

	return s
}

func (s *Server) setupMiddleware() {
	s.app.Use(recover.New())
	s.app.Use(logger.New(logger.Config{
		Format: "${time} ${status} ${method} ${path} ${latency}\n",
	}))
	s.app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, X-API-Key, Authorization",
		AllowMethods: "GET, POST, PUT, DELETE, PATCH, OPTIONS",
	}))
}

func (s *Server) setupRoutes() {
	// Health endpoints (no auth)
	s.app.Get("/health", handlers.HealthCheck)
	s.app.Get("/ready", handlers.ReadyCheck)

	// Admin endpoints (system API key)
	admin := s.app.Group("/admin")
	admin.Use(adminAuth(s.config.APIKey))
	tenantH := &handlers.TenantHandlers{DB: s.db}
	admin.Get("/tenants", tenantH.ListTenants)
	admin.Post("/tenants", tenantH.CreateTenant)

	// API v1 (tenant auth)
	v1 := s.app.Group("/api/v1")
	v1.Use(middleware.AuthMiddleware(s.db))

	// Rules
	ruleH := &handlers.RuleHandlers{DB: s.db, Engine: s.engine}
	v1.Get("/rules", ruleH.ListRules)
	v1.Get("/rules/:id", ruleH.GetRule)
	v1.Post("/rules", ruleH.CreateRule)
	v1.Put("/rules/:id", ruleH.UpdateRule)
	v1.Delete("/rules/:id", ruleH.DeleteRule)
	v1.Post("/rules/test", ruleH.TestRule)
	v1.Post("/rules/validate", ruleH.ValidateRule)
	v1.Get("/logs", ruleH.ListExecutionLogs)

	// Accounts
	accountH := &handlers.AccountHandlers{DB: s.db}
	v1.Get("/accounts", accountH.ListAccounts)
	v1.Get("/accounts/:id", accountH.GetAccount)
	v1.Post("/accounts", accountH.CreateAccount)
	v1.Put("/accounts/:id", accountH.UpdateAccount)
	v1.Delete("/accounts/:id", accountH.DeleteAccount)
}

// Start begins listening on the configured address.
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.ServerAddr, s.config.ServerPort)
	return s.app.Listen(addr)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown() error {
	return s.app.Shutdown()
}

func errorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}
	return c.Status(code).JSON(fiber.Map{"error": err.Error()})
}

func adminAuth(systemKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if systemKey == "" {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "admin API not configured",
			})
		}
		key := c.Get("X-API-Key")
		if key == "" {
			key = c.Get("Authorization")
			if len(key) > 7 {
				key = key[7:]
			}
		}
		if key != systemKey {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid admin API key",
			})
		}
		return c.Next()
	}
}
