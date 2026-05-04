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
	"github.com/djpadz/email-automation/internal/imaplistener"
	"github.com/djpadz/email-automation/internal/movedetect"
	natsbus "github.com/djpadz/email-automation/internal/nats"
	"github.com/djpadz/email-automation/internal/notifier"
)

func main() {
	cfg := config.Load()
	setupLogging(cfg)

	log.Info().Msg("starting IMAP listener")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Database
	database, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer database.Close()
	log.Info().Msg("connected to database")

	// Password encryption
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
		log.Fatal().Msg("NATS_URL is required for IMAP listener")
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
	var telegram *notifier.Telegram
	if cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
		telegram = notifier.NewTelegram(cfg.TelegramBotToken, cfg.TelegramChatID)
		log.Info().Msg("Telegram notifications enabled")
	}

	// Start IMAP listener
	listener := imaplistener.New(database, bus, cfg.IMAPIdleTimeout, cfg.IMAPPollInterval)
	if err := listener.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to start IMAP listener")
	}
	defer listener.Stop()

	log.Info().Msg("IMAP listener started")

	// Start move detector
	// Poll interval for move detection is 2x the IMAP poll interval to avoid
	// detecting our own rule-engine moves as manual moves.
	moveDetectInterval := cfg.IMAPPollInterval * 2
	if moveDetectInterval < 2*time.Minute {
		moveDetectInterval = 2 * time.Minute
	}
	detector := movedetect.New(database, telegram, moveDetectInterval)
	if err := detector.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to start move detector")
	}
	defer detector.Stop()

	log.Info().Dur("poll_interval", moveDetectInterval).Msg("move detector started")

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("shutting down IMAP listener")
	cancel()

	log.Info().Msg("IMAP listener shutdown complete")
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
