package metrics

import (
	"akswitch/internal/keypool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metric handles for AK Switch.
type Metrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	KeyPoolKeys     *prometheus.GaugeVec
	UpstreamErrors  *prometheus.CounterVec

	UpstreamCBState     *prometheus.GaugeVec   // akswitch_upstream_cb_state, labels: {"provider"} (0=CLOSED, 1=OPEN, 2=HALF_OPEN)
	HealthCheckProbes   *prometheus.CounterVec // akswitch_healthcheck_probes_total, labels: {"provider","status":"ok"|"fail"}
	HealthCheckDuration *prometheus.HistogramVec // akswitch_healthcheck_duration_seconds, labels: {"provider"}
}

// NewRegistry creates a non-global Prometheus registry and registers all application metrics.
// Returns the registry, the Metrics handles, and a factory for auto-registration.
func NewRegistry() (*prometheus.Registry, *Metrics) {
	reg := prometheus.NewRegistry()

	factory := promauto.With(reg)

	m := &Metrics{
		RequestsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "akswitch_requests_total",
				Help: "Total number of proxy requests by method, status class, and key index.",
			},
			[]string{"method", "status", "key_index"},
		),
		RequestDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "akswitch_request_duration_seconds",
				Help:    "Request latency distribution by method and status class.",
				Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 15, 30, 60, 120},
			},
			[]string{"method", "status"},
		),
		KeyPoolKeys: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "akswitch_keypool_keys",
				Help: "Current number of keys by provider and state (active, cooling, disabled).",
			},
			[]string{"provider", "state"},
		),
		UpstreamErrors: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "akswitch_upstream_errors_total",
				Help: "Count of upstream errors by type (network, rate_limited, auth_rejected, server_error).",
			},
			[]string{"type"},
		),
		UpstreamCBState: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "akswitch",
				Name:      "upstream_cb_state",
				Help:      "Upstream circuit breaker state per provider: 0=CLOSED, 1=OPEN, 2=HALF_OPEN",
			},
			[]string{"provider"},
		),
		HealthCheckProbes: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "akswitch",
				Name:      "healthcheck_probes_total",
				Help:      "Count of health check probes by provider and status",
			},
			[]string{"provider", "status"},
		),
		HealthCheckDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "akswitch",
				Name:      "healthcheck_duration_seconds",
				Help:      "Duration of health check probes per provider",
				Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 15, 30, 60, 120},
			},
			[]string{"provider"},
		),
	}

	// Register Go runtime metrics (go_*, process_*) as well
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	reg.MustRegister(prometheus.NewGoCollector())

	return reg, m
}

// RefreshKeyPoolGauge updates the KeyPoolKeys gauge from the pool's current state.
// Call this periodically (e.g. every 10 seconds).
func (m *Metrics) RefreshKeyPoolGauge(pool *keypool.KeyPool, providerName string) {
	m.KeyPoolKeys.WithLabelValues(providerName, "active").Set(float64(pool.ActiveCount()))
	m.KeyPoolKeys.WithLabelValues(providerName, "cooling").Set(float64(pool.CoolingCount()))
	m.KeyPoolKeys.WithLabelValues(providerName, "disabled").Set(float64(pool.DisabledCount()))
}

// StatusLabel converts an HTTP status code to a Prometheus-compatible status class label.
func StatusLabel(code int) string {
	switch {
	case code < 200:
		return "1xx"
	case code < 300:
		return "2xx"
	case code < 400:
		return "3xx"
	case code < 500:
		return "4xx"
	default:
		return "5xx"
	}
}
