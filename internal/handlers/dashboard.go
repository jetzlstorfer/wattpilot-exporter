package handlers

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/wattpilot"
)

var tracer = otel.Tracer("github.com/jetzlstorfer/wattpilot-exporter/internal/handlers")

// SessionPoint represents a single charging session for dashboard display.
type SessionPoint struct {
	EndTime string
	Energy  float64
	Price   float64
}

// DashboardData is the template context for the main dashboard page.
type DashboardData struct {
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

func calculateData(ctx context.Context, date string) (DashboardData, error) {
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
		return DashboardData{
			Date:          monthToCalculate,
			FormattedDate: "Invalid date",
			PrevMonth:     "",
			NextMonth:     "",
			Error:         "The requested month is invalid. Please use format YYYY-MM.",
		}, nil
	}
	formattedDate := parsedTime.Format("January 2006")

	parsedData, err := wattpilot.GetStatsForMonth(ctx, monthToCalculate)

	// Return data with error message if fetch/parse failed
	if err != nil {
		slog.ErrorContext(ctx, "Error fetching data", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return DashboardData{
			Date:          monthToCalculate,
			FormattedDate: formattedDate,
			PrevMonth:     wattpilot.GetPrevMonth(monthToCalculate),
			NextMonth:     wattpilot.GetNextMonth(monthToCalculate),
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
		price := wattpilot.CalculatePrice(data.End, data.Energy, 100)
		totalPrice += price
		totalMargin += wattpilot.CalculatePriceMargin(data.End, data.Energy, data.Eco)
		latestSession = data.End
		sessions = append(sessions, SessionPoint{
			EndTime: data.End,
			Energy:  wattpilot.RoundFloat(data.Energy, 2),
			Price:   wattpilot.RoundFloat(price, 2),
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

	return DashboardData{
		Date:             monthToCalculate,
		FormattedDate:    formattedDate,
		PrevMonth:        wattpilot.GetPrevMonth(monthToCalculate),
		NextMonth:        wattpilot.GetNextMonth(monthToCalculate),
		ChargingSessions: len(parsedData.Data),
		LatestSession:    latestSession,
		IsCharging:       activeSession,
		TotalEnergy:      totalEnergy,
		TotalPrice:       totalPrice,
		TotalMargin:      totalMargin,
		Sessions:         sessions,
	}, nil
}

// DashboardHandler handles GET / (main dashboard).
func DashboardHandler(templateDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		date := r.URL.Query().Get("date")
		data, err := calculateData(r.Context(), date)
		if err != nil {
			slog.ErrorContext(r.Context(), "mainHandler: error calculating data", "error", err)
		}

		// Surface a refresh-error message (passed via query param by RefreshHandler on failure).
		if refreshErr := r.URL.Query().Get("error"); refreshErr != "" && data.Error == "" {
			data.Error = refreshErr
		}

		tmpl, err := template.ParseFiles(filepath.Join(templateDir, "template.html"))
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			slog.ErrorContext(r.Context(), "mainHandler: template parse error", "error", err)
			return
		}
		if err := tmpl.Execute(w, data); err != nil {
			slog.ErrorContext(r.Context(), "mainHandler: template execute error", "error", err)
		}
	}
}

// InfoHandler handles GET /info.
func InfoHandler(templateDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles(filepath.Join(templateDir, "info.html"))
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			slog.ErrorContext(r.Context(), "infoHandler: template parse error", "error", err)
			return
		}
		if err := tmpl.Execute(w, nil); err != nil {
			slog.ErrorContext(r.Context(), "infoHandler: template execute error", "error", err)
		}
	}
}

// FaviconHandler serves /favicon.ico.
func FaviconHandler(staticDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		http.ServeFile(w, r, filepath.Join(staticDir, "favicon.ico"))
	}
}

// FaviconSVGHandler serves /favicon.svg.
func FaviconSVGHandler(staticDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		http.ServeFile(w, r, filepath.Join(staticDir, "favicon.svg"))
	}
}

// RefreshHandler handles GET /refresh.
func RefreshHandler(w http.ResponseWriter, r *http.Request) {
	err := wattpilot.RefreshData(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "refreshHandler: failed to refresh data", "error", err)
		// Redirect back to the dashboard so the user still sees the cached data.
		// The error message is surfaced via the ?error query parameter.
		http.Redirect(w, r, "/?error=Data+could+not+be+fetched+from+the+API.+Previous+data+is+still+displayed.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
