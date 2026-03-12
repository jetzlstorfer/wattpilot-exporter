package main

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.25.0"
)

const serviceName = "wattpilot-exporter"

// initTelemetry initialises the OpenTelemetry trace and log providers.
// When the OTEL_EXPORTER_OTLP_ENDPOINT environment variable is set the data is
// shipped to that collector via OTLP/HTTP; otherwise it falls back to writing
// human-readable output to stdout so the app is still observable in development
// without a collector.
//
// The returned shutdown function must be called (typically with defer) to flush
// and close all providers cleanly.
func initTelemetry(ctx context.Context) (shutdown func(context.Context) error, err error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	var shutdownFuncs []func(context.Context) error
	shutdown = func(ctx context.Context) error {
		var errs []error
		for _, fn := range shutdownFuncs {
			if err := fn(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("telemetry shutdown errors: %v", errs)
		}
		return nil
	}

	// ---- trace provider ----
	tp, err := newTracerProvider(ctx, res)
	if err != nil {
		return shutdown, fmt.Errorf("failed to create tracer provider: %w", err)
	}
	shutdownFuncs = append(shutdownFuncs, tp.Shutdown)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// ---- log provider ----
	lp, err := newLoggerProvider(ctx, res)
	if err != nil {
		return shutdown, fmt.Errorf("failed to create logger provider: %w", err)
	}
	shutdownFuncs = append(shutdownFuncs, lp.Shutdown)
	global.SetLoggerProvider(lp)

	return shutdown, nil
}

func newTracerProvider(ctx context.Context, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	var exporter sdktrace.SpanExporter
	var err error

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		exporter, err = otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
		}
	} else {
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout trace exporter: %w", err)
		}
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	return tp, nil
}

func newLoggerProvider(ctx context.Context, res *resource.Resource) (*sdklog.LoggerProvider, error) {
	var exporter sdklog.Exporter
	var err error

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		exporter, err = otlploghttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP log exporter: %w", err)
		}
	} else {
		exporter, err = stdoutlog.New()
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout log exporter: %w", err)
		}
	}

	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	)
	return lp, nil
}
