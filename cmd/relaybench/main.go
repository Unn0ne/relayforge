package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

var eventTypePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,99}$`)

type config struct {
	BaseURL        string
	APIKey         string
	EndpointID     string
	EventType      string
	Requests       int
	Concurrency    int
	RequestTimeout time.Duration
}

type sample struct {
	duration time.Duration
	status   int
	err      string
}

type latencyReport struct {
	MinMS float64 `json:"min_ms"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
}

type report struct {
	RunID             string         `json:"run_id"`
	Requested         int            `json:"requested"`
	Completed         int            `json:"completed"`
	Accepted          int            `json:"accepted"`
	Failed            int            `json:"failed"`
	DurationSeconds   float64        `json:"duration_seconds"`
	RequestsPerSecond float64        `json:"requests_per_second"`
	Latency           latencyReport  `json:"latency"`
	StatusCodes       map[string]int `json:"status_codes"`
	Errors            map[string]int `json:"errors,omitempty"`
}

func main() {
	cfg, err := parseConfig(os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	result, runErr := run(ctx, cfg)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		os.Exit(1)
	}
	if result.Failed > 0 {
		os.Exit(1)
	}
}

func parseConfig(args []string, getenv func(string) string) (config, error) {
	var result config
	flags := flag.NewFlagSet("relaybench", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&result.BaseURL, "base-url", "http://localhost:8080", "RelayForge base URL")
	flags.StringVar(&result.APIKey, "api-key", getenv("RELAYFORGE_API_KEY"), "RelayForge API key")
	flags.StringVar(&result.EndpointID, "endpoint-id", "", "target endpoint ID")
	flags.StringVar(&result.EventType, "event-type", "relaybench.event", "event type")
	flags.IntVar(&result.Requests, "requests", 1000, "number of events")
	flags.IntVar(&result.Concurrency, "concurrency", 20, "parallel requests")
	flags.DurationVar(&result.RequestTimeout, "request-timeout", 10*time.Second, "per-request timeout")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, errors.New("unexpected positional arguments")
	}

	parsedURL, err := url.Parse(strings.TrimSpace(result.BaseURL))
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return config{}, errors.New("base-url must be an absolute HTTP URL")
	}
	if parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return config{}, errors.New("base-url cannot contain a query or fragment")
	}
	result.BaseURL = strings.TrimRight(parsedURL.String(), "/")
	result.APIKey = strings.TrimSpace(result.APIKey)
	if result.APIKey == "" {
		return config{}, errors.New("api-key or RELAYFORGE_API_KEY is required")
	}
	if _, err = uuid.Parse(result.EndpointID); err != nil {
		return config{}, errors.New("endpoint-id must be a UUID")
	}
	result.EventType = strings.TrimSpace(result.EventType)
	if !eventTypePattern.MatchString(result.EventType) {
		return config{}, errors.New("event-type must contain 1 to 100 safe characters")
	}
	if result.Requests < 1 || result.Requests > 1_000_000 {
		return config{}, errors.New("requests must be between 1 and 1000000")
	}
	if result.Concurrency < 1 || result.Concurrency > 1000 {
		return config{}, errors.New("concurrency must be between 1 and 1000")
	}
	if result.Concurrency > result.Requests {
		result.Concurrency = result.Requests
	}
	if result.RequestTimeout <= 0 {
		return config{}, errors.New("request-timeout must be positive")
	}
	return result, nil
}

func run(ctx context.Context, cfg config) (report, error) {
	runID := uuid.NewString()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = cfg.Concurrency * 2
	transport.MaxIdleConnsPerHost = cfg.Concurrency
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer transport.CloseIdleConnections()

	jobs := make(chan int)
	results := make(chan sample, cfg.Concurrency)
	var workers sync.WaitGroup
	workers.Add(cfg.Concurrency)
	for current := 0; current < cfg.Concurrency; current++ {
		go func() {
			defer workers.Done()
			for sequence := range jobs {
				results <- publish(ctx, client, cfg, runID, sequence)
			}
		}()
	}

	startedAt := time.Now()
	go func() {
		defer close(jobs)
		for sequence := 1; sequence <= cfg.Requests; sequence++ {
			select {
			case jobs <- sequence:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	result := report{
		RunID:       runID,
		Requested:   cfg.Requests,
		StatusCodes: make(map[string]int),
		Errors:      make(map[string]int),
	}
	latencies := make([]time.Duration, 0, cfg.Requests)
	for current := range results {
		result.Completed++
		latencies = append(latencies, current.duration)
		if current.status != 0 {
			result.StatusCodes[strconv.Itoa(current.status)]++
		}
		if current.err == "" {
			result.Accepted++
		} else {
			result.Failed++
			result.Errors[current.err]++
		}
	}
	if missing := result.Requested - result.Completed; missing > 0 {
		result.Failed += missing
		result.Errors["not started"] = missing
	}

	elapsed := time.Since(startedAt)
	result.DurationSeconds = elapsed.Seconds()
	if elapsed > 0 {
		result.RequestsPerSecond = float64(result.Completed) / elapsed.Seconds()
	}
	result.Latency = summarizeLatency(latencies)
	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, ctx.Err()
}

func publish(parent context.Context, client *http.Client, cfg config, runID string, sequence int) sample {
	body, err := json.Marshal(map[string]any{
		"endpoint_id": cfg.EndpointID,
		"type":        cfg.EventType,
		"payload": map[string]any{
			"run_id":   runID,
			"sequence": sequence,
		},
	})
	if err != nil {
		return sample{err: err.Error()}
	}

	ctx, cancel := context.WithTimeout(parent, cfg.RequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/v1/events", bytes.NewReader(body))
	if err != nil {
		return sample{err: err.Error()}
	}
	request.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", runID+":"+strconv.Itoa(sequence))

	startedAt := time.Now()
	response, err := client.Do(request)
	duration := time.Since(startedAt)
	if err != nil {
		return sample{duration: duration, err: err.Error()}
	}
	defer func() {
		_ = response.Body.Close()
	}()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
	if readErr != nil {
		return sample{duration: duration, status: response.StatusCode, err: readErr.Error()}
	}
	if response.StatusCode != http.StatusAccepted {
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		return sample{
			duration: duration,
			status:   response.StatusCode,
			err:      fmt.Sprintf("http %d: %s", response.StatusCode, message),
		}
	}
	return sample{duration: duration, status: response.StatusCode}
}

func summarizeLatency(values []time.Duration) latencyReport {
	if len(values) == 0 {
		return latencyReport{}
	}
	sort.Slice(values, func(left, right int) bool {
		return values[left] < values[right]
	})
	return latencyReport{
		MinMS: milliseconds(values[0]),
		P50MS: milliseconds(percentile(values, 0.50)),
		P95MS: milliseconds(percentile(values, 0.95)),
		P99MS: milliseconds(percentile(values, 0.99)),
		MaxMS: milliseconds(values[len(values)-1]),
	}
}

func percentile(values []time.Duration, quantile float64) time.Duration {
	index := int(math.Ceil(float64(len(values))*quantile)) - 1
	return values[index]
}

func milliseconds(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}
