package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/eventing"
)

func TestPublishEvent(t *testing.T) {
	service := &eventServiceStub{
		result: eventing.Enqueued{
			Event: delivery.Event{ID: "7288525c-e049-4c36-b4a7-8ee79f733b7d"},
			Delivery: delivery.Delivery{
				ID:     "734ac360-ded7-4f17-b2b6-8b74aacf9ef2",
				Status: delivery.StatusPending,
			},
		},
	}
	handler := New(testLogger(), Dependencies{Readiness: readyStub, APIKey: testAPIKey, Events: service}).Handler()
	request := authenticatedRequest(http.MethodPost, "/v1/events", `{"endpoint_id":"0f4d9e5f-aac0-48d1-aa48-df706d70be39","type":"invoice.paid","payload":{"id":"invoice-id"}}`)
	request.Header.Set("Idempotency-Key", "request-id")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Location") != "/v1/deliveries/734ac360-ded7-4f17-b2b6-8b74aacf9ef2" {
		t.Fatalf("Location = %q", response.Header().Get("Location"))
	}
	var body publishEventResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.EventID != service.result.Event.ID || body.DeliveryID != service.result.Delivery.ID || body.Status != "pending" {
		t.Fatalf("body = %+v", body)
	}
	if service.input.IdempotencyKey != "request-id" {
		t.Fatalf("idempotency key = %q", service.input.IdempotencyKey)
	}
}

func TestPublishEventMapsIdempotencyConflict(t *testing.T) {
	service := &eventServiceStub{err: fmt.Errorf("enqueue event: %w", eventing.ErrIdempotencyConflict)}
	handler := New(testLogger(), Dependencies{Readiness: readyStub, APIKey: testAPIKey, Events: service}).Handler()
	request := authenticatedRequest(http.MethodPost, "/v1/events", `{"endpoint_id":"0f4d9e5f-aac0-48d1-aa48-df706d70be39","type":"invoice.paid","payload":{}}`)
	request.Header.Set("Idempotency-Key", "request-id")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}

func TestPublishEventRejectsLargeBody(t *testing.T) {
	service := &eventServiceStub{}
	handler := New(testLogger(), Dependencies{Readiness: readyStub, APIKey: testAPIKey, Events: service}).Handler()
	request := authenticatedRequest(http.MethodPost, "/v1/events", `{"endpoint_id":"0f4d9e5f-aac0-48d1-aa48-df706d70be39","type":"invoice.paid","payload":"`+string(make([]byte, maximumEventBody))+`"}`)
	request.Header.Set("Idempotency-Key", "request-id")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
	if service.called {
		t.Fatal("service was called for oversized request")
	}
}

type eventServiceStub struct {
	input  eventing.PublishInput
	result eventing.Enqueued
	err    error
	called bool
}

func (s *eventServiceStub) Publish(_ context.Context, input eventing.PublishInput) (eventing.Enqueued, error) {
	s.called = true
	s.input = input
	return s.result, s.err
}

var _ EventService = (*eventServiceStub)(nil)
