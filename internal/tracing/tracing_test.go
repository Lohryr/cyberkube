package tracing

import (
	"context"
	"os"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
)

// unsetForTest guarantees key is absent from the environment for the
// duration of the test, regardless of what the ambient environment (local
// shell, CI) already has set, then restores whatever was there before.
func unsetForTest(t *testing.T, key string) {
	t.Helper()
	original, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, original)
		}
	})
}

// TestSetupDisabledWithoutEndpoint is the mandatory minimal case from the
// design: booting with neither OTEL_EXPORTER_OTLP_ENDPOINT nor
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT set must not configure an exporter, must
// not register a TracerProvider (global stays whatever it already was —
// the SDK's built-in no-op before anyone calls Setup), and must return no
// error.
func TestSetupDisabledWithoutEndpoint(t *testing.T) {
	unsetForTest(t, "OTEL_EXPORTER_OTLP_ENDPOINT")
	unsetForTest(t, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")

	before := otel.GetTracerProvider()

	shutdown, err := Setup(context.Background(), "test")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if got := otel.GetTracerProvider(); got != before {
		t.Error("Setup registered a TracerProvider despite no OTLP endpoint being configured")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("noop shutdown returned an error: %v", err)
	}
}

// TestSetupInstallsW3CPropagatorEvenWhenDisabled checks that traceparent/
// baggage propagation stays active even when this replica never exports a
// span itself — required so a trace started upstream still flows through to
// e.g. the Kubernetes API calls this service makes.
func TestSetupInstallsW3CPropagatorEvenWhenDisabled(t *testing.T) {
	unsetForTest(t, "OTEL_EXPORTER_OTLP_ENDPOINT")
	unsetForTest(t, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")

	if _, err := Setup(context.Background(), ""); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	fields := otel.GetTextMapPropagator().Fields()
	for _, want := range []string{"traceparent", "baggage"} {
		found := false
		for _, f := range fields {
			if f == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("propagator fields = %v, want %q among them", fields, want)
		}
	}
}

// TestSetupEnabledRegistersProvider exercises the "endpoint configured"
// branch. otlptracehttp.New does not dial synchronously, so this needs no
// live OTLP receiver: it only proves Setup wires a real TracerProvider and
// that shutdown completes cleanly (no pending spans to flush).
func TestSetupEnabledRegistersProvider(t *testing.T) {
	unsetForTest(t, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")

	before := otel.GetTracerProvider()
	shutdown, err := Setup(context.Background(), "1.2.3")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { otel.SetTracerProvider(before) })

	if got := otel.GetTracerProvider(); got == before {
		t.Error("Setup did not register a new TracerProvider despite OTEL_EXPORTER_OTLP_ENDPOINT being set")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestSamplerFromEnv(t *testing.T) {
	tests := []struct {
		name        string
		sampler     string
		arg         string
		wantContain string
	}{
		{name: "unset defaults to parent-based always-on", wantContain: "ParentBased"},
		{name: "always_off", sampler: "always_off", wantContain: "AlwaysOffSampler"},
		{name: "traceidratio", sampler: "traceidratio", arg: "0.25", wantContain: "0.25"},
		{name: "parentbased_traceidratio", sampler: "parentbased_traceidratio", arg: "0.5", wantContain: "0.5"},
		{name: "unrecognized falls back to parent-based always-on", sampler: "not-a-real-sampler", wantContain: "ParentBased"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetForTest(t, "OTEL_TRACES_SAMPLER")
			unsetForTest(t, "OTEL_TRACES_SAMPLER_ARG")
			if tt.sampler != "" {
				t.Setenv("OTEL_TRACES_SAMPLER", tt.sampler)
			}
			if tt.arg != "" {
				t.Setenv("OTEL_TRACES_SAMPLER_ARG", tt.arg)
			}

			got := samplerFromEnv().Description()
			if !strings.Contains(got, tt.wantContain) {
				t.Errorf("samplerFromEnv().Description() = %q, want to contain %q", got, tt.wantContain)
			}
		})
	}
}
