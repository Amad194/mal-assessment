package main

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// tracer is the package tracer; before initTracer runs it's a no-op, so handlers
// can call it unconditionally.
func tracer() trace.Tracer { return otel.Tracer("accounts-api") }

// initTracer wires an OTLP/gRPC exporter when OTEL_EXPORTER_OTLP_ENDPOINT is set
// (e.g. an OpenTelemetry Collector sidecar/daemonset). When unset it returns a
// no-op so local runs and tests need no collector. The collector fans out to
// Tempo / X-Ray / Honeycomb — the app stays backend-agnostic.
func initTracer(ctx context.Context, log *slog.Logger) func(context.Context) error {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		log.Info("tracing disabled (OTEL_EXPORTER_OTLP_ENDPOINT unset)")
		return func(context.Context) error { return nil }
	}
	exp, err := otlptracegrpc.New(ctx) // reads standard OTEL_* env vars
	if err != nil {
		log.Error("otel exporter init failed; continuing without tracing", "err", err)
		return func(context.Context) error { return nil }
	}
	res, _ := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", "accounts-api"),
	))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		// Sample 10% by default; head-based, parent-respecting.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))),
	)
	otel.SetTracerProvider(tp)
	log.Info("tracing enabled", "endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	return tp.Shutdown
}
