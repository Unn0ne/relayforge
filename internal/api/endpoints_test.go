package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/endpoint"
)

const testAPIKey = "01234567890123456789012345678901"

func TestEndpointAPIRequiresAuthentication(t *testing.T) {
	service := &endpointServiceStub{}
	handler := New(testLogger(), readyStub, testAPIKey, service).Handler()
	request := httptest.NewRequest(http.MethodGet, "/v1/endpoints", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
	if response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q", response.Header().Get("WWW-Authenticate"))
	}
	if service.listCalled {
		t.Fatal("service was called without authentication")
	}
}

func TestCreateEndpoint(t *testing.T) {
	now := time.Now().UTC()
	service := &endpointServiceStub{
		created: endpoint.Created{
			Endpoint: delivery.Endpoint{
				ID:               "0f4d9e5f-aac0-48d1-aa48-df706d70be39",
				Name:             "billing",
				URL:              "https://example.com/hooks",
				SecretCiphertext: []byte("must-not-leak"),
				Timeout:          5 * time.Second,
				MaxAttempts:      8,
				CreatedAt:        now,
				UpdatedAt:        now,
			},
			Secret: "returned-once",
		},
	}
	handler := New(testLogger(), readyStub, testAPIKey, service).Handler()
	request := authenticatedRequest(http.MethodPost, "/v1/endpoints", `{"name":"billing","url":"https://example.com/hooks"}`)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Location") != "/v1/endpoints/0f4d9e5f-aac0-48d1-aa48-df706d70be39" {
		t.Fatalf("Location = %q", response.Header().Get("Location"))
	}
	if strings.Contains(response.Body.String(), "must-not-leak") {
		t.Fatal("ciphertext leaked in response")
	}
	var body endpointResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Secret != "returned-once" || body.MaxAttempts != 8 || body.TimeoutMS != 5000 {
		t.Fatalf("body = %+v", body)
	}
}

func TestCreateEndpointRejectsUnknownField(t *testing.T) {
	service := &endpointServiceStub{}
	handler := New(testLogger(), readyStub, testAPIKey, service).Handler()
	request := authenticatedRequest(http.MethodPost, "/v1/endpoints", `{"name":"billing","url":"https://example.com","unknown":true}`)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
	if service.createCalled {
		t.Fatal("service was called with invalid body")
	}
}

func TestCreateEndpointRequiresJSON(t *testing.T) {
	service := &endpointServiceStub{}
	handler := New(testLogger(), readyStub, testAPIKey, service).Handler()
	request := httptest.NewRequest(http.MethodPost, "/v1/endpoints", bytes.NewBufferString("name=billing"))
	request.Header.Set("Authorization", "Bearer "+testAPIKey)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestGetEndpointNotFound(t *testing.T) {
	service := &endpointServiceStub{getError: endpoint.ErrNotFound}
	handler := New(testLogger(), readyStub, testAPIKey, service).Handler()
	request := authenticatedRequest(http.MethodGet, "/v1/endpoints/0f4d9e5f-aac0-48d1-aa48-df706d70be39", "")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}

func readyStub(context.Context) error {
	return nil
}

func authenticatedRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testAPIKey)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

type endpointServiceStub struct {
	created      endpoint.Created
	createError  error
	items        []delivery.Endpoint
	listError    error
	getItem      delivery.Endpoint
	getError     error
	disableError error
	createCalled bool
	listCalled   bool
}

func (s *endpointServiceStub) Create(context.Context, endpoint.CreateInput) (endpoint.Created, error) {
	s.createCalled = true
	return s.created, s.createError
}

func (s *endpointServiceStub) List(context.Context, int) ([]delivery.Endpoint, error) {
	s.listCalled = true
	return s.items, s.listError
}

func (s *endpointServiceStub) Get(context.Context, string) (delivery.Endpoint, error) {
	return s.getItem, s.getError
}

func (s *endpointServiceStub) Disable(context.Context, string) (delivery.Endpoint, error) {
	return delivery.Endpoint{}, s.disableError
}

var _ EndpointService = (*endpointServiceStub)(nil)
