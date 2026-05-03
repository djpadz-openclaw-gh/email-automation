package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/api"
	"github.com/djpadz/email-automation/internal/config"
	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/engine"
	"github.com/djpadz/email-automation/internal/imap"
	natsbus "github.com/djpadz/email-automation/internal/nats"
	"github.com/djpadz/email-automation/internal/notifier"
	"github.com/djpadz/email-automation/internal/scheduler"
)

func main() {
	// Load config
	cfg := config.Load()

	// Setup logging
	setupLogging(cfg)

	log.Info().Msg("starting email-automation")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Database
	database, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer database.Close()
	log.Info().Msg("connected to database")

	// Run migrations
	if err := database.RunMigrations(ctx, "./migrations"); err != nil {
		log.Fatal().Err(err).Msg("failed to run migrations")
	}

	// NATS
	var bus *natsbus.Bus
	if cfg.NatsURL != "" {
		bus, err = natsbus.New(cfg.NatsURL)
		if err != nil {
			log.Warn().Err(err).Msg("failed to connect to NATS, continuing without event bus")
		} else {
			defer bus.Close()
		}
	}

	// Rule engine with Kiro semantic evaluation
	var kiroClient *engine.KiroClient
	if cfg.KiroAPIKey != "" {
		kiroClient = engine.NewKiroClient(cfg.KiroAPIKey, cfg.KiroAPIURL)
		log.Info().Msg("kiro semantic evaluation enabled")
	}
	eng := engine.NewWithKiro(kiroClient)

	// Telegram notifier
	telegram := notifier.NewTelegram(cfg.TelegramBotToken, cfg.TelegramChatID)
	if telegram.Enabled() {
		log.Info().Msg("telegram notifications enabled")
	}

	// Scheduler
	sched := scheduler.New(database, telegram, cfg.SchedulerInterval)
	go sched.Start(ctx)
	defer sched.Stop()

	// IMAP worker pool
	pool := imap.NewPool(database, eng, bus, telegram, cfg.IMAPIdleTimeout, cfg.IMAPPollInterval)
	if err := pool.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to start IMAP worker pool")
	}
	defer pool.Stop()

	// API server
	server := api.NewServer(cfg, database, eng)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := server.Start(); err != nil {
			log.Fatal().Err(err).Msg("server failed")
		}
	}()

	log.Info().Int("port", cfg.ServerPort).Msg("server started")

	// Wait for shutdown signal
	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("shutting down")

	// Give 10 seconds for graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	cancel() // Cancel main context

	if err := server.Shutdown(); err != nil {
		log.Error().Err(err).Msg("server shutdown error")
	}

	_ = shutdownCtx // Used by deferred cleanup
	log.Info().Msg("shutdown complete")
}

func setupLogging(cfg *config.Config) {
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	if !cfg.LogJSON {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	zerolog.TimeFieldFormat = time.RFC3339
}
