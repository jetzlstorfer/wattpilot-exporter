package telemetry

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const defaultServiceName = "wattpilot-exporter"

var (
	logger        log.Logger
	loggerInitErr error
	loggerOnce    sync.Once
	provider      *sdklog.LoggerProvider
	traceProvider *sdktrace.TracerProvider
	tracer        trace.Tracer
)

// Init sets up OpenTelemetry LoggerProvider and TracerProvider that export to stdout so telemetry remains visible on the console.
func Init(ctx context.Context) error {
	loggerOnce.Do(func() {
		res := resource.NewSchemaless(
			attribute.String("service.name", defaultServiceName),
		)

		exporter, err := stdoutlog.New(stdoutlog.WithWriter(os.Stdout))
		if err != nil {
			loggerInitErr = err
			return
		}

		provider = sdklog.NewLoggerProvider(
			sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)),
			sdklog.WithResource(res),
		)
		global.SetLoggerProvider(provider)
		logger = provider.Logger(defaultServiceName)

		traceExp, err := stdouttrace.New(stdouttrace.WithWriter(os.Stdout), stdouttrace.WithPrettyPrint())
		if err != nil {
			loggerInitErr = err
			return
		}
		traceProvider = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExp),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(traceProvider)
		tracer = traceProvider.Tracer(defaultServiceName)
	})
	return loggerInitErr
}

// Shutdown flushes and closes the configured LoggerProvider.
func Shutdown(ctx context.Context) error {
	if traceProvider != nil {
		if err := traceProvider.Shutdown(ctx); err != nil {
			return err
		}
	}
	if provider == nil {
		return nil
	}
	return provider.Shutdown(ctx)
}

func ensureLogger(ctx context.Context) {
	if logger != nil || loggerInitErr != nil {
		return
	}
	_ = Init(ctx)
}

func ensureTracer(ctx context.Context) {
	if tracer != nil || loggerInitErr != nil {
		return
	}
	_ = Init(ctx)
}

// StartSpan starts a new tracing span with optional attributes.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	ensureTracer(ctx)
	if tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

func emit(ctx context.Context, severity log.Severity, severityText, msg string) {
	ensureLogger(ctx)
	if logger == nil {
		// Initialization failed, nothing to emit.
		return
	}

	var record log.Record
	now := time.Now()
	record.SetTimestamp(now)
	record.SetObservedTimestamp(now)
	record.SetSeverity(severity)
	record.SetSeverityText(severityText)
	record.SetBody(log.StringValue(msg))

	logger.Emit(ctx, record)
}

// Info writes an informational log entry.
func Info(msg string) {
	emit(context.Background(), log.SeverityInfo, "INFO", msg)
}

// Infof writes a formatted informational log entry.
func Infof(format string, args ...interface{}) {
	emit(context.Background(), log.SeverityInfo, "INFO", fmt.Sprintf(format, args...))
}

// Errorf writes a formatted error log entry.
func Errorf(format string, args ...interface{}) {
	emit(context.Background(), log.SeverityError, "ERROR", fmt.Sprintf(format, args...))
}

// Fatalf writes a formatted fatal log entry and terminates the process.
func Fatalf(format string, args ...interface{}) {
	emit(context.Background(), log.SeverityFatal, "FATAL", fmt.Sprintf(format, args...))
	os.Exit(1)
}
