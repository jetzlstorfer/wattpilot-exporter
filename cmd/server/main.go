package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/handlers"
	"github.com/jetzlstorfer/wattpilot-exporter/internal/settings"
	"github.com/jetzlstorfer/wattpilot-exporter/internal/wattpilot"
	"github.com/joho/godotenv"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// get env variables from .env file
	// Using Overload() instead of Load() to ensure .env always takes precedence
	// over any pre-existing environment variables (e.g. empty WATTPILOT_KEY in shell)
	if os.Getenv("WATTPILOT_SKIP_DOTENV") != "1" {
		if err := godotenv.Overload(); err != nil {
			// Not fatal — the variable may already be in the environment
			log.Println("Note: .env file not loaded:", err)
		}
	} else {
		log.Println("Note: skipping .env loading because WATTPILOT_SKIP_DOTENV=1")
	}

	// Initialise OpenTelemetry (traces + logs).
	shutdown, err := initTelemetry(ctx)
	if err != nil {
		log.Fatalf("Failed to initialise telemetry: %v", err)
	}
	defer func() {
		if err := shutdown(context.Background()); err != nil {
			log.Printf("Telemetry shutdown error: %v", err)
		}
	}()

	// Replace the default slog logger with one that bridges to OTel.
	logger := otelslog.NewLogger(serviceName)
	slog.SetDefault(logger)

	// Initialise the data store (local filesystem or Azure Blob Storage).
	wattpilot.InitStore(ctx)

	// Load settings from Azure Blob Storage (falls back to defaults)
	settings.Load(ctx)

	templateDir := "templates"
	staticDir := "static"

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	mux.HandleFunc("/favicon.ico", handlers.FaviconHandler(staticDir))
	mux.HandleFunc("/favicon.svg", handlers.FaviconSVGHandler(staticDir))
	mux.HandleFunc("/", handlers.DashboardHandler(templateDir))
	mux.HandleFunc("/refresh", handlers.RefreshHandler)
	mux.HandleFunc("/charts", handlers.ChartHandler(templateDir))
	mux.HandleFunc("/info", handlers.InfoHandler(templateDir))
	mux.HandleFunc("/settings", handlers.SettingsHandler(templateDir))
	mux.HandleFunc("/settings/fetch-price", handlers.FetchPriceHandler)
	mux.HandleFunc("/download", handlers.DownloadHandler)

	// Wrap the entire mux with the OTel HTTP middleware so every request gets
	// a trace span, propagation headers are read/written, and standard HTTP
	// attributes (method, path, status code, …) are recorded automatically.
	handler := otelhttp.NewHandler(mux, "wattpilot-exporter")

	srv := &http.Server{
		Addr:    ":8080",
		Handler: handler,
		// ReadTimeout covers reading the request headers/body.
		// WriteTimeout must exceed the API fetch timeout (FetchTimeoutSeconds = 30s)
		// to allow handlers that trigger a data refresh to complete successfully.
		// IdleTimeout limits keep-alive connections between requests.
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	slog.InfoContext(ctx, "Starting server", "addr", ":8080")
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.InfoContext(context.Background(), "Shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server shutdown error", "error", err)
	}
}
