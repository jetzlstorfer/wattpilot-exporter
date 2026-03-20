package handlers

import (
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/wattpilot"
)

// MonthStat holds aggregated statistics for a single month.
type MonthStat struct {
	Month    string
	Sessions int
	Energy   float64
	Price    float64
	Margin   float64
}

// ChartsData is the template context for the charts page.
type ChartsData struct {
	Months []MonthStat
}

// ChartHandler handles GET /charts.
func ChartHandler(templateDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// generate all months since June 2024
		firstMonthWithData := "2024-06"
		months := []string{firstMonthWithData}
		for nextMonth := wattpilot.GetNextMonth(firstMonthWithData); nextMonth != ""; nextMonth = wattpilot.GetNextMonth(nextMonth) {
			months = append(months, nextMonth)
		}

		// get data from wattpilot
		data, err := wattpilot.GetStatsForMonths(ctx, months)
		if err != nil {
			// Log the error but still render the page with a message
			slog.ErrorContext(ctx, "chartHandler: failed to get stats", "error", err)
			tmpl, tmplErr := template.ParseFiles(filepath.Join(templateDir, "charts.html"))
			if tmplErr != nil {
				slog.ErrorContext(ctx, "chartHandler: template parse error in error path", "error", tmplErr)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			if execErr := tmpl.Execute(w, ChartsData{Months: []MonthStat{}}); execErr != nil {
				slog.ErrorContext(ctx, "chartHandler: template execute error in error path", "error", execErr)
			}
			return
		}

		var monthStats []MonthStat

		for i, monthData := range data {
			totalEnergy := 0.0
			totalEuro := 0.0
			totalMargin := 0.0
			for _, session := range monthData.Data {
				totalEnergy += session.Energy
				totalEuro += wattpilot.CalculatePrice(session.End, session.Energy, 100)
				totalMargin += wattpilot.CalculatePriceMargin(session.End, session.Energy, session.Eco)
			}
			monthStats = append(monthStats, MonthStat{
				Month:    months[i],
				Sessions: len(monthData.Data),
				Energy:   wattpilot.RoundFloat(totalEnergy, 2),
				Price:    wattpilot.RoundFloat(totalEuro, 2),
				Margin:   wattpilot.RoundFloat(totalMargin, 2),
			})
		}

		tmpl, err := template.ParseFiles(filepath.Join(templateDir, "charts.html"))
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			slog.ErrorContext(ctx, "chartHandler: template parse error", "error", err)
			return
		}
		if err := tmpl.Execute(w, ChartsData{Months: monthStats}); err != nil {
			slog.ErrorContext(ctx, "chartHandler: template execute error", "error", err)
		}
	}
}
