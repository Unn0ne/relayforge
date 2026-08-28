package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLiveness(t *testing.T) {
	handler := New(testLogger(), Dependencies{Readiness: func(context.Context) error { return nil }, APIKey: "test-api-key"}).Handler()
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestReadiness(t *testing.T) {
	tests := []struct {
		name       string
		check      func(context.Context) error
		wantStatus int
	}{
		{
			name:       "database available",
			check:      func(context.Context) error { return nil },
			wantStatus: http.StatusOK,
		},
		{
			name:       "database unavailable",
			check:      func(context.Context) error { return errors.New("database unavailable") },
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := New(testLogger(), Dependencies{Readiness: tt.check, APIKey: "test-api-key"}).Handler()
			request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
		})
	}
}

func TestMetricsArePublicAndObservedByRoute(t *testing.T) {
	observer := &httpObserverStub{}
	handler := New(testLogger(), Dependencies{
		Readiness: func(context.Context) error { return nil },
		APIKey:    "test-api-key",
		Metrics: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		HTTPObserver: observer,
	}).Handler()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if observer.method != http.MethodGet || observer.route != "GET /metrics" || observer.status != http.StatusNoContent {
		t.Fatalf("observation = %+v", observer)
	}
}

func TestUnmatchedRequestUsesBoundedMetricLabel(t *testing.T) {
	observer := &httpObserverStub{}
	handler := New(testLogger(), Dependencies{
		Readiness:    func(context.Context) error { return nil },
		APIKey:       "test-api-key",
		HTTPObserver: observer,
	}).Handler()
	request := httptest.NewRequest(http.MethodGet, "/missing/unique-id", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if observer.route != "unmatched" || observer.status != http.StatusNotFound {
		t.Fatalf("observation = %+v", observer)
	}
}

type httpObserverStub struct {
	method   string
	route    string
	status   int
	duration time.Duration
}

func (o *httpObserverStub) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	o.method = method
	o.route = route
	o.status = status
	o.duration = duration
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
