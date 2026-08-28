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
	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/endpoint"
	"github.com/Unn0ne/relayforge/internal/eventing"
	"github.com/Unn0ne/relayforge/internal/observability"
	"github.com/Unn0ne/relayforge/internal/secure"
	"github.com/Unn0ne/relayforge/internal/store"
	"github.com/Unn0ne/relayforge/internal/webhook"
	"github.com/Unn0ne/relayforge/internal/worker"
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

	secretBox, err := secure.NewBox(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("initialize secret encryption: %w", err)
	}
	repository := store.New(db.Pool())
	metrics := observability.NewMetrics(repository, db.Pool())
	endpointService := endpoint.New(repository, secretBox, endpoint.Options{
		AllowHTTP:           cfg.AllowHTTP,
		AllowPrivateTargets: cfg.AllowPrivateTargets,
	})
	eventService := eventing.New(repository)
	deliveryService := delivery.NewService(repository)
	retryPolicy, err := delivery.NewRetryPolicy(cfg.RetryBaseDelay, cfg.RetryMaxDelay, cfg.RetryJitter)
	if err != nil {
		return fmt.Errorf("initialize retry policy: %w", err)
	}
	dispatcher := webhook.NewDispatcher(secretBox, webhook.Options{
		AllowHTTP:           cfg.AllowHTTP,
		AllowPrivateTargets: cfg.AllowPrivateTargets,
	})
	defer dispatcher.Close()
	workerPool, err := worker.New(repository, dispatcher, logger, worker.Config{
		Concurrency:      cfg.WorkerConcurrency,
		PollInterval:     cfg.WorkerPollInterval,
		LeaseDuration:    cfg.WorkerLeaseDuration,
		FinishTimeout:    cfg.WorkerFinishTimeout,
		RetryPolicy:      retryPolicy,
		CircuitThreshold: cfg.CircuitFailureThreshold,
		CircuitCooldown:  cfg.CircuitCooldown,
		Observer:         metrics,
	})
	if err != nil {
		return fmt.Errorf("initialize delivery workers: %w", err)
	}

	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.New(logger, api.Dependencies{
			Readiness:    db.Ping,
			APIKey:       cfg.APIKey,
			Endpoints:    endpointService,
			Events:       eventService,
			Deliveries:   deliveryService,
			Metrics:      metrics.Handler(),
			HTTPObserver: metrics,
		}).Handler(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	workersDone := make(chan struct{})
	go func() {
		workerPool.Run(ctx)
		close(workersDone)
	}()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server started", "addr", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()

	var serveErr error
	select {
	case <-ctx.Done():
		logger.Info("shutdown started")
	case err = <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = fmt.Errorf("serve http: %w", err)
		}
	}
	stop()

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

	select {
	case <-workersDone:
	case <-shutdownCtx.Done():
		return errors.New("delivery workers did not stop before shutdown timeout")
	}
	if serveErr != nil {
		return serveErr
	}

	logger.Info("shutdown completed")
	return nil
}
