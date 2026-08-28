package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/eventing"
	"github.com/google/uuid"
)

func TestDeliveryInspectionAndReplayIntegration(t *testing.T) {
	ctx := context.Background()
	store, pool := openIntegrationStore(t)
	endpointID := seedEndpoint(t, ctx, pool, false)
	enqueued, err := store.EnqueueEvent(ctx, eventing.EnqueueParams{
		EventID:        uuid.NewString(),
		DeliveryID:     uuid.NewString(),
		EndpointID:     endpointID,
		Type:           "invoice.paid",
		Payload:        json.RawMessage(`{"id":"invoice-id"}`),
		IdempotencyKey: "request-id",
	})
	if err != nil {
		t.Fatal(err)
	}

	completedAt := time.Now().UTC()
	statusCode := 503
	_, err = pool.Exec(ctx, `
        UPDATE deliveries
        SET status = 'dead',
            attempt_count = max_attempts,
            last_status_code = $2,
            last_error = '',
            last_completed_at = $3
        WHERE id = $1`, enqueued.Delivery.ID, statusCode, completedAt)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
        INSERT INTO delivery_attempts (
            id, delivery_id, attempt_number, status_code, response_body,
            duration_ms, started_at, completed_at
        )
        VALUES ($1, $2, 5, $3, 'unavailable', 25, $4, $5)`,
		uuid.NewString(), enqueued.Delivery.ID, statusCode, completedAt.Add(-25*time.Millisecond), completedAt)
	if err != nil {
		t.Fatal(err)
	}

	details, err := store.GetDelivery(ctx, enqueued.Delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	if details.Delivery.Status != delivery.StatusDead || details.Event.Type != "invoice.paid" {
		t.Fatalf("details = %+v", details)
	}
	if len(details.Attempts) != 1 || details.Attempts[0].StatusCode == nil || *details.Attempts[0].StatusCode != 503 {
		t.Fatalf("attempts = %+v", details.Attempts)
	}

	replayed, err := store.ReplayDelivery(ctx, enqueued.Delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Status != delivery.StatusPending || replayed.AttemptCount != 5 || replayed.MaxAttempts != 10 {
		t.Fatalf("replayed = %+v", replayed)
	}

	job, err := store.ClaimDelivery(ctx, "replay-worker", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.Delivery.ID != enqueued.Delivery.ID || job.Delivery.AttemptCount != 6 {
		t.Fatalf("job = %+v", job)
	}
}

func TestReplayRequiresDeadDeliveryIntegration(t *testing.T) {
	ctx := context.Background()
	store, pool := openIntegrationStore(t)
	endpointID := seedEndpoint(t, ctx, pool, false)
	enqueued, err := store.EnqueueEvent(ctx, eventing.EnqueueParams{
		EventID:        uuid.NewString(),
		DeliveryID:     uuid.NewString(),
		EndpointID:     endpointID,
		Type:           "invoice.paid",
		Payload:        json.RawMessage(`{}`),
		IdempotencyKey: "request-id",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.ReplayDelivery(ctx, enqueued.Delivery.ID)
	if !errors.Is(err, delivery.ErrNotReplayable) {
		t.Fatalf("error = %v", err)
	}
}

func TestReplayRejectsDisabledEndpointIntegration(t *testing.T) {
	ctx := context.Background()
	store, pool := openIntegrationStore(t)
	endpointID := seedEndpoint(t, ctx, pool, false)
	enqueued, err := store.EnqueueEvent(ctx, eventing.EnqueueParams{
		EventID:        uuid.NewString(),
		DeliveryID:     uuid.NewString(),
		EndpointID:     endpointID,
		Type:           "invoice.paid",
		Payload:        json.RawMessage(`{}`),
		IdempotencyKey: "request-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `UPDATE deliveries SET status = 'dead', attempt_count = max_attempts WHERE id = $1`, enqueued.Delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `UPDATE endpoints SET disabled_at = now() WHERE id = $1`, endpointID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.ReplayDelivery(ctx, enqueued.Delivery.ID)
	if !errors.Is(err, delivery.ErrEndpointDisabled) {
		t.Fatalf("error = %v", err)
	}
}
