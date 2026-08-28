package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
)

type DeliveryService interface {
	Get(context.Context, string) (delivery.Details, error)
	Replay(context.Context, string) (delivery.Delivery, error)
}

type deliveryResponse struct {
	ID              string          `json:"id"`
	EventID         string          `json:"event_id"`
	EndpointID      string          `json:"endpoint_id"`
	Status          delivery.Status `json:"status"`
	AttemptCount    int             `json:"attempt_count"`
	MaxAttempts     int             `json:"max_attempts"`
	NextAttemptAt   time.Time       `json:"next_attempt_at"`
	LockedUntil     *time.Time      `json:"locked_until,omitempty"`
	LastStatusCode  *int            `json:"last_status_code,omitempty"`
	LastError       string          `json:"last_error,omitempty"`
	LastCompletedAt *time.Time      `json:"last_completed_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type eventResponse struct {
	ID             string          `json:"id"`
	EndpointID     string          `json:"endpoint_id"`
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey string          `json:"idempotency_key"`
	CreatedAt      time.Time       `json:"created_at"`
}

type attemptResponse struct {
	ID           string    `json:"id"`
	Number       int       `json:"number"`
	StatusCode   *int      `json:"status_code,omitempty"`
	ResponseBody string    `json:"response_body,omitempty"`
	ErrorMessage string    `json:"error,omitempty"`
	DurationMS   int64     `json:"duration_ms"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at"`
}

type deliveryDetailsResponse struct {
	Delivery deliveryResponse  `json:"delivery"`
	Event    eventResponse     `json:"event"`
	Attempts []attemptResponse `json:"attempts"`
}

func (s *Server) getDelivery(w http.ResponseWriter, r *http.Request) {
	details, err := s.deliveries.Get(r.Context(), r.PathValue("delivery_id"))
	if err != nil {
		s.writeDeliveryError(w, err)
		return
	}

	attempts := make([]attemptResponse, 0, len(details.Attempts))
	for _, attempt := range details.Attempts {
		attempts = append(attempts, attemptResponse{
			ID:           attempt.ID,
			Number:       attempt.Number,
			StatusCode:   attempt.StatusCode,
			ResponseBody: attempt.ResponseBody,
			ErrorMessage: attempt.ErrorMessage,
			DurationMS:   attempt.Duration.Milliseconds(),
			StartedAt:    attempt.StartedAt,
			CompletedAt:  attempt.CompletedAt,
		})
	}

	writeJSON(w, http.StatusOK, deliveryDetailsResponse{
		Delivery: deliveryView(details.Delivery),
		Event: eventResponse{
			ID:             details.Event.ID,
			EndpointID:     details.Event.EndpointID,
			Type:           details.Event.Type,
			Payload:        details.Event.Payload,
			IdempotencyKey: details.Event.IdempotencyKey,
			CreatedAt:      details.Event.CreatedAt,
		},
		Attempts: attempts,
	})
}

func (s *Server) replayDelivery(w http.ResponseWriter, r *http.Request) {
	result, err := s.deliveries.Replay(r.Context(), r.PathValue("delivery_id"))
	if err != nil {
		s.writeDeliveryError(w, err)
		return
	}
	w.Header().Set("Location", "/v1/deliveries/"+result.ID)
	writeJSON(w, http.StatusAccepted, deliveryView(result))
}

func (s *Server) writeDeliveryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, delivery.ErrInvalid):
		message := strings.TrimPrefix(err.Error(), delivery.ErrInvalid.Error()+": ")
		writeError(w, http.StatusBadRequest, "invalid_request", message)
	case errors.Is(err, delivery.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "delivery not found")
	case errors.Is(err, delivery.ErrNotReplayable):
		writeError(w, http.StatusConflict, "not_replayable", "only dead deliveries can be replayed")
	case errors.Is(err, delivery.ErrEndpointDisabled):
		writeError(w, http.StatusConflict, "endpoint_disabled", "delivery endpoint is disabled")
	default:
		s.logger.Error("delivery request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func deliveryView(value delivery.Delivery) deliveryResponse {
	return deliveryResponse{
		ID:              value.ID,
		EventID:         value.EventID,
		EndpointID:      value.EndpointID,
		Status:          value.Status,
		AttemptCount:    value.AttemptCount,
		MaxAttempts:     value.MaxAttempts,
		NextAttemptAt:   value.NextAttemptAt,
		LockedUntil:     value.LockedUntil,
		LastStatusCode:  value.LastStatusCode,
		LastError:       value.LastError,
		LastCompletedAt: value.LastCompletedAt,
		CreatedAt:       value.CreatedAt,
		UpdatedAt:       value.UpdatedAt,
	}
}
