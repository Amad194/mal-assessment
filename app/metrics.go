package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Application metrics. client_golang also exports Go runtime and process
// metrics for free, which feed the SLO burn-rate and saturation dashboards.
var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests by route, method and status code.",
	}, []string{"route", "method", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds by route and method.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"route", "method"})

	dbUp = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "accounts_db_up",
		Help: "1 if the most recent readiness DB ping succeeded, else 0.",
	})

	auditPublishFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "accounts_audit_publish_failures_total",
		Help: "Count of audit events that failed to publish to Kafka.",
	})
)

func metricsHandler() http.Handler { return promhttp.Handler() }

// instrument wraps a handler with latency + request-count instrumentation,
// labelled by a stable route name (not the raw path, to avoid cardinality
// explosions from the {id} segment).
func instrument(route string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h(rec, r)
		httpDuration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
		httpRequests.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Inc()
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
