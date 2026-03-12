package main

import (
	"context"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	wattpilotutils "github.com/jetzlstorfer/wattpilot-exporter/utils"
	"github.com/joho/godotenv"
)

type SessionPoint struct {
	EndTime string
	Energy  float64
	Price   float64
}

type Data struct {
	Date             string
	FormattedDate    string
	PrevMonth        string
	NextMonth        string
	ChargingSessions int
	LatestSession    string
	IsCharging       bool
	TotalEnergy      float64
	TotalPrice       float64
	TotalMargin      float64
	Sessions         []SessionPoint
	Error            string
}

var tracer = otel.Tracer("github.com/jetzlstorfer/wattpilot-exporter")

func calculateData(ctx context.Context, date string) (Data, error) {
	ctx, span := tracer.Start(ctx, "calculateData")
	defer span.End()

	monthToCalculate := time.Now().Format("2006-01")
	if date != "" {
		monthToCalculate = date
	}
	span.SetAttributes(attribute.String("month", monthToCalculate))

	// Parse and format the date for display
	parsedTime, err := time.Parse("2006-01", monthToCalculate)
	if err != nil {
		slog.WarnContext(ctx, "Invalid date parameter", "date", monthToCalculate, "error", err)
		return Data{
			Date:          monthToCalculate,
			FormattedDate: "Invalid date",
			PrevMonth:     "",
			NextMonth:     "",
			Error:         "The requested month is invalid. Please use format YYYY-MM.",
		}, nil
	}
	formattedDate := parsedTime.Format("January 2006")

	parsedData, err := wattpilotutils.GetStatsForMonth(ctx, monthToCalculate)

	// Return data with error message if fetch/parse failed
	if err != nil {
		slog.ErrorContext(ctx, "Error fetching data", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Data{
			Date:          monthToCalculate,
			FormattedDate: formattedDate,
			PrevMonth:     wattpilotutils.GetPrevMonth(monthToCalculate),
			NextMonth:     wattpilotutils.GetNextMonth(monthToCalculate),
			Error:         "Unable to fetch charging data. Please try refreshing or check back later.",
		}, nil
	}

	// Calculate total energy & price
	totalEnergy := 0.0
	totalPrice := 0.0
	totalMargin := 0.0
	latestSession := ""

	var sessions []SessionPoint

	// loop over the data
	for _, data := range parsedData.Data {
		totalEnergy += data.Energy
		price := wattpilotutils.CalculatePrice(data.End, data.Energy, 100)
		totalPrice += price
		totalMargin += wattpilotutils.CalculatePriceMargin(data.End, data.Energy, data.Eco)
		latestSession = data.End
		sessions = append(sessions, SessionPoint{
			EndTime: data.End,
			Energy:  wattpilotutils.RoundFloat(data.Energy, 2),
			Price:   wattpilotutils.RoundFloat(price, 2),
		})
	}
	activeSession := false
	loc, _ := time.LoadLocation("Europe/Berlin")
	latestSessionTimeStamp, _ := time.Parse(time.DateTime, latestSession)
	latestSessionTimeStamp = latestSessionTimeStamp.Add(-2 * time.Hour) // fix for timezone

	if latestSessionTimeStamp.Add(1 * time.Minute).After(time.Now().In(loc)) {
		// session is active
		activeSession = true
	}

	// fmt.Println("Total Energy in kWh:", totalEnergy)
	// fmt.Println("Total Energy in €:", totalPrice)

	return Data{
		Date:             monthToCalculate,
		FormattedDate:    formattedDate,
		PrevMonth:        wattpilotutils.GetPrevMonth(monthToCalculate),
		NextMonth:        wattpilotutils.GetNextMonth(monthToCalculate),
		ChargingSessions: len(parsedData.Data),
		LatestSession:    latestSession,
		IsCharging:       activeSession,
		TotalEnergy:      totalEnergy,
		TotalPrice:       totalPrice,
		TotalMargin:      totalMargin,
		Sessions:         sessions}, nil

}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("info.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		slog.ErrorContext(r.Context(), "infoHandler: template parse error", "error", err)
		return
	}
	if err := tmpl.Execute(w, nil); err != nil {
		slog.ErrorContext(r.Context(), "infoHandler: template execute error", "error", err)
	}
}

func mainHandler(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	data, err := calculateData(r.Context(), date)
	if err != nil {
		// Still show the UI even if there's an error in calculateData
		// (though calculateData now returns nil errors and embeds error message in data)
		slog.ErrorContext(r.Context(), "mainHandler: error calculating data", "error", err)
	}

	tmpl, err := template.ParseFiles("template.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		slog.ErrorContext(r.Context(), "mainHandler: template parse error", "error", err)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		slog.ErrorContext(r.Context(), "mainHandler: template execute error", "error", err)
	}
}

func faviconHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	http.ServeFile(w, r, "favicon.ico")
}

func faviconSVGHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	http.ServeFile(w, r, "favicon.svg")
}

func refreshHandler(w http.ResponseWriter, r *http.Request) {
	// refresh data
	err := wattpilotutils.RefreshData(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "refreshHandler: failed to refresh data", "error", err)
		http.Error(w, "Failed to refresh data. Please try again later.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// get env variables from .env file
	// Using Overload() instead of Load() to ensure .env always takes precedence
	// over any pre-existing environment variables (e.g. empty WATTPILOT_KEY in shell)
	if err := godotenv.Overload(); err != nil {
		// Not fatal — the variable may already be in the environment
		log.Println("Note: .env file not loaded:", err)
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

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/favicon.ico", faviconHandler)
	mux.HandleFunc("/favicon.svg", faviconSVGHandler)
	mux.HandleFunc("/", mainHandler)
	mux.HandleFunc("/refresh", refreshHandler)
	mux.HandleFunc("/charts", chartHandler)
	mux.HandleFunc("/info", infoHandler)
	mux.HandleFunc("/download", downloadHandler)

	// Wrap the entire mux with the OTel HTTP middleware so every request gets
	// a trace span, propagation headers are read/written, and standard HTTP
	// attributes (method, path, status code, …) are recorded automatically.
	handler := otelhttp.NewHandler(mux, "wattpilot-exporter")

	srv := &http.Server{
		Addr:    ":8080",
		Handler: handler,
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
