package main

import (
	"html/template"
	"net/http"

	"github.com/jetzlstorfer/wattpilot-exporter/telemetry"
	wattpilotutils "github.com/jetzlstorfer/wattpilot-exporter/utils"
)

type MonthStat struct {
	Month    string
	Sessions int
	Energy   float64
	Price    float64
	Margin   float64
}

type ChartsData struct {
	Months []MonthStat
}

func chartHandler(w http.ResponseWriter, r *http.Request) {
	_, span := telemetry.StartSpan(r.Context(), "chartHandler")
	defer span.End()
	// generate all months since June 2024
	firstMonthWithData := "2024-06"
	months := []string{firstMonthWithData}
	for nextMonth := wattpilotutils.GetNextMonth(firstMonthWithData); nextMonth != ""; nextMonth = wattpilotutils.GetNextMonth(nextMonth) {
		months = append(months, nextMonth)
	}

	// get data from wattpilot
	data := wattpilotutils.GetStatsForMonths(months)
	var monthStats []MonthStat

	for i, monthData := range data {
		totalEnergy := 0.0
		totalEuro := 0.0
		totalMargin := 0.0
		for _, session := range monthData.Data {
			totalEnergy += session.Energy
			totalEuro += wattpilotutils.CalculatePrice(session.End, session.Energy, 100)
			totalMargin += wattpilotutils.CalculatePriceMargin(session.End, session.Energy, session.Eco)
		}
		monthStats = append(monthStats, MonthStat{
			Month:    months[i],
			Sessions: len(monthData.Data),
			Energy:   wattpilotutils.RoundFloat(totalEnergy, 2),
			Price:    wattpilotutils.RoundFloat(totalEuro, 2),
			Margin:   wattpilotutils.RoundFloat(totalMargin, 2),
		})
	}

	tmpl, err := template.ParseFiles("charts.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		telemetry.Errorf("chartHandler: template parse error: %v", err)
		return
	}
	if err := tmpl.Execute(w, ChartsData{Months: monthStats}); err != nil {
		telemetry.Errorf("chartHandler: template execute error: %v", err)
	}
}
