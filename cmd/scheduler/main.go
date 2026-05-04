package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/djpadz/email-automation/internal/config"
	"github.com/djpadz/email-automation/internal/crypto"
	"github.com/djpadz/email-automation/internal/db"
	natsbus "github.com/djpadz/email-automation/internal/nats"
	"github.com/djpadz/email-automation/internal/notifier"
	"github.com/djpadz/email-automation/internal/sched"
)

func main() {
	cfg := config.Load()
	setupLogging(cfg)

	log.Info().Msg("starting scheduler")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Database
	database, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer database.Close()
	log.Info().Msg("connected to database")

	// Password encryption (needed for IMAP action execution)
	if cfg.EncryptionMasterKey != "" {
		keys := make(map[int]string)
		keys[cfg.EncryptionMasterKeyVersion] = cfg.EncryptionMasterKey
		for v, k := range cfg.EncryptionPreviousKeys {
			keys[v] = k
		}
		enc, encErr := crypto.NewVersionedEncryptor(keys, cfg.EncryptionMasterKeyVersion)
		if encErr != nil {
			log.Fatal().Err(encErr).Msg("failed to initialize password encryption")
		}
		database.SetEncryptor(enc)
		log.Info().Int("version", cfg.EncryptionMasterKeyVersion).Msg("password encryption enabled")
	}

	// NATS with JetStream
	if cfg.NatsURL == "" {
		log.Fatal().Msg("NATS_URL is required for scheduler")
	}
	bus, err := natsbus.New(cfg.NatsURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to NATS")
	}
	defer bus.Close()

	// Ensure JetStream streams exist
	if err := bus.EnsureStreams(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to ensure JetStream streams")
	}

	// Telegram notifier
	telegram := notifier.NewTelegram(cfg.TelegramBotToken, cfg.TelegramChatID)
	if telegram.Enabled() {
		log.Info().Msg("telegram notifications enabled")
	}

	// Start scheduler
	s := sched.New(database, bus, telegram, cfg.SchedulerInterval)
	go s.Start(ctx)
	defer s.Stop()

	log.Info().Dur("interval", cfg.SchedulerInterval).Msg("scheduler started")

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("shutting down scheduler")
	cancel()

	log.Info().Msg("scheduler shutdown complete")
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
