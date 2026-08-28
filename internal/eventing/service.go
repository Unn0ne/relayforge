package eventing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/google/uuid"
)

const MaximumPayloadBytes = 1 << 20

var (
	ErrInvalid             = errors.New("invalid event")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	ErrEndpointDisabled    = errors.New("endpoint disabled")
	eventTypePattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,99}$`)
)

type EnqueueParams struct {
	EventID        string
	DeliveryID     string
	EndpointID     string
	Type           string
	Payload        json.RawMessage
	IdempotencyKey string
}

type Enqueued struct {
	Event     delivery.Event
	Delivery  delivery.Delivery
	Duplicate bool
}

type Repository interface {
	EnqueueEvent(context.Context, EnqueueParams) (Enqueued, error)
}

type Service struct {
	repository Repository
}

type PublishInput struct {
	EndpointID     string
	Type           string
	Payload        json.RawMessage
	IdempotencyKey string
}

func New(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Publish(ctx context.Context, input PublishInput) (Enqueued, error) {
	if _, err := uuid.Parse(input.EndpointID); err != nil {
		return Enqueued{}, fmt.Errorf("%w: malformed endpoint id", ErrInvalid)
	}

	eventType := strings.TrimSpace(input.Type)
	if !eventTypePattern.MatchString(eventType) {
		return Enqueued{}, fmt.Errorf("%w: event type must contain 1 to 100 safe characters", ErrInvalid)
	}

	if len(input.Payload) == 0 || len(input.Payload) > MaximumPayloadBytes || !json.Valid(input.Payload) {
		return Enqueued{}, fmt.Errorf("%w: payload must be valid JSON up to %d bytes", ErrInvalid, MaximumPayloadBytes)
	}

	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if !validIdempotencyKey(idempotencyKey) {
		return Enqueued{}, fmt.Errorf("%w: idempotency key must contain 1 to 200 printable characters", ErrInvalid)
	}

	result, err := s.repository.EnqueueEvent(ctx, EnqueueParams{
		EventID:        uuid.NewString(),
		DeliveryID:     uuid.NewString(),
		EndpointID:     input.EndpointID,
		Type:           eventType,
		Payload:        append(json.RawMessage(nil), input.Payload...),
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return Enqueued{}, fmt.Errorf("enqueue event: %w", err)
	}
	return result, nil
}

func validIdempotencyKey(value string) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 200 {
		return false
	}
	for _, current := range value {
		if current < 32 || current == 127 {
			return false
		}
	}
	return true
}
