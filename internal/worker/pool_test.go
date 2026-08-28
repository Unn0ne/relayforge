package worker

import (
	"context"
	"errors"
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

func TestProcessRecordsAttemptMetric(t *testing.T) {
	completedAt := time.Now().UTC()
	observer := &observerStub{}
	pool := testPool(t, &queueStub{}, dispatcherStub{result: webhook.Result{
		Decision:    delivery.DecisionDiscard,
		Duration:    12 * time.Millisecond,
		StartedAt:   completedAt.Add(-12 * time.Millisecond),
		CompletedAt: completedAt,
	}})
	pool.observer = observer

	if err := pool.process(context.Background(), "worker-id", testJob()); err != nil {
		t.Fatal(err)
	}
	if observer.decision != delivery.DecisionDiscard || observer.duration != 12*time.Millisecond {
		t.Fatalf("observer = %+v", observer)
	}
}

func TestRunObservesWorkerLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	queue := &runQueue{job: testJob(), cancel: cancel}
	completedAt := time.Now().UTC()
	pool := testPool(t, queue, dispatcherStub{result: webhook.Result{
		Decision:    delivery.DecisionSucceed,
		Duration:    time.Millisecond,
		StartedAt:   completedAt.Add(-time.Millisecond),
		CompletedAt: completedAt,
	}})
	observer := &observerStub{}
	pool.observer = observer

	pool.Run(ctx)

	if len(observer.claims) != 1 || observer.claims[0] != "claimed" {
		t.Fatalf("claims = %v", observer.claims)
	}
	if observer.inFlight != 0 || observer.finishErrors != 0 {
		t.Fatalf("observer = %+v", observer)
	}
}

func TestRunRecordsFinishError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	queue := &runQueue{job: testJob(), cancel: cancel, finishErr: errors.New("write failed")}
	completedAt := time.Now().UTC()
	pool := testPool(t, queue, dispatcherStub{result: webhook.Result{
		Decision:    delivery.DecisionRetry,
		Duration:    time.Millisecond,
		StartedAt:   completedAt.Add(-time.Millisecond),
		CompletedAt: completedAt,
	}})
	observer := &observerStub{}
	pool.observer = observer

	pool.Run(ctx)

	if observer.finishErrors != 1 || observer.inFlight != 0 {
		t.Fatalf("observer = %+v", observer)
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

type runQueue struct {
	job       *store.Job
	cancel    context.CancelFunc
	finishErr error
}

func (q *runQueue) ClaimDelivery(ctx context.Context, _ string, _ time.Duration) (*store.Job, error) {
	if q.job != nil {
		job := q.job
		q.job = nil
		return job, nil
	}
	return nil, ctx.Err()
}

func (q *runQueue) FinishAttempt(context.Context, store.AttemptResult) (delivery.Status, error) {
	q.cancel()
	return delivery.StatusSucceeded, q.finishErr
}

func (d dispatcherStub) Deliver(context.Context, webhook.Request) webhook.Result {
	return d.result
}

type observerStub struct {
	decision     delivery.Decision
	duration     time.Duration
	claims       []string
	finishErrors int
	inFlight     float64
}

func (o *observerStub) ObserveClaim(result string) {
	o.claims = append(o.claims, result)
}

func (o *observerStub) ObserveAttempt(decision delivery.Decision, duration time.Duration) {
	o.decision = decision
	o.duration = duration
}

func (o *observerStub) ObserveFinishError() {
	o.finishErrors++
}

func (o *observerStub) AddInFlight(delta float64) {
	o.inFlight += delta
}
