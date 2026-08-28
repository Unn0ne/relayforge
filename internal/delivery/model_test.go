package delivery

import "testing"

func TestStatusTransitions(t *testing.T) {
	tests := []struct {
		name string
		from Status
		to   Status
		want bool
	}{
		{name: "pending is claimed", from: StatusPending, to: StatusProcessing, want: true},
		{name: "retry is claimed", from: StatusRetrying, to: StatusProcessing, want: true},
		{name: "processing succeeds", from: StatusProcessing, to: StatusSucceeded, want: true},
		{name: "processing retries", from: StatusProcessing, to: StatusRetrying, want: true},
		{name: "processing dies", from: StatusProcessing, to: StatusDead, want: true},
		{name: "dead is replayed", from: StatusDead, to: StatusPending, want: true},
		{name: "success is final", from: StatusSucceeded, to: StatusPending, want: false},
		{name: "pending cannot succeed directly", from: StatusPending, to: StatusSucceeded, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.from.CanTransition(tt.to); got != tt.want {
				t.Fatalf("CanTransition() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatusFinal(t *testing.T) {
	if !StatusSucceeded.Final() {
		t.Fatal("succeeded status must be final")
	}
	if !StatusDead.Final() {
		t.Fatal("dead status must be final")
	}
	if StatusRetrying.Final() {
		t.Fatal("retrying status must not be final")
	}
}
