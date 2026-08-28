package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/store"
	"github.com/Unn0ne/relayforge/internal/webhook"
)

func TestProcessSuccessfulDelivery(t *testing.T) {
	completedAt := time.Now().UTC()
	statusCode := 204
	queue := &queueStub{}
	dispatcher := dispatcherStub{result: webhook.Result{
		Decision:      delivery.DecisionSucceed,
		StatusCode:    &statusCode,
		CircuitSignal: webhook.CircuitSuccess,
		Duration:      10 * time.Millisecond,
		StartedAt:     completedAt.Add(-10 * time.Millisecond),
		CompletedAt:   completedAt,
	}}
	pool := testPool(t, queue, dispatcher)

	err := pool.process(context.Background(), "worker-id", testJob())
	if err != nil {
		t.Fatal(err)
	}
	if queue.finished.Decision != delivery.DecisionSucceed || queue.finished.CircuitOutcome != store.CircuitSuccess {
		t.Fatalf("finished = %+v", queue.finished)
	}
	if !queue.finished.NextAttemptAt.IsZero() {
		t.Fatalf("next attempt = %s", queue.finished.NextAttemptAt)
	}
}

func TestProcessSchedulesRetry(t *testing.T) {
	completedAt := time.Now().UTC()
	statusCode := 503
	queue := &queueStub{}
	dispatcher := dispatcherStub{result: webhook.Result{
		Decision:      delivery.DecisionRetry,
		StatusCode:    &statusCode,
		CircuitSignal: webhook.CircuitFailure,
		Duration:      10 * time.Millisecond,
		StartedAt:     completedAt.Add(-10 * time.Millisecond),
		CompletedAt:   completedAt,
	}}
	pool := testPool(t, queue, dispatcher)
	pool.sample = func() float64 { return 0.5 }

	err := pool.process(context.Background(), "worker-id", testJob())
	if err != nil {
		t.Fatal(err)
	}
	if queue.finished.NextAttemptAt != completedAt.Add(time.Second) {
		t.Fatalf("next attempt = %s", queue.finished.NextAttemptAt)
	}
	if queue.finished.CircuitOutcome != store.CircuitFailure || queue.finished.CircuitThreshold != 3 {
		t.Fatalf("finished = %+v", queue.finished)
	}
}

func testPool(t *testing.T, queue Queue, dispatcher Dispatcher) *Pool {
	t.Helper()
	policy, err := delivery.NewRetryPolicy(time.Second, time.Minute, 0)
	if err != nil {
		t.Fatal(err)
	}
	result, err := New(queue, dispatcher, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		Concurrency:      1,
		PollInterval:     time.Millisecond,
		LeaseDuration:    time.Minute,
		FinishTimeout:    time.Second,
		RetryPolicy:      policy,
		CircuitThreshold: 3,
		CircuitCooldown:  time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func testJob() *store.Job {
	return &store.Job{
		Delivery: delivery.Delivery{ID: "delivery-id", AttemptCount: 1, LeaseToken: "lease-token"},
		Event:    delivery.Event{ID: "event-id", Type: "invoice.paid", Payload: []byte(`{}`)},
		Endpoint: delivery.Endpoint{ID: "endpoint-id", URL: "https://example.com", Timeout: time.Second},
	}
}

type queueStub struct {
	finished store.AttemptResult
}

func (*queueStub) ClaimDelivery(context.Context, string, time.Duration) (*store.Job, error) {
	return nil, nil
}

func (q *queueStub) FinishAttempt(_ context.Context, result store.AttemptResult) (delivery.Status, error) {
	q.finished = result
	if result.Decision == delivery.DecisionSucceed {
		return delivery.StatusSucceeded, nil
	}
	return delivery.StatusRetrying, nil
}

type dispatcherStub struct {
	result webhook.Result
}

func (d dispatcherStub) Deliver(context.Context, webhook.Request) webhook.Result {
	return d.result
}
