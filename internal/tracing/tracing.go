// Package tracing bootstraps the OpenTelemetry Go SDK for cyberkube: OTLP
// trace export, W3C propagation, and the resource identifying this service.
// It is wired once at boot (cmd/cyberkube); every other package that wants
// a span uses the global otel.Tracer/otel.GetTracerProvider, so this is the
// only package that touches SDK construction.
package tracing

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
)

// ServiceName is reported as the service.name resource attribute, and used
// as the default when OTEL_SERVICE_NAME is unset.
const ServiceName = "cyberkube"

// Shutdown flushes and stops the tracer provider. Safe to call even when
// tracing was never enabled (a no-op in that case); always safe to call more
// than once.
type Shutdown func(context.Context) error

func noopShutdown(context.Context) error { return nil }

// Setup wires the global TracerProvider and W3C propagator from standard
// OTEL_* environment variables (see the package doc comment on env() for the
// full list, and the Helm chart values for how they map to config).
//
// When neither OTEL_EXPORTER_OTLP_ENDPOINT nor
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT is set, tracing stays disabled: no
// exporter is created, no global TracerProvider is registered (callers fall
// back to the SDK's built-in no-op tracer, which is already the default
// before Setup runs), and Setup returns cleanly with no error and no log
// output. The W3C propagator is installed either way, so traceparent/
// baggage headers are still parsed and forwarded even when this replica
// itself never exports a span — that keeps distributed traces intact across
// hops even during partial rollout of OTEL_EXPORTER_OTLP_ENDPOINT.
//
// version is reported as the service.version resource attribute; pass "" if
// unknown (no build-time version variable wired yet).
func Setup(ctx context.Context, version string) (Shutdown, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return noopShutdown, nil
	}

	exporter, err := newExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("otlp trace exporter: %w", err)
	}

	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(serviceName()),
		semconv.ServiceVersion(version),
	))
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(samplerFromEnv()),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

func serviceName() string {
	if name := os.Getenv("OTEL_SERVICE_NAME"); name != "" {
		return name
	}
	return ServiceName
}

// newExporter builds the OTLP trace exporter for OTEL_EXPORTER_OTLP_PROTOCOL
// (default "http/protobuf", matching the Alloy OTLP/HTTP receiver already
// deployed). Endpoint/header/timeout configuration is read by the exporter
// itself from the standard OTEL_EXPORTER_OTLP_* env vars — Setup does not
// re-parse them.
func newExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL") == "grpc" {
		return otlptracegrpc.New(ctx)
	}
	return otlptracehttp.New(ctx)
}

// samplerFromEnv maps OTEL_TRACES_SAMPLER / OTEL_TRACES_SAMPLER_ARG to a
// sdktrace.Sampler. Only the standard sampler names are supported (no
// exotic/custom sampling per the observability design); anything unset or
// unrecognized falls back to ParentBased(AlwaysSample), i.e. sample
// everything that isn't already decided by an upstream parent span.
func samplerFromEnv() sdktrace.Sampler {
	switch os.Getenv("OTEL_TRACES_SAMPLER") {
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(samplerRatioArg())
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(samplerRatioArg()))
	default: // "", "always_on", "parentbased_always_on", or anything unknown.
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}

func samplerRatioArg() float64 {
	ratio, err := strconv.ParseFloat(os.Getenv("OTEL_TRACES_SAMPLER_ARG"), 64)
	if err != nil {
		return 1.0
	}
	return ratio
}
