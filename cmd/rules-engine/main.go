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
	"github.com/djpadz/email-automation/internal/engine"
	natsbus "github.com/djpadz/email-automation/internal/nats"
	"github.com/djpadz/email-automation/internal/notifier"
	"github.com/djpadz/email-automation/internal/rulesengine"
)

func main() {
	cfg := config.Load()
	setupLogging(cfg)

	log.Info().Msg("starting rules engine")

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
		log.Fatal().Msg("NATS_URL is required for rules engine")
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

	// Start rules engine consumer
	re := rulesengine.New(database, eng, bus, telegram)
	if err := re.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to start rules engine")
	}
	defer re.Stop()

	log.Info().Msg("rules engine started, consuming from email.incoming")

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("shutting down rules engine")
	cancel()

	log.Info().Msg("rules engine shutdown complete")
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
