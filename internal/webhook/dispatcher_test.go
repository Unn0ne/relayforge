package webhook

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/endpoint"
	"github.com/Unn0ne/relayforge/internal/secure"
)

func TestDispatcherDeliversSignedRequest(t *testing.T) {
	secret := "signing-secret"
	box, ciphertext := encryptedSecret(t, secret, "endpoint-id")
	times := []time.Time{
		time.Unix(1700000000, 0),
		time.Unix(1700000001, 0),
	}
	currentTime := 0
	payload := []byte(`{"id":"invoice-id"}`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if !bytes.Equal(body, payload) {
			t.Errorf("body = %q", body)
		}
		if r.Header.Get("X-RelayForge-Delivery") != "delivery-id" {
			t.Errorf("delivery header = %q", r.Header.Get("X-RelayForge-Delivery"))
		}
		if r.Header.Get("X-RelayForge-Signature") != Sign([]byte(secret), 1700000000, "delivery-id", payload) {
			t.Errorf("signature = %q", r.Header.Get("X-RelayForge-Signature"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	dispatcher := NewDispatcher(box, Options{
		AllowHTTP:           true,
		AllowPrivateTargets: true,
		Now: func() time.Time {
			result := times[currentTime]
			currentTime++
			return result
		},
	})
	defer dispatcher.Close()

	result := dispatcher.Deliver(context.Background(), Request{
		DeliveryID:       "delivery-id",
		EventID:          "event-id",
		EndpointID:       "endpoint-id",
		EventType:        "invoice.paid",
		TargetURL:        server.URL,
		SecretCiphertext: ciphertext,
		Payload:          payload,
		Timeout:          time.Second,
	})
	if result.Decision != delivery.DecisionSucceed || result.StatusCode == nil || *result.StatusCode != http.StatusNoContent {
		t.Fatalf("result = %+v", result)
	}
	if result.CircuitSignal != CircuitSuccess || result.Duration != time.Second {
		t.Fatalf("result = %+v", result)
	}
}

func TestDispatcherDoesNotFollowRedirects(t *testing.T) {
	box, ciphertext := encryptedSecret(t, "secret", "endpoint-id")
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Store(true)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	dispatcher := NewDispatcher(box, Options{AllowHTTP: true, AllowPrivateTargets: true})
	defer dispatcher.Close()
	result := dispatcher.Deliver(context.Background(), Request{
		DeliveryID:       "delivery-id",
		EndpointID:       "endpoint-id",
		TargetURL:        source.URL,
		SecretCiphertext: ciphertext,
		Payload:          []byte(`{}`),
		Timeout:          time.Second,
	})

	if result.Decision != delivery.DecisionDiscard || result.StatusCode == nil || *result.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("result = %+v", result)
	}
	if redirected.Load() {
		t.Fatal("redirect target was called")
	}
}

func TestDispatcherBlocksPrivateTarget(t *testing.T) {
	box, ciphertext := encryptedSecret(t, "secret", "endpoint-id")
	dispatcher := NewDispatcher(box, Options{AllowHTTP: true})
	defer dispatcher.Close()

	result := dispatcher.Deliver(context.Background(), Request{
		DeliveryID:       "delivery-id",
		EndpointID:       "endpoint-id",
		TargetURL:        "http://127.0.0.1:8080/hook",
		SecretCiphertext: ciphertext,
		Payload:          []byte(`{}`),
		Timeout:          time.Second,
	})
	if result.Decision != delivery.DecisionRetry || result.CircuitSignal != CircuitFailure {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.ErrorMessage, ErrBlockedAddress.Error()) {
		t.Fatalf("error = %q", result.ErrorMessage)
	}
}

func TestDispatcherLimitsResponseBody(t *testing.T) {
	box, ciphertext := encryptedSecret(t, "secret", "endpoint-id")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 100))
	}))
	defer server.Close()

	dispatcher := NewDispatcher(box, Options{AllowHTTP: true, AllowPrivateTargets: true, ResponseLimit: 10})
	defer dispatcher.Close()
	result := dispatcher.Deliver(context.Background(), Request{
		DeliveryID:       "delivery-id",
		EndpointID:       "endpoint-id",
		TargetURL:        server.URL,
		SecretCiphertext: ciphertext,
		Payload:          []byte(`{}`),
		Timeout:          time.Second,
	})
	if result.ResponseBody != "xxxxxxxxxx" {
		t.Fatalf("response body = %q", result.ResponseBody)
	}
}

func encryptedSecret(t *testing.T, secret, endpointID string) (*secure.Box, []byte) {
	t.Helper()
	box, err := secure.NewBox(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := box.Encrypt([]byte(secret), endpoint.SecretContext(endpointID))
	if err != nil {
		t.Fatal(err)
	}
	return box, ciphertext
}
