package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"go.opentelemetry.io/otel/trace"
)

// tracingOperation is the otelhttp base span name used before chi has
// matched a route; Tracing renames every span before it ends (see below),
// so this value never reaches an exporter as-is.
const tracingOperation = "cyberkube.http"

// Tracing returns chi middleware that wraps every request in an OTel span
// via otelhttp (server timing/status attributes, W3C traceparent/baggage
// extraction) and names the span after the chi route pattern instead of the
// raw URL, the same way RequestLogger keeps its "route" log field and the
// http_request_duration metric's "route" label bounded.
//
// tp selects which TracerProvider starts the span. Pass nil in production
// (cmd/cyberkube does): otelhttp then resolves otel.GetTracerProvider() —
// whatever tracing.Setup registered, a no-op when
// OTEL_EXPORTER_OTLP_ENDPOINT is unset — fresh on every request. Tests that
// need a real, sampled span for a single assertion should pass an explicit
// provider here (via Config.TracerProvider) rather than calling the process-
// global otel.SetTracerProvider: that global is a one-way ratchet in the
// upstream SDK (see internal/global/state.go's "guaranteed to happen only
// once" delegate-swap) and cannot be reset between tests in the same binary.
//
// otelhttp only knows how to rename a span from the stdlib ServeMux's
// http.Request.Pattern (Go 1.22+), which chi never populates. So route
// naming happens here instead, reading chi's RouteContext after the inner
// handler returns — chi mutates that RouteContext in place while routing,
// so a post-hoc read sees the final matched pattern; this is the same
// technique RequestLogger already uses for its "route" attribute.
func Tracing(tp trace.TracerProvider) func(http.Handler) http.Handler {
	var opts []otelhttp.Option
	if tp != nil {
		opts = append(opts, otelhttp.WithTracerProvider(tp))
	}
	return func(next http.Handler) http.Handler {
		renamed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = r.URL.Path
			}
			span := trace.SpanFromContext(r.Context())
			span.SetName(r.Method + " " + route)
			span.SetAttributes(semconv.HTTPRoute(route))
		})
		return otelhttp.NewHandler(renamed, tracingOperation, opts...)
	}
}
