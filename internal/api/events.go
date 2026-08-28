package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Unn0ne/relayforge/internal/endpoint"
	"github.com/Unn0ne/relayforge/internal/eventing"
)

const maximumEventBody = eventing.MaximumPayloadBytes + 4096

type EventService interface {
	Publish(context.Context, eventing.PublishInput) (eventing.Enqueued, error)
}

type publishEventRequest struct {
	EndpointID string          `json:"endpoint_id"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
}

type publishEventResponse struct {
	EventID    string `json:"event_id"`
	DeliveryID string `json:"delivery_id"`
	Status     string `json:"status"`
	Duplicate  bool   `json:"duplicate"`
}

func (s *Server) publishEvent(w http.ResponseWriter, r *http.Request) {
	var request publishEventRequest
	if err := decodeJSON(w, r, &request, maximumEventBody); err != nil {
		if errors.Is(err, errUnsupportedMediaType) {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := s.events.Publish(r.Context(), eventing.PublishInput{
		EndpointID:     request.EndpointID,
		Type:           request.Type,
		Payload:        request.Payload,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		s.writeEventError(w, err)
		return
	}

	w.Header().Set("Location", "/v1/deliveries/"+result.Delivery.ID)
	writeJSON(w, http.StatusAccepted, publishEventResponse{
		EventID:    result.Event.ID,
		DeliveryID: result.Delivery.ID,
		Status:     string(result.Delivery.Status),
		Duplicate:  result.Duplicate,
	})
}

func (s *Server) writeEventError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, eventing.ErrInvalid):
		message := strings.TrimPrefix(err.Error(), "enqueue event: ")
		message = strings.TrimPrefix(message, eventing.ErrInvalid.Error()+": ")
		writeError(w, http.StatusBadRequest, "invalid_request", message)
	case errors.Is(err, endpoint.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
	case errors.Is(err, eventing.ErrEndpointDisabled):
		writeError(w, http.StatusConflict, "endpoint_disabled", "endpoint is disabled")
	case errors.Is(err, eventing.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was already used with a different event")
	default:
		s.logger.Error("event request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
