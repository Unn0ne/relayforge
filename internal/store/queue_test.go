package store

import (
	"testing"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
)

func TestValidateAttemptResult(t *testing.T) {
	now := time.Now().UTC()
	status := 503
	valid := AttemptResult{
		DeliveryID:    "delivery-id",
		WorkerID:      "worker-id",
		LeaseToken:    "lease-token",
		AttemptNumber: 1,
		Decision:      delivery.DecisionRetry,
		StatusCode:    &status,
		Duration:      time.Second,
		StartedAt:     now,
		CompletedAt:   now.Add(time.Second),
		NextAttemptAt: now.Add(2 * time.Second),
	}

	if err := validateAttemptResult(valid); err != nil {
		t.Fatalf("valid result rejected: %v", err)
	}
	permanentFailure := valid
	permanentFailure.StatusCode = nil
	permanentFailure.ErrorMessage = "invalid encrypted secret"
	permanentFailure.Decision = delivery.DecisionDiscard
	permanentFailure.NextAttemptAt = time.Time{}
	if err := validateAttemptResult(permanentFailure); err != nil {
		t.Fatalf("permanent failure rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*AttemptResult)
	}{
		{name: "missing delivery", mutate: func(r *AttemptResult) { r.DeliveryID = "" }},
		{name: "missing worker", mutate: func(r *AttemptResult) { r.WorkerID = "" }},
		{name: "missing token", mutate: func(r *AttemptResult) { r.LeaseToken = "" }},
		{name: "invalid attempt", mutate: func(r *AttemptResult) { r.AttemptNumber = 0 }},
		{name: "invalid decision", mutate: func(r *AttemptResult) { r.Decision = "unknown" }},
		{name: "invalid status", mutate: func(r *AttemptResult) { value := 99; r.StatusCode = &value }},
		{name: "missing outcome", mutate: func(r *AttemptResult) { r.StatusCode = nil; r.ErrorMessage = "" }},
		{name: "statusless success", mutate: func(r *AttemptResult) {
			r.StatusCode = nil
			r.ErrorMessage = "reset"
			r.Decision = delivery.DecisionSucceed
		}},
		{name: "status decision mismatch", mutate: func(r *AttemptResult) { value := 204; r.StatusCode = &value }},
		{name: "negative duration", mutate: func(r *AttemptResult) { r.Duration = -time.Second }},
		{name: "reversed timestamps", mutate: func(r *AttemptResult) { r.CompletedAt = r.StartedAt.Add(-time.Second) }},
		{name: "invalid retry time", mutate: func(r *AttemptResult) { r.NextAttemptAt = r.CompletedAt }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := valid
			tt.mutate(&result)
			if err := validateAttemptResult(result); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDurationMilliseconds(t *testing.T) {
	if got := durationMilliseconds(1500 * time.Microsecond); got != 1 {
		t.Fatalf("durationMilliseconds() = %d", got)
	}
	if got := durationMilliseconds(1000000 * time.Hour); got != 2147483647 {
		t.Fatalf("durationMilliseconds() cap = %d", got)
	}
}
