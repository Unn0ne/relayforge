package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/endpoint"
	"github.com/Unn0ne/relayforge/internal/eventing"
	"github.com/Unn0ne/relayforge/internal/secure"
	"github.com/Unn0ne/relayforge/internal/store"
	"github.com/Unn0ne/relayforge/internal/webhook"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWorkerDeliveryIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	database, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	resetWorkerSchema(t, ctx, database)
	migration, err := os.ReadFile("../../migrations/001_init.up.sql")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err = database.Exec(ctx, string(migration)); err != nil {
		database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		resetWorkerSchema(t, context.Background(), database)
		database.Close()
	})

	type receivedRequest struct {
		body       []byte
		deliveryID string
		timestamp  string
		signature  string
	}
	received := make(chan receivedRequest, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Error(readErr)
		}
		received <- receivedRequest{
			body:       body,
			deliveryID: r.Header.Get("X-RelayForge-Delivery"),
			timestamp:  r.Header.Get("X-RelayForge-Timestamp"),
			signature:  r.Header.Get("X-RelayForge-Signature"),
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	box, err := secure.NewBox(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	repository := store.New(database)
	endpointService := endpoint.New(repository, box, endpoint.Options{AllowHTTP: true, AllowPrivateTargets: true})
	created, err := endpointService.Create(ctx, endpoint.CreateInput{Name: "receiver", URL: receiver.URL, MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	eventService := eventing.New(repository)
	published, err := eventService.Publish(ctx, eventing.PublishInput{
		EndpointID:     created.Endpoint.ID,
		Type:           "invoice.paid",
		Payload:        json.RawMessage(`{"id":"invoice-id"}`),
		IdempotencyKey: "request-id",
	})
	if err != nil {
		t.Fatal(err)
	}

	dispatcher := webhook.NewDispatcher(box, webhook.Options{AllowHTTP: true, AllowPrivateTargets: true})
	defer dispatcher.Close()
	retryPolicy, err := delivery.NewRetryPolicy(time.Second, time.Minute, 0)
	if err != nil {
		t.Fatal(err)
	}
	workers, err := New(repository, dispatcher, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		Concurrency:      2,
		PollInterval:     10 * time.Millisecond,
		LeaseDuration:    10 * time.Second,
		FinishTimeout:    time.Second,
		RetryPolicy:      retryPolicy,
		CircuitThreshold: 3,
		CircuitCooldown:  time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	workersDone := make(chan struct{})
	go func() {
		workers.Run(workerCtx)
		close(workersDone)
	}()

	var request receivedRequest
	select {
	case request = <-received:
	case <-time.After(5 * time.Second):
		cancelWorkers()
		t.Fatal("webhook was not delivered")
	}

	deliverySucceeded := false
	for attempt := 0; attempt < 100; attempt++ {
		details, getErr := repository.GetDelivery(ctx, published.Delivery.ID)
		if getErr != nil {
			cancelWorkers()
			t.Fatal(getErr)
		}
		if details.Delivery.Status == delivery.StatusSucceeded && len(details.Attempts) == 1 {
			deliverySucceeded = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancelWorkers()
	select {
	case <-workersDone:
	case <-time.After(2 * time.Second):
		t.Fatal("workers did not stop")
	}
	if !deliverySucceeded {
		t.Fatal("delivery did not reach succeeded state")
	}

	timestamp, err := strconv.ParseInt(request.timestamp, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if request.deliveryID != published.Delivery.ID {
		t.Fatalf("delivery id = %q", request.deliveryID)
	}
	if request.signature != webhook.Sign([]byte(created.Secret), timestamp, request.deliveryID, request.body) {
		t.Fatalf("signature = %q", request.signature)
	}
}

func resetWorkerSchema(t *testing.T, ctx context.Context, database *pgxpool.Pool) {
	t.Helper()
	_, err := database.Exec(ctx, `
        DROP TABLE IF EXISTS delivery_attempts;
        DROP TABLE IF EXISTS deliveries;
        DROP TABLE IF EXISTS events;
        DROP TABLE IF EXISTS endpoints`)
	if err != nil {
		t.Fatal(err)
	}
}
