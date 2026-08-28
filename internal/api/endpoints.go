package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/endpoint"
)

const maximumEndpointBody = 64 << 10

var errUnsupportedMediaType = errors.New("Content-Type must be application/json")

type EndpointService interface {
	Create(context.Context, endpoint.CreateInput) (endpoint.Created, error)
	List(context.Context, int) ([]delivery.Endpoint, error)
	Get(context.Context, string) (delivery.Endpoint, error)
	Disable(context.Context, string) (delivery.Endpoint, error)
}

type createEndpointRequest struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	TimeoutMS   int    `json:"timeout_ms"`
	MaxAttempts int    `json:"max_attempts"`
}

type endpointResponse struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	URL                 string     `json:"url"`
	Secret              string     `json:"secret,omitempty"`
	TimeoutMS           int64      `json:"timeout_ms"`
	MaxAttempts         int        `json:"max_attempts"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	CircuitOpenUntil    *time.Time `json:"circuit_open_until,omitempty"`
	DisabledAt          *time.Time `json:"disabled_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func (s *Server) createEndpoint(w http.ResponseWriter, r *http.Request) {
	var request createEndpointRequest
	if err := decodeJSON(w, r, &request, maximumEndpointBody); err != nil {
		if errors.Is(err, errUnsupportedMediaType) {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if request.TimeoutMS < 0 || request.TimeoutMS > 30000 {
		writeError(w, http.StatusBadRequest, "invalid_request", "timeout_ms must be between 0 and 30000")
		return
	}
	if request.MaxAttempts < 0 || request.MaxAttempts > 100 {
		writeError(w, http.StatusBadRequest, "invalid_request", "max_attempts must be between 0 and 100")
		return
	}

	created, err := s.endpoints.Create(r.Context(), endpoint.CreateInput{
		Name:        request.Name,
		URL:         request.URL,
		Timeout:     time.Duration(request.TimeoutMS) * time.Millisecond,
		MaxAttempts: request.MaxAttempts,
	})
	if err != nil {
		s.writeEndpointError(w, err)
		return
	}

	response := endpointView(created.Endpoint)
	response.Secret = created.Secret
	w.Header().Set("Location", "/v1/endpoints/"+created.Endpoint.ID)
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) listEndpoints(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be an integer")
			return
		}
		limit = parsed
	}

	items, err := s.endpoints.List(r.Context(), limit)
	if err != nil {
		s.writeEndpointError(w, err)
		return
	}
	response := make([]endpointResponse, 0, len(items))
	for _, item := range items {
		response = append(response, endpointView(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": response})
}

func (s *Server) getEndpoint(w http.ResponseWriter, r *http.Request) {
	item, err := s.endpoints.Get(r.Context(), r.PathValue("endpoint_id"))
	if err != nil {
		s.writeEndpointError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, endpointView(item))
}

func (s *Server) disableEndpoint(w http.ResponseWriter, r *http.Request) {
	_, err := s.endpoints.Disable(r.Context(), r.PathValue("endpoint_id"))
	if err != nil {
		s.writeEndpointError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeEndpointError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, endpoint.ErrInvalid):
		message := strings.TrimPrefix(err.Error(), endpoint.ErrInvalid.Error()+": ")
		writeError(w, http.StatusBadRequest, "invalid_request", message)
	case errors.Is(err, endpoint.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
	default:
		s.logger.Error("endpoint request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func endpointView(value delivery.Endpoint) endpointResponse {
	return endpointResponse{
		ID:                  value.ID,
		Name:                value.Name,
		URL:                 value.URL,
		TimeoutMS:           value.Timeout.Milliseconds(),
		MaxAttempts:         value.MaxAttempts,
		ConsecutiveFailures: value.ConsecutiveFailures,
		CircuitOpenUntil:    value.CircuitOpenUntil,
		DisabledAt:          value.DisabledAt,
		CreatedAt:           value.CreatedAt,
		UpdatedAt:           value.UpdatedAt,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any, maximumBytes int64) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errUnsupportedMediaType
	}

	r.Body = http.MaxBytesReader(w, r.Body, maximumBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err = decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}
