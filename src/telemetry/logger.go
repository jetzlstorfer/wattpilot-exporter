package telemetry

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

const defaultServiceName = "wattpilot-exporter"

var (
	logger        log.Logger
	loggerInitErr error
	loggerOnce    sync.Once
	provider      *sdklog.LoggerProvider
)

// Init sets up an OpenTelemetry LoggerProvider that exports to stdout so logs remain visible on the console.
func Init(ctx context.Context) error {
	loggerOnce.Do(func() {
		exporter, err := stdoutlog.New(stdoutlog.WithWriter(os.Stdout))
		if err != nil {
			loggerInitErr = err
			return
		}

		provider = sdklog.NewLoggerProvider(
			sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)),
			sdklog.WithResource(resource.NewSchemaless(
				attribute.String("service.name", defaultServiceName),
			)),
		)
		global.SetLoggerProvider(provider)
		logger = provider.Logger(defaultServiceName)
	})
	return loggerInitErr
}

// Shutdown flushes and closes the configured LoggerProvider.
func Shutdown(ctx context.Context) error {
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
