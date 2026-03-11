package telemetry

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
)

// Init sets up the OpenTelemetry trace provider.
//
// Behavior by env vars:
//   - OTEL_EXPORTER_OTLP_ENDPOINT set → OTLP/HTTP exporter (production/Jaeger)
//   - OTEL_TRACES_EXPORTER=console  → stdout exporter (local dev)
//   - Default                       → no-op provider (zero overhead)
//
// Returns a shutdown function that must be called on exit.
func Init(ctx context.Context, serviceName, version string) (shutdown func(context.Context) error, err error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
			semconv.DeploymentEnvironmentName(envOrDefault("DEPLOYMENT_ENV", "development")),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create resource: %w", err)
	}

	var exporter sdktrace.SpanExporter

	switch {
	case os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "":
		exporter, err = otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("telemetry: create OTLP exporter: %w", err)
		}

	case os.Getenv("OTEL_TRACES_EXPORTER") == "console":
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("telemetry: create stdout exporter: %w", err)
		}

	default:
		// No-op: set global provider to default (noop) — zero overhead.
		return func(context.Context) error { return nil }, nil
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
