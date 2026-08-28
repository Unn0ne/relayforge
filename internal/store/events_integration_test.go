package store

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/Unn0ne/relayforge/internal/eventing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdempotentEventEnqueueIntegration(t *testing.T) {
	ctx := context.Background()
	store, pool := openIntegrationStore(t)
	endpointID := seedEndpoint(t, ctx, pool, false)
	params := eventing.EnqueueParams{
		EventID:        uuid.NewString(),
		DeliveryID:     uuid.NewString(),
		EndpointID:     endpointID,
		Type:           "invoice.paid",
		Payload:        json.RawMessage(`{"id":"invoice-id"}`),
		IdempotencyKey: "request-id",
	}

	first, err := store.EnqueueEvent(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if first.Duplicate || first.Event.ID != params.EventID || first.Delivery.ID != params.DeliveryID {
		t.Fatalf("first result = %+v", first)
	}

	duplicateParams := params
	duplicateParams.EventID = uuid.NewString()
	duplicateParams.DeliveryID = uuid.NewString()
	second, err := store.EnqueueEvent(ctx, duplicateParams)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || second.Event.ID != first.Event.ID || second.Delivery.ID != first.Delivery.ID {
		t.Fatalf("duplicate result = %+v", second)
	}

	conflictingParams := duplicateParams
	conflictingParams.EventID = uuid.NewString()
	conflictingParams.DeliveryID = uuid.NewString()
	conflictingParams.Payload = json.RawMessage(`{"id":"other-invoice"}`)
	_, err = store.EnqueueEvent(ctx, conflictingParams)
	if !errors.Is(err, eventing.ErrIdempotencyConflict) {
		t.Fatalf("conflict error = %v", err)
	}

	var events int
	var deliveries int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM deliveries`).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if events != 1 || deliveries != 1 {
		t.Fatalf("events=%d deliveries=%d", events, deliveries)
	}
}

func TestConcurrentIdempotentEnqueueIntegration(t *testing.T) {
	ctx := context.Background()
	store, pool := openIntegrationStore(t)
	endpointID := seedEndpoint(t, ctx, pool, false)
	const publishers = 16

	start := make(chan struct{})
	results := make(chan eventing.Enqueued, publishers)
	errs := make(chan error, publishers)
	var workers sync.WaitGroup

	for current := 0; current < publishers; current++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, err := store.EnqueueEvent(ctx, eventing.EnqueueParams{
				EventID:        uuid.NewString(),
				DeliveryID:     uuid.NewString(),
				EndpointID:     endpointID,
				Type:           "invoice.paid",
				Payload:        json.RawMessage(`{"id":"invoice-id"}`),
				IdempotencyKey: "shared-request-id",
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}

	close(start)
	workers.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}
	var eventID string
	var deliveryID string
	newResults := 0
	for result := range results {
		if eventID == "" {
			eventID = result.Event.ID
			deliveryID = result.Delivery.ID
		}
		if result.Event.ID != eventID || result.Delivery.ID != deliveryID {
			t.Fatalf("non-idempotent result = %+v", result)
		}
		if !result.Duplicate {
			newResults++
		}
	}
	if newResults != 1 {
		t.Fatalf("new results = %d", newResults)
	}
}

func TestDisabledEndpointRejectsEventIntegration(t *testing.T) {
	ctx := context.Background()
	store, pool := openIntegrationStore(t)
	endpointID := seedEndpoint(t, ctx, pool, true)

	_, err := store.EnqueueEvent(ctx, eventing.EnqueueParams{
		EventID:        uuid.NewString(),
		DeliveryID:     uuid.NewString(),
		EndpointID:     endpointID,
		Type:           "invoice.paid",
		Payload:        json.RawMessage(`{"id":"invoice-id"}`),
		IdempotencyKey: "request-id",
	})
	if !errors.Is(err, eventing.ErrEndpointDisabled) {
		t.Fatalf("error = %v", err)
	}
}

func seedEndpoint(t *testing.T, ctx context.Context, pool *pgxpool.Pool, disabled bool) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(ctx, `
        INSERT INTO endpoints (id, name, url, secret_ciphertext, max_attempts, disabled_at)
        VALUES ($1, 'test', 'https://example.com/hooks', $2, 5, CASE WHEN $3 THEN now() END)`,
		id, []byte("ciphertext"), disabled)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
