// Package metrics defines the Prometheus collectors exposed by cyberkube on
// /metrics. All collectors are registered once at package init against the
// default registry, and referenced by other packages to avoid import cycles
// (auth, engine, and server all depend on metrics; metrics depends on
// nothing internal).
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// HTTPRequestDuration observes request latency per route/method/status. The
// route label is the chi route pattern (e.g. "/api/v1/challenges/{name}"),
// not the raw path, to keep cardinality bounded.
var HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "cyberkube_http_request_duration_seconds",
	Help:    "HTTP request duration in seconds.",
	Buckets: prometheus.DefBuckets,
}, []string{"route", "method", "status"})

// LoginsTotal counts login attempts by outcome ("success" or "failure").
var LoginsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "cyberkube_logins_total",
	Help: "Total login attempts by result.",
}, []string{"result"})

// RegistrationsTotal counts successful registrations.
var RegistrationsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "cyberkube_registrations_total",
	Help: "Total successful user registrations.",
})

// SubmissionsTotal counts flag submissions by outcome ("true" or "false").
var SubmissionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "cyberkube_submissions_total",
	Help: "Total flag submissions by correctness.",
}, []string{"correct"})

// ActiveWSClients gauges the number of currently connected WebSocket
// clients on this pod. Held at 0 until /api/v1/events is implemented.
var ActiveWSClients = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "cyberkube_active_ws_clients",
	Help: "Number of currently connected WebSocket clients on this pod.",
})

// ChallengeCacheSize gauges the number of Challenge CRs currently held in
// the informer cache.
var ChallengeCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "cyberkube_challenge_cache_size",
	Help: "Number of Challenge CRs currently held in the informer cache.",
})
