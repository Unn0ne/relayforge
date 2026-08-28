package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Unn0ne/relayforge/internal/api"
	"github.com/Unn0ne/relayforge/internal/config"
	"github.com/Unn0ne/relayforge/internal/database"
)

func main() {
	if err := run(); err != nil {
		slog.Error("relayforge stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	databaseCtx, databaseCancel := context.WithTimeout(context.Background(), cfg.DatabaseConnectTimeout)
	db, err := database.Open(databaseCtx, database.Config{
		URL:            cfg.DatabaseURL,
		MinConnections: cfg.DatabaseMinConnections,
		MaxConnections: cfg.DatabaseMaxConnections,
	})
	databaseCancel()
	if err != nil {
		return err
	}
	defer db.Close()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.New(logger, db.Ping).Handler(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server started", "addr", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown started")
	case err = <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve http: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err = server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}

	select {
	case err = <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("stop http server: %w", err)
		}
	case <-time.After(100 * time.Millisecond):
	}

	logger.Info("shutdown completed")
	return nil
}
