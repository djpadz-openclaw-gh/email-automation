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
	"github.com/djpadz/email-automation/internal/crypto"
	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/engine"
)

func main() {
	cfg := config.Load()
	setupLogging(cfg)

	log.Info().Msg("starting email-automation API server")

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
	if err := database.RunMigrations(ctx, cfg.MigrationsDir); err != nil {
		log.Fatal().Err(err).Msg("failed to run migrations")
	}

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
		log.Info().Int("version", cfg.EncryptionMasterKeyVersion).Int("total_keys", len(keys)).Msg("password encryption enabled")
	} else {
		log.Warn().Msg("ENCRYPTION_MASTER_KEY not set, passwords stored in plaintext")
	}

	// Rule engine (for test/validate/dry-run endpoints)
	var kiroClient *engine.KiroClient
	if cfg.KiroAPIKey != "" {
		kiroClient = engine.NewKiroClient(cfg.KiroAPIKey, cfg.KiroAPIURL, cfg.KiroTextModel, cfg.KiroVisionModel)
		log.Info().Msg("kiro semantic evaluation enabled")
	}
	eng := engine.NewWithKiro(kiroClient)

	// API server
	server := api.NewServer(cfg, database, eng)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := server.Start(); err != nil {
			log.Fatal().Err(err).Msg("API server failed")
		}
	}()

	log.Info().Int("port", cfg.ServerPort).Msg("API server started")

	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("shutting down API server")

	cancel()

	if err := server.Shutdown(); err != nil {
		log.Error().Err(err).Msg("server shutdown error")
	}

	log.Info().Msg("API server shutdown complete")
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
