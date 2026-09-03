package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRunPublishesUniqueEvents(t *testing.T) {
	const requestCount = 40
	var lock sync.Mutex
	keys := make(map[string]struct{}, requestCount)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer api-key" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		lock.Lock()
		keys[r.Header.Get("Idempotency-Key")] = struct{}{}
		lock.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	result, err := run(context.Background(), config{
		BaseURL:        server.URL,
		APIKey:         "api-key",
		EndpointID:     uuid.NewString(),
		EventType:      "bench.event",
		Requests:       requestCount,
		Concurrency:    8,
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != requestCount || result.Failed != 0 || result.Completed != requestCount {
		t.Fatalf("result = %+v", result)
	}
	if result.StatusCodes["202"] != requestCount || len(keys) != requestCount {
		t.Fatalf("statuses=%v unique_keys=%d", result.StatusCodes, len(keys))
	}
	if result.RequestsPerSecond <= 0 || result.Latency.MaxMS < result.Latency.MinMS {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunReportsRejectedEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer server.Close()

	result, err := run(context.Background(), config{
		BaseURL:        server.URL,
		APIKey:         "api-key",
		EndpointID:     uuid.NewString(),
		EventType:      "bench.event",
		Requests:       3,
		Concurrency:    2,
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 0 || result.Failed != 3 || result.StatusCodes["429"] != 3 {
		t.Fatalf("result = %+v", result)
	}
}

func TestParseConfigUsesEnvironmentKey(t *testing.T) {
	endpointID := uuid.NewString()
	result, err := parseConfig([]string{
		"-base-url", "https://relayforge.test/",
		"-endpoint-id", endpointID,
		"-requests", "4",
		"-concurrency", "10",
	}, func(name string) string {
		if name == "RELAYFORGE_API_KEY" {
			return "api-key"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.BaseURL != "https://relayforge.test" || result.APIKey != "api-key" || result.Concurrency != 4 {
		t.Fatalf("config = %+v", result)
	}
}

func TestParseConfigRejectsInvalidConcurrency(t *testing.T) {
	_, err := parseConfig([]string{
		"-endpoint-id", uuid.NewString(),
		"-api-key", "api-key",
		"-concurrency", "0",
	}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestParseConfigRejectsInvalidEventType(t *testing.T) {
	_, err := parseConfig([]string{
		"-endpoint-id", uuid.NewString(),
		"-api-key", "api-key",
		"-event-type", "invoice paid",
	}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSummarizeLatency(t *testing.T) {
	result := summarizeLatency([]time.Duration{
		100 * time.Millisecond,
		10 * time.Millisecond,
		50 * time.Millisecond,
		20 * time.Millisecond,
	})
	if result.MinMS != 10 || result.P50MS != 20 || result.P95MS != 100 || result.MaxMS != 100 {
		t.Fatalf("latency = %+v", result)
	}
}
