package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration.
type Config struct {
	// Server
	ServerAddr string
	ServerPort int

	// Database
	DatabaseURL string

	// NATS
	NatsURL string

	// IMAP
	IMAPIdleTimeout  time.Duration
	IMAPWorkerCount  int
	IMAPPollInterval time.Duration

	// Scheduler
	SchedulerInterval time.Duration

	// Notifier
	TelegramBotToken string
	TelegramChatID   string

	// Rules
	RulesDir string

	// Auth
	APIKey string

	// Logging
	LogLevel string
	LogJSON  bool
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	return &Config{
		ServerAddr:        envStr("SERVER_ADDR", "0.0.0.0"),
		ServerPort:        envInt("SERVER_PORT", 8080),
		DatabaseURL:       envStr("DATABASE_URL", "postgres://emailauto:emailauto@localhost:5432/emailauto?sslmode=disable"),
		NatsURL:           envStr("NATS_URL", "nats://localhost:4222"),
		IMAPIdleTimeout:   envDuration("IMAP_IDLE_TIMEOUT", 25*time.Minute),
		IMAPWorkerCount:   envInt("IMAP_WORKER_COUNT", 4),
		IMAPPollInterval:  envDuration("IMAP_POLL_INTERVAL", 5*time.Minute),
		SchedulerInterval: envDuration("SCHEDULER_INTERVAL", 30*time.Second),
		TelegramBotToken:  envStr("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:    envStr("TELEGRAM_CHAT_ID", ""),
		RulesDir:          envStr("RULES_DIR", "./rules"),
		APIKey:            envStr("API_KEY", ""),
		LogLevel:          envStr("LOG_LEVEL", "info"),
		LogJSON:           envBool("LOG_JSON", true),
	}
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
