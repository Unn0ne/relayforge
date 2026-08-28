package delivery

import (
	"errors"
	"testing"
	"time"
)

func TestRetryPolicyNextDelay(t *testing.T) {
	policy, err := NewRetryPolicy(time.Second, 8*time.Second, 0.2)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		attempt int
		sample  float64
		want    time.Duration
	}{
		{name: "first lower bound", attempt: 1, sample: 0, want: 800 * time.Millisecond},
		{name: "second midpoint", attempt: 2, sample: 0.5, want: 2 * time.Second},
		{name: "third upper bound", attempt: 3, sample: 1, want: 4800 * time.Millisecond},
		{name: "delay is capped", attempt: 10, sample: 0.5, want: 8 * time.Second},
		{name: "jitter respects cap", attempt: 10, sample: 1, want: 8 * time.Second},
		{name: "sample is clamped", attempt: 1, sample: 2, want: 1200 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := policy.NextDelay(tt.attempt, tt.sample); got != tt.want {
				t.Fatalf("NextDelay() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNewRetryPolicyValidation(t *testing.T) {
	tests := []struct {
		name   string
		base   time.Duration
		max    time.Duration
		jitter float64
	}{
		{name: "zero base", base: 0, max: time.Second, jitter: 0.1},
		{name: "max below base", base: time.Second, max: time.Millisecond, jitter: 0.1},
		{name: "negative jitter", base: time.Second, max: time.Second, jitter: -0.1},
		{name: "excessive jitter", base: time.Second, max: time.Second, jitter: 1.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewRetryPolicy(tt.base, tt.max, tt.jitter); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		err        error
		want       Decision
	}{
		{name: "network error", err: errors.New("connection reset"), want: DecisionRetry},
		{name: "accepted", statusCode: 202, want: DecisionSucceed},
		{name: "timeout", statusCode: 408, want: DecisionRetry},
		{name: "too early", statusCode: 425, want: DecisionRetry},
		{name: "rate limited", statusCode: 429, want: DecisionRetry},
		{name: "server error", statusCode: 503, want: DecisionRetry},
		{name: "bad request", statusCode: 400, want: DecisionDiscard},
		{name: "redirect", statusCode: 307, want: DecisionDiscard},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Evaluate(tt.statusCode, tt.err); got != tt.want {
				t.Fatalf("Evaluate() = %s, want %s", got, tt.want)
			}
		})
	}
}
