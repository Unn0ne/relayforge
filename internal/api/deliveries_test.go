package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
)

func TestGetDelivery(t *testing.T) {
	statusCode := 503
	service := &deliveryServiceStub{details: delivery.Details{
		Delivery: delivery.Delivery{
			ID:             "734ac360-ded7-4f17-b2b6-8b74aacf9ef2",
			Status:         delivery.StatusDead,
			LastStatusCode: &statusCode,
		},
		Event:    delivery.Event{ID: "7288525c-e049-4c36-b4a7-8ee79f733b7d", Payload: json.RawMessage(`{"id":"invoice-id"}`)},
		Attempts: []delivery.Attempt{{Number: 1, StatusCode: &statusCode, Duration: 25 * time.Millisecond}},
	}}
	handler := New(testLogger(), Dependencies{Readiness: readyStub, APIKey: testAPIKey, Deliveries: service}).Handler()
	request := authenticatedRequest(http.MethodGet, "/v1/deliveries/734ac360-ded7-4f17-b2b6-8b74aacf9ef2", "")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "lease_token") || strings.Contains(response.Body.String(), "locked_by") {
		t.Fatal("lease internals leaked in response")
	}
	var body deliveryDetailsResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Delivery.Status != delivery.StatusDead || len(body.Attempts) != 1 || body.Attempts[0].DurationMS != 25 {
		t.Fatalf("body = %+v", body)
	}
}

func TestReplayDeliveryConflict(t *testing.T) {
	service := &deliveryServiceStub{replayError: delivery.ErrNotReplayable}
	handler := New(testLogger(), Dependencies{Readiness: readyStub, APIKey: testAPIKey, Deliveries: service}).Handler()
	request := authenticatedRequest(http.MethodPost, "/v1/deliveries/734ac360-ded7-4f17-b2b6-8b74aacf9ef2/replay", "")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}

type deliveryServiceStub struct {
	details     delivery.Details
	getError    error
	replayed    delivery.Delivery
	replayError error
}

func (s *deliveryServiceStub) Get(context.Context, string) (delivery.Details, error) {
	return s.details, s.getError
}

func (s *deliveryServiceStub) Replay(context.Context, string) (delivery.Delivery, error) {
	return s.replayed, s.replayError
}

var _ DeliveryService = (*deliveryServiceStub)(nil)
