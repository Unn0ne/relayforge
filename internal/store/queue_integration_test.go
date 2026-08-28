package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestQueueLifecycleIntegration(t *testing.T) {
	ctx := context.Background()
	store, pool := openIntegrationStore(t)
	deliveryID := seedDelivery(t, ctx, pool, 2)

	job, err := store.ClaimDelivery(ctx, "worker-a", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil {
		t.Fatal("expected a claimed delivery")
	}
	if job.Delivery.ID != deliveryID || job.Delivery.AttemptCount != 1 {
		t.Fatalf("unexpected job: %+v", job.Delivery)
	}
	if job.Delivery.LeaseToken == "" {
		t.Fatal("lease token is empty")
	}

	other, err := store.ClaimDelivery(ctx, "worker-b", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if other != nil {
		t.Fatal("claimed an active lease twice")
	}

	now := time.Now().UTC()
	statusCode := 503
	_, err = store.FinishAttempt(ctx, AttemptResult{
		DeliveryID:    deliveryID,
		WorkerID:      "worker-a",
		LeaseToken:    uuid.NewString(),
		AttemptNumber: 1,
		Decision:      delivery.DecisionRetry,
		StatusCode:    &statusCode,
		StartedAt:     now,
		CompletedAt:   now.Add(10 * time.Millisecond),
		Duration:      10 * time.Millisecond,
		NextAttemptAt: now.Add(time.Minute),
	})
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale token error = %v", err)
	}

	resultStatus, err := store.FinishAttempt(ctx, AttemptResult{
		DeliveryID:    deliveryID,
		WorkerID:      "worker-a",
		LeaseToken:    job.Delivery.LeaseToken,
		AttemptNumber: 1,
		Decision:      delivery.DecisionRetry,
		StatusCode:    &statusCode,
		ResponseBody:  "temporarily unavailable",
		StartedAt:     now,
		CompletedAt:   now.Add(10 * time.Millisecond),
		Duration:      10 * time.Millisecond,
		NextAttemptAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resultStatus != delivery.StatusRetrying {
		t.Fatalf("status = %s", resultStatus)
	}

	if _, err = pool.Exec(ctx, `UPDATE deliveries SET next_attempt_at = now() WHERE id = $1`, deliveryID); err != nil {
		t.Fatal(err)
	}

	job, err = store.ClaimDelivery(ctx, "worker-b", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.Delivery.AttemptCount != 2 {
		t.Fatalf("unexpected retry job: %+v", job)
	}

	statusCode = 204
	resultStatus, err = store.FinishAttempt(ctx, AttemptResult{
		DeliveryID:    deliveryID,
		WorkerID:      "worker-b",
		LeaseToken:    job.Delivery.LeaseToken,
		AttemptNumber: 2,
		Decision:      delivery.DecisionSucceed,
		StatusCode:    &statusCode,
		StartedAt:     now.Add(time.Minute),
		CompletedAt:   now.Add(time.Minute + 5*time.Millisecond),
		Duration:      5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resultStatus != delivery.StatusSucceeded {
		t.Fatalf("status = %s", resultStatus)
	}

	var status delivery.Status
	var attemptCount int
	var recordedAttempts int
	if err = pool.QueryRow(ctx, `SELECT status, attempt_count FROM deliveries WHERE id = $1`, deliveryID).Scan(&status, &attemptCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM delivery_attempts WHERE delivery_id = $1`, deliveryID).Scan(&recordedAttempts); err != nil {
		t.Fatal(err)
	}
	if status != delivery.StatusSucceeded || attemptCount != 2 || recordedAttempts != 2 {
		t.Fatalf("status=%s attempts=%d recorded=%d", status, attemptCount, recordedAttempts)
	}
}

func TestConcurrentClaimsAreUniqueIntegration(t *testing.T) {
	ctx := context.Background()
	store, pool := openIntegrationStore(t)
	const deliveries = 16

	for current := 0; current < deliveries; current++ {
		seedDelivery(t, ctx, pool, 3)
	}

	start := make(chan struct{})
	jobs := make(chan *Job, deliveries)
	errs := make(chan error, deliveries)
	var workers sync.WaitGroup

	for current := 0; current < deliveries; current++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			job, err := store.ClaimDelivery(ctx, uuid.NewString(), 30*time.Second)
			if err != nil {
				errs <- err
				return
			}
			jobs <- job
		}()
	}

	close(start)
	workers.Wait()
	close(jobs)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	claimed := make(map[string]struct{}, deliveries)
	for job := range jobs {
		if job == nil {
			t.Fatal("worker did not claim a delivery")
		}
		if _, exists := claimed[job.Delivery.ID]; exists {
			t.Fatalf("delivery %s was claimed twice", job.Delivery.ID)
		}
		claimed[job.Delivery.ID] = struct{}{}
	}
	if len(claimed) != deliveries {
		t.Fatalf("claimed %d deliveries", len(claimed))
	}
}

func TestExpiredFinalLeaseBecomesDeadIntegration(t *testing.T) {
	ctx := context.Background()
	store, pool := openIntegrationStore(t)
	deliveryID := seedDelivery(t, ctx, pool, 1)

	job, err := store.ClaimDelivery(ctx, "worker-a", 30*time.Second)
	if err != nil || job == nil {
		t.Fatalf("claim: job=%v error=%v", job, err)
	}

	_, err = pool.Exec(ctx, `
        UPDATE deliveries
        SET locked_at = now() - interval '2 seconds',
            locked_until = now() - interval '1 second'
        WHERE id = $1`, deliveryID)
	if err != nil {
		t.Fatal(err)
	}

	job, err = store.ClaimDelivery(ctx, "worker-b", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if job != nil {
		t.Fatal("exhausted delivery was claimed again")
	}

	var status delivery.Status
	var attempts int
	var message string
	err = pool.QueryRow(ctx, `
        SELECT d.status, count(a.id), max(a.error_message)
        FROM deliveries d
        LEFT JOIN delivery_attempts a ON a.delivery_id = d.id
        WHERE d.id = $1
        GROUP BY d.status`, deliveryID).Scan(&status, &attempts, &message)
	if err != nil {
		t.Fatal(err)
	}
	if status != delivery.StatusDead || attempts != 1 || message != "delivery lease expired" {
		t.Fatalf("status=%s attempts=%d message=%q", status, attempts, message)
	}
}

func TestExpiredLeaseIsRecordedAndReclaimedIntegration(t *testing.T) {
	ctx := context.Background()
	store, pool := openIntegrationStore(t)
	deliveryID := seedDelivery(t, ctx, pool, 2)

	job, err := store.ClaimDelivery(ctx, "worker-a", 30*time.Second)
	if err != nil || job == nil {
		t.Fatalf("claim: job=%v error=%v", job, err)
	}

	_, err = pool.Exec(ctx, `
        UPDATE deliveries
        SET locked_at = now() - interval '2 seconds',
            locked_until = now() - interval '1 second'
        WHERE id = $1`, deliveryID)
	if err != nil {
		t.Fatal(err)
	}

	job, err = store.ClaimDelivery(ctx, "worker-b", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.Delivery.ID != deliveryID || job.Delivery.AttemptCount != 2 {
		t.Fatalf("unexpected recovered job: %+v", job)
	}

	var attempts int
	var message string
	err = pool.QueryRow(ctx, `
        SELECT count(*), max(error_message)
        FROM delivery_attempts
        WHERE delivery_id = $1`, deliveryID).Scan(&attempts, &message)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || message != "delivery lease expired" {
		t.Fatalf("attempts=%d message=%q", attempts, message)
	}
}

func openIntegrationStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := "relayforge_store_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		if _, cleanupErr := admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); cleanupErr != nil {
			t.Error(cleanupErr)
		}
		admin.Close()
	})
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/001_init.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	return New(pool), pool
}

func seedDelivery(t *testing.T, ctx context.Context, pool *pgxpool.Pool, maxAttempts int) string {
	t.Helper()
	endpointID := uuid.NewString()
	eventID := uuid.NewString()
	deliveryID := uuid.NewString()

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	_, err = tx.Exec(ctx, `
        INSERT INTO endpoints (id, name, url, secret_ciphertext, max_attempts)
        VALUES ($1, $2, $3, $4, $5)`,
		endpointID, "test endpoint", "https://example.com/hooks", []byte("encrypted"), maxAttempts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `
        INSERT INTO events (id, endpoint_id, event_type, payload, idempotency_key)
        VALUES ($1, $2, $3, $4, $5)`,
		eventID, endpointID, "invoice.paid", []byte(`{"id":"invoice-id"}`), uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `
        INSERT INTO deliveries (id, event_id, endpoint_id, max_attempts)
        VALUES ($1, $2, $3, $4)`, deliveryID, eventID, endpointID, maxAttempts)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return deliveryID
}
