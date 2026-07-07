package server

import (
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/trace"

	"github.com/CyberKube-ISEN/cyberkube/internal/auth"
	"github.com/CyberKube-ISEN/cyberkube/internal/metrics"
)

// NewLogger builds a slog.Logger writing to out: JSON output when
// format is "json" (production, parsed by Loki/Alloy), human-readable text
// otherwise (local dev, the zero value).
func NewLogger(out io.Writer, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(out, opts)
	} else {
		handler = slog.NewTextHandler(out, opts)
	}
	return slog.New(handler)
}

// RequestLogger returns chi middleware that replaces middleware.Logger: it
// emits one structured log line per request (request_id, route, method,
// status, duration_ms, and user_id/team_id when authenticated) and records
// the request in the cyberkube_http_request_duration_seconds histogram.
//
// identify is used to best-effort resolve the caller's identity for logging
// purposes; it may be nil (no identity fields are logged) and must never
// itself enforce authentication (see auth.Handler.OptionalClaims).
func RequestLogger(logger *slog.Logger, identify func(*http.Request) *auth.Claims) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			duration := time.Since(start)
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = r.URL.Path
			}
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}

			attrs := []any{
				"request_id", middleware.GetReqID(r.Context()),
				"route", route,
				"method", r.Method,
				"status", status,
				"duration_ms", duration.Milliseconds(),
			}
			// trace_id/span_id correlate this log line with Tempo: present
			// whenever Tracing() (see tracing.go) produced or propagated a
			// valid span context, absent (no attrs, no noise) when tracing
			// is disabled and no upstream caller sent a traceparent either.
			if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
				attrs = append(attrs, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
			}
			if identify != nil {
				if claims := identify(r); claims != nil {
					attrs = append(attrs, "user_id", claims.Subject, "team_id", claims.TeamID)
				}
			}
			logger.Info("request", attrs...)

			metrics.HTTPRequestDuration.
				WithLabelValues(route, r.Method, strconv.Itoa(status)).
				Observe(duration.Seconds())
		})
	}
}
