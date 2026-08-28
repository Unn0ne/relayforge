package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/eventing"
	"github.com/google/uuid"
)

func TestCircuitBreakerIntegration(t *testing.T) {
	ctx := context.Background()
	store, pool := openIntegrationStore(t)
	endpointID := seedEndpoint(t, ctx, pool, false)
	for current := 0; current < 3; current++ {
		_, err := store.EnqueueEvent(ctx, eventing.EnqueueParams{
			EventID:        uuid.NewString(),
			DeliveryID:     uuid.NewString(),
			EndpointID:     endpointID,
			Type:           "invoice.paid",
			Payload:        json.RawMessage(`{"id":"invoice-id"}`),
			IdempotencyKey: uuid.NewString(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	for attempt := 1; attempt <= 2; attempt++ {
		workerID := uuid.NewString()
		job, err := store.ClaimDelivery(ctx, workerID, 30*time.Second)
		if err != nil || job == nil {
			t.Fatalf("claim %d: job=%v error=%v", attempt, job, err)
		}
		completedAt := time.Now().UTC()
		statusCode := 503
		_, err = store.FinishAttempt(ctx, AttemptResult{
			DeliveryID:       job.Delivery.ID,
			WorkerID:         workerID,
			LeaseToken:       job.Delivery.LeaseToken,
			AttemptNumber:    job.Delivery.AttemptCount,
			Decision:         delivery.DecisionRetry,
			StatusCode:       &statusCode,
			StartedAt:        completedAt.Add(-time.Millisecond),
			CompletedAt:      completedAt,
			Duration:         time.Millisecond,
			NextAttemptAt:    completedAt.Add(time.Minute),
			CircuitOutcome:   CircuitFailure,
			CircuitThreshold: 2,
			CircuitCooldown:  time.Minute,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	job, err := store.ClaimDelivery(ctx, "blocked-worker", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if job != nil {
		t.Fatal("delivery was claimed while circuit was open")
	}

	var consecutiveFailures int
	var circuitOpenUntil *time.Time
	if err = pool.QueryRow(ctx, `
        SELECT consecutive_failures, circuit_open_until
        FROM endpoints
        WHERE id = $1`, endpointID).Scan(&consecutiveFailures, &circuitOpenUntil); err != nil {
		t.Fatal(err)
	}
	if consecutiveFailures != 2 || circuitOpenUntil == nil || !circuitOpenUntil.After(time.Now()) {
		t.Fatalf("failures=%d circuit_open_until=%v", consecutiveFailures, circuitOpenUntil)
	}

	if _, err = pool.Exec(ctx, `UPDATE endpoints SET circuit_open_until = now() - interval '1 second' WHERE id = $1`, endpointID); err != nil {
		t.Fatal(err)
	}
	job, err = store.ClaimDelivery(ctx, "recovery-worker", 30*time.Second)
	if err != nil || job == nil {
		t.Fatalf("recovery claim: job=%v error=%v", job, err)
	}
	completedAt := time.Now().UTC()
	statusCode := 204
	_, err = store.FinishAttempt(ctx, AttemptResult{
		DeliveryID:     job.Delivery.ID,
		WorkerID:       "recovery-worker",
		LeaseToken:     job.Delivery.LeaseToken,
		AttemptNumber:  job.Delivery.AttemptCount,
		Decision:       delivery.DecisionSucceed,
		StatusCode:     &statusCode,
		StartedAt:      completedAt.Add(-time.Millisecond),
		CompletedAt:    completedAt,
		Duration:       time.Millisecond,
		CircuitOutcome: CircuitSuccess,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err = pool.QueryRow(ctx, `
        SELECT consecutive_failures, circuit_open_until
        FROM endpoints
        WHERE id = $1`, endpointID).Scan(&consecutiveFailures, &circuitOpenUntil); err != nil {
		t.Fatal(err)
	}
	if consecutiveFailures != 0 || circuitOpenUntil != nil {
		t.Fatalf("failures=%d circuit_open_until=%v", consecutiveFailures, circuitOpenUntil)
	}
}
