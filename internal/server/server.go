// Package server wires the HTTP router: /api/v1 (the contractual API,
// documented in api/openapi.yaml) with a temporary /api alias for the
// v2->v3 cutover, plus the infra-facing /healthz and /auth/verify routes
// and the unauthenticated Prometheus /metrics endpoint.
package server

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/CyberKube-ISEN/cyberkube/internal/auth"
	"github.com/CyberKube-ISEN/cyberkube/internal/engine"
	"github.com/CyberKube-ISEN/cyberkube/internal/events"
	"github.com/CyberKube-ISEN/cyberkube/internal/teams"
)

// Config carries the handlers to mount.
type Config struct {
	Auth   *auth.Handler
	Teams  *teams.Handler
	Engine *engine.Handler

	// Events serves WS /api/v1/events. Optional: nil disables the route
	// entirely rather than mounting a handler that would panic.
	Events *events.Handler

	// Logger receives one structured line per request. Defaults to a
	// discarded text logger when nil (tests, and callers that do not care).
	Logger *slog.Logger

	// TracerProvider starts the per-request span (see Tracing). Nil (the
	// production default set by cmd/cyberkube) means "use whatever
	// tracing.Setup registered as the global OTel TracerProvider"; set this
	// explicitly only in tests that need a deterministic provider without
	// touching global OTel state (see Tracing's doc comment for why).
	TracerProvider trace.TracerProvider
}

// New builds the chi router with all routes.
func New(cfg Config) http.Handler {
	logger := cfg.Logger
	if logger == nil {
		logger = NewLogger(io.Discard, "text")
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	// Tracing before RequestLogger: it puts a span (real or propagated, see
	// Tracing's doc comment) in the request context so the log line below
	// can pull trace_id/span_id out of it for Loki<->Tempo correlation.
	r.Use(Tracing(cfg.TracerProvider))
	r.Use(RequestLogger(logger, cfg.Auth.OptionalClaims))
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Auth check target: NGINX Ingress auth-url subrequest, Envoy Gateway
	// SecurityPolicy extAuth, and the frontend's session validity probe.
	// Envoy's ext_authz HTTP service appends the original request path to
	// the configured path (/auth/verify + /foo → /auth/verify/foo), so the
	// wildcard variant must answer too.
	r.Get("/auth/verify", cfg.Auth.Verify)
	r.Get("/auth/verify/*", cfg.Auth.Verify)

	// Prometheus scrape target: unauthenticated, same port as the API (no
	// separate metrics listener to manage in the chart).
	r.Handle("/metrics", promhttp.Handler())

	// /api/v1 is the contractual API. /api is a legacy alias mounting the
	// exact same handlers (no duplicated logic) until the v2 frontend
	// cutover completes, per the platform-api spec.
	r.Route("/api/v1", func(r chi.Router) { mountAPI(r, cfg) })
	r.Route("/api", func(r chi.Router) { mountAPI(r, cfg) })

	return r
}

// mountAPI registers the business routes on r. Called twice (under /api/v1
// and /api) so both prefixes share the same handlers.
func mountAPI(r chi.Router, cfg Config) {
	r.Post("/register", cfg.Auth.Register)
	r.Post("/login", cfg.Auth.Login)

	r.Group(func(r chi.Router) {
		r.Use(cfg.Auth.Middleware)
		r.Get("/me", cfg.Auth.Me)

		r.Post("/teams", cfg.Teams.Create)
		r.Post("/teams/join", cfg.Teams.Join)
		r.Get("/teams/mine", cfg.Teams.Mine)

		r.Get("/world", cfg.Engine.World)
		if cfg.Events != nil {
			r.Get("/events", cfg.Events.ServeWS)
		}

		r.Get("/challenges", cfg.Engine.List)
		r.Get("/challenges/{name}/attachments/{attachment}", cfg.Engine.Attachment)
		r.Post("/challenges/{name}/submit", cfg.Engine.Submit)
		r.Post("/challenges/{name}/launch", cfg.Engine.Launch)
		r.Get("/challenges/{name}/instance", cfg.Engine.InstanceStatus)
		r.Get("/scoreboard", cfg.Engine.Scoreboard)
	})
}
