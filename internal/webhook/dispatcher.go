package webhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/endpoint"
)

const (
	defaultResponseLimit = 32 << 10
	maximumErrorLength   = 2048
)

type Cipher interface {
	Decrypt([]byte, []byte) ([]byte, error)
}

type CircuitSignal string

const (
	CircuitNone    CircuitSignal = "none"
	CircuitFailure CircuitSignal = "failure"
	CircuitSuccess CircuitSignal = "success"
)

type Request struct {
	DeliveryID       string
	EventID          string
	EndpointID       string
	EventType        string
	TargetURL        string
	SecretCiphertext []byte
	Payload          []byte
	Timeout          time.Duration
}

type Result struct {
	Decision      delivery.Decision
	StatusCode    *int
	ResponseBody  string
	ErrorMessage  string
	CircuitSignal CircuitSignal
	Duration      time.Duration
	StartedAt     time.Time
	CompletedAt   time.Time
}

type Options struct {
	AllowHTTP           bool
	AllowPrivateTargets bool
	Resolver            Resolver
	Dialer              ContextDialer
	Now                 func() time.Time
	ResponseLimit       int64
}

type Dispatcher struct {
	cipher        Cipher
	client        *http.Client
	now           func() time.Time
	allowHTTP     bool
	responseLimit int64
	transport     *http.Transport
}

func NewDispatcher(cipher Cipher, options Options) *Dispatcher {
	resolver := options.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := options.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	responseLimit := options.ResponseLimit
	if responseLimit < 1 {
		responseLimit = defaultResponseLimit
	}

	safeDialer := NewSafeDialer(resolver, dialer, options.AllowPrivateTargets)
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            safeDialer.DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           100,
		MaxIdleConnsPerHost:    10,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    10 * time.Second,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: 64 << 10,
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &Dispatcher{
		cipher:        cipher,
		client:        client,
		now:           now,
		allowHTTP:     options.AllowHTTP,
		responseLimit: responseLimit,
		transport:     transport,
	}
}

func (d *Dispatcher) Deliver(ctx context.Context, input Request) Result {
	startedAt := d.now().UTC()
	secret, err := d.cipher.Decrypt(input.SecretCiphertext, endpoint.SecretContext(input.EndpointID))
	if err != nil {
		return d.failed(startedAt, delivery.DecisionDiscard, CircuitNone, fmt.Errorf("decrypt endpoint secret: %w", err))
	}
	defer clear(secret)

	if input.Timeout <= 0 {
		return d.failed(startedAt, delivery.DecisionDiscard, CircuitNone, errors.New("endpoint timeout must be positive"))
	}
	requestCtx, cancel := context.WithTimeout(ctx, input.Timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, input.TargetURL, bytes.NewReader(input.Payload))
	if err != nil {
		return d.failed(startedAt, delivery.DecisionDiscard, CircuitNone, fmt.Errorf("build webhook request: %w", err))
	}
	if request.URL.Scheme != "https" && (!d.allowHTTP || request.URL.Scheme != "http") {
		return d.failed(startedAt, delivery.DecisionDiscard, CircuitNone, errors.New("webhook target must use HTTPS"))
	}

	timestamp := startedAt.Unix()
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "RelayForge/1.0")
	request.Header.Set("X-RelayForge-Delivery", input.DeliveryID)
	request.Header.Set("X-RelayForge-Event-ID", input.EventID)
	request.Header.Set("X-RelayForge-Event", input.EventType)
	request.Header.Set("X-RelayForge-Timestamp", fmt.Sprintf("%d", timestamp))
	request.Header.Set("X-RelayForge-Signature", Sign(secret, timestamp, input.DeliveryID, input.Payload))

	response, err := d.client.Do(request)
	if err != nil {
		return d.failed(startedAt, delivery.DecisionRetry, CircuitFailure, fmt.Errorf("send webhook: %w", err))
	}
	defer func() {
		_ = response.Body.Close()
	}()

	body, readErr := io.ReadAll(io.LimitReader(response.Body, d.responseLimit+1))
	if int64(len(body)) > d.responseLimit {
		body = body[:d.responseLimit]
	}
	completedAt := d.now().UTC()
	statusCode := response.StatusCode
	result := Result{
		Decision:      delivery.Evaluate(statusCode, nil),
		StatusCode:    &statusCode,
		ResponseBody:  validText(body),
		CircuitSignal: circuitSignal(statusCode),
		Duration:      completedAt.Sub(startedAt),
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
	}
	if readErr != nil {
		result.ErrorMessage = errorText(fmt.Errorf("read webhook response: %w", readErr))
	}
	return result
}

func (d *Dispatcher) Close() {
	d.transport.CloseIdleConnections()
}

func (d *Dispatcher) failed(startedAt time.Time, decision delivery.Decision, signal CircuitSignal, err error) Result {
	completedAt := d.now().UTC()
	return Result{
		Decision:      decision,
		ErrorMessage:  errorText(err),
		CircuitSignal: signal,
		Duration:      completedAt.Sub(startedAt),
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
	}
}

func circuitSignal(status int) CircuitSignal {
	if status >= 500 || status == http.StatusRequestTimeout {
		return CircuitFailure
	}
	if status >= 100 {
		return CircuitSuccess
	}
	return CircuitNone
}

func validText(value []byte) string {
	if utf8.Valid(value) {
		return string(value)
	}
	return strings.ToValidUTF8(string(value), "�")
}

func errorText(err error) string {
	result := strings.ToValidUTF8(err.Error(), "�")
	if len(result) > maximumErrorLength {
		result = result[:maximumErrorLength]
	}
	return result
}
