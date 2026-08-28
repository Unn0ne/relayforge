package observability

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMetricsExposeRuntimeAndOperationalState(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://localhost:1/relayforge")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	metrics := NewMetrics(statsStub{stats: store.OperationalStats{
		Pending:      2,
		Processing:   3,
		Retrying:     4,
		Succeeded:    5,
		Dead:         6,
		OpenCircuits: 1,
	}}, pool)
	metrics.ObserveHTTPRequest("GET", "GET /health/live", 200, 15*time.Millisecond)
	metrics.ObserveClaim("claimed")
	metrics.ObserveAttempt(delivery.DecisionSucceed, 10*time.Millisecond)
	metrics.ObserveFinishError()
	metrics.AddInFlight(1)

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	body, err := io.ReadAll(response.Result().Body)
	if err != nil {
		t.Fatal(err)
	}

	for _, expected := range []string{
		`relayforge_http_requests_total{method="GET",route="GET /health/live",status="200"} 1`,
		`relayforge_worker_claims_total{result="claimed"} 1`,
		`relayforge_delivery_attempts_total{decision="succeed"} 1`,
		`relayforge_delivery_finish_errors_total 1`,
		`relayforge_delivery_in_flight 1`,
		`relayforge_deliveries{status="pending"} 2`,
		`relayforge_deliveries{status="dead"} 6`,
		`relayforge_open_circuits 1`,
		`relayforge_operational_stats_scrape_success 1`,
		`relayforge_database_connections{state="total"} 0`,
	} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("metric %q is missing", expected)
		}
	}
}

func TestMetricsReportOperationalQueryFailure(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://localhost:1/relayforge")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	metrics := NewMetrics(statsStub{err: errors.New("database unavailable")}, pool)
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))

	if !strings.Contains(response.Body.String(), "relayforge_operational_stats_scrape_success 0") {
		t.Fatalf("body = %s", response.Body.String())
	}
}

type statsStub struct {
	stats store.OperationalStats
	err   error
}

func (s statsStub) GetOperationalStats(context.Context) (store.OperationalStats, error) {
	return s.stats, s.err
}
