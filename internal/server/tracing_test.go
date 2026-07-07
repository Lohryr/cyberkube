package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestRequestLoggerCorrelatesTraceID is the log<->trace correlation
// requirement: when Tracing() produces a real (sampled) span, RequestLogger
// must surface its trace_id/span_id in the structured log line.
//
// The provider is injected via Config.TracerProvider rather than
// otel.SetTracerProvider: the latter is a one-way ratchet in the global OTel
// SDK (see Tracing's doc comment) and would leak into every other test in
// this binary once called, so it must never be used here.
func TestRequestLoggerCorrelatesTraceID(t *testing.T) {
	var buf bytes.Buffer
	cfg := newTestConfig()
	cfg.Logger = NewLogger(&buf, "json")
	cfg.TracerProvider = sdktrace.NewTracerProvider() // records spans, exports nowhere: enough for a real, non-zero span context with no network dependency.
	h := New(cfg)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line not valid JSON: %v (%s)", err, buf.String())
	}

	traceID, _ := line["trace_id"].(string)
	if traceID == "" || traceID == "00000000000000000000000000000000" {
		t.Errorf("trace_id = %q, want a real non-zero trace id", traceID)
	}
	if spanID, _ := line["span_id"].(string); spanID == "" {
		t.Error("missing span_id field")
	}
}

// TestRequestLoggerOmitsTraceIDWhenTracingDisabled guards against log
// noise: with no TracerProvider configured (Config.TracerProvider left nil,
// falling back to the default global no-op) and no incoming traceparent
// header, RequestLogger must not emit trace_id/span_id at all.
func TestRequestLoggerOmitsTraceIDWhenTracingDisabled(t *testing.T) {
	var buf bytes.Buffer
	cfg := newTestConfig()
	cfg.Logger = NewLogger(&buf, "json")
	h := New(cfg)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line not valid JSON: %v (%s)", err, buf.String())
	}
	if v, ok := line["trace_id"]; ok {
		t.Errorf("trace_id = %v present with tracing disabled, want absent", v)
	}
}
