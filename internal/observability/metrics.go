package observability

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type StatsProvider interface {
	GetOperationalStats(context.Context) (store.OperationalStats, error)
}

type Metrics struct {
	registry             *prometheus.Registry
	stats                StatsProvider
	pool                 *pgxpool.Pool
	httpRequests         *prometheus.CounterVec
	httpDuration         *prometheus.HistogramVec
	workerClaims         *prometheus.CounterVec
	deliveryAttempts     *prometheus.CounterVec
	deliveryDuration     *prometheus.HistogramVec
	deliveryFinishErrors prometheus.Counter
	deliveryInFlight     prometheus.Gauge
	deliveries           *prometheus.Desc
	openCircuits         *prometheus.Desc
	statsScrapeSuccess   *prometheus.Desc
	databaseConnections  *prometheus.Desc
}

func NewMetrics(stats StatsProvider, pool *pgxpool.Pool) *Metrics {
	result := &Metrics{
		registry: prometheus.NewRegistry(),
		stats:    stats,
		pool:     pool,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "relayforge",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total HTTP requests.",
		}, []string{"method", "route", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "relayforge",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "route"}),
		workerClaims: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "relayforge",
			Subsystem: "worker",
			Name:      "claims_total",
			Help:      "Delivery claim attempts by result.",
		}, []string{"result"}),
		deliveryAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "relayforge",
			Subsystem: "delivery",
			Name:      "attempts_total",
			Help:      "Webhook delivery attempts by decision.",
		}, []string{"decision"}),
		deliveryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "relayforge",
			Subsystem: "delivery",
			Name:      "attempt_duration_seconds",
			Help:      "Webhook delivery attempt duration in seconds.",
			Buckets:   []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"decision"}),
		deliveryFinishErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "relayforge",
			Subsystem: "delivery",
			Name:      "finish_errors_total",
			Help:      "Failed delivery result persistence operations.",
		}),
		deliveryInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "relayforge",
			Subsystem: "delivery",
			Name:      "in_flight",
			Help:      "Deliveries currently processed by this instance.",
		}),
		deliveries: prometheus.NewDesc(
			"relayforge_deliveries",
			"Current durable deliveries by status.",
			[]string{"status"},
			nil,
		),
		openCircuits: prometheus.NewDesc(
			"relayforge_open_circuits",
			"Endpoints with an open circuit.",
			nil,
			nil,
		),
		statsScrapeSuccess: prometheus.NewDesc(
			"relayforge_operational_stats_scrape_success",
			"Whether durable operational stats were collected successfully.",
			nil,
			nil,
		),
		databaseConnections: prometheus.NewDesc(
			"relayforge_database_connections",
			"PostgreSQL pool connections by state.",
			[]string{"state"},
			nil,
		),
	}
	result.registry.MustRegister(
		result.httpRequests,
		result.httpDuration,
		result.workerClaims,
		result.deliveryAttempts,
		result.deliveryDuration,
		result.deliveryFinishErrors,
		result.deliveryInFlight,
		result,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewBuildInfoCollector(),
	)
	return result
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})
}

func (m *Metrics) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	m.httpRequests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.httpDuration.WithLabelValues(method, route).Observe(duration.Seconds())
}

func (m *Metrics) ObserveClaim(result string) {
	m.workerClaims.WithLabelValues(result).Inc()
}

func (m *Metrics) ObserveAttempt(decision delivery.Decision, duration time.Duration) {
	label := string(decision)
	m.deliveryAttempts.WithLabelValues(label).Inc()
	m.deliveryDuration.WithLabelValues(label).Observe(duration.Seconds())
}

func (m *Metrics) ObserveFinishError() {
	m.deliveryFinishErrors.Inc()
}

func (m *Metrics) AddInFlight(delta float64) {
	m.deliveryInFlight.Add(delta)
}

func (m *Metrics) Describe(output chan<- *prometheus.Desc) {
	output <- m.deliveries
	output <- m.openCircuits
	output <- m.statsScrapeSuccess
	output <- m.databaseConnections
}

func (m *Metrics) Collect(output chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	stats, err := m.stats.GetOperationalStats(ctx)
	cancel()
	if err != nil {
		output <- prometheus.MustNewConstMetric(m.statsScrapeSuccess, prometheus.GaugeValue, 0)
	} else {
		output <- prometheus.MustNewConstMetric(m.statsScrapeSuccess, prometheus.GaugeValue, 1)
		for status, count := range map[string]int64{
			"pending":    stats.Pending,
			"processing": stats.Processing,
			"retrying":   stats.Retrying,
			"succeeded":  stats.Succeeded,
			"dead":       stats.Dead,
		} {
			output <- prometheus.MustNewConstMetric(m.deliveries, prometheus.GaugeValue, float64(count), status)
		}
		output <- prometheus.MustNewConstMetric(m.openCircuits, prometheus.GaugeValue, float64(stats.OpenCircuits))
	}

	poolStats := m.pool.Stat()
	for state, count := range map[string]int32{
		"acquired": poolStats.AcquiredConns(),
		"idle":     poolStats.IdleConns(),
		"total":    poolStats.TotalConns(),
		"max":      poolStats.MaxConns(),
	} {
		output <- prometheus.MustNewConstMetric(m.databaseConnections, prometheus.GaugeValue, float64(count), state)
	}
}
