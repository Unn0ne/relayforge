package delivery

import (
	"encoding/json"
	"time"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusRetrying   Status = "retrying"
	StatusSucceeded  Status = "succeeded"
	StatusDead       Status = "dead"
)

type Endpoint struct {
	ID                  string
	Name                string
	URL                 string
	SecretCiphertext    []byte
	Timeout             time.Duration
	MaxAttempts         int
	ConsecutiveFailures int
	DisabledAt          *time.Time
	CircuitOpenUntil    *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type Event struct {
	ID             string
	EndpointID     string
	Type           string
	Payload        json.RawMessage
	IdempotencyKey string
	CreatedAt      time.Time
}

type Delivery struct {
	ID              string
	EventID         string
	EndpointID      string
	Status          Status
	AttemptCount    int
	MaxAttempts     int
	NextAttemptAt   time.Time
	LockedBy        string
	LeaseToken      string
	LockedAt        *time.Time
	LockedUntil     *time.Time
	LastStatusCode  int
	LastError       string
	LastCompletedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Attempt struct {
	ID           string
	DeliveryID   string
	Number       int
	StatusCode   int
	ResponseBody string
	Error        string
	Duration     time.Duration
	StartedAt    time.Time
	CompletedAt  time.Time
}

func (s Status) Final() bool {
	return s == StatusSucceeded || s == StatusDead
}

func (s Status) CanTransition(next Status) bool {
	switch s {
	case StatusPending, StatusRetrying:
		return next == StatusProcessing
	case StatusProcessing:
		return next == StatusSucceeded || next == StatusRetrying || next == StatusDead
	case StatusDead:
		return next == StatusPending
	default:
		return false
	}
}
