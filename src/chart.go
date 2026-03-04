package main

import (
	"html/template"
	"log"
	"net/http"

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

func chartHandler(w http.ResponseWriter, _ *http.Request) {
	// generate all months since June 2024
	firstMonthWithData := "2024-06"
	months := []string{firstMonthWithData}
	for nextMonth := wattpilotutils.GetNextMonth(firstMonthWithData); nextMonth != ""; nextMonth = wattpilotutils.GetNextMonth(nextMonth) {
		months = append(months, nextMonth)
	}

	// get data from wattpilot
	data, err := wattpilotutils.GetStatsForMonths(months)
	if err != nil {
		// Log the error but still render the page with a message
		log.Printf("chartHandler: failed to get stats: %v", err)
		tmpl, tmplErr := template.ParseFiles("charts.html")
		if tmplErr != nil {
			log.Printf("chartHandler: template parse error in error path: %v", tmplErr)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if execErr := tmpl.Execute(w, ChartsData{Months: []MonthStat{}}); execErr != nil {
			log.Printf("chartHandler: template execute error in error path: %v", execErr)
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
		log.Printf("chartHandler: template parse error: %v", err)
		return
	}
	if err := tmpl.Execute(w, ChartsData{Months: monthStats}); err != nil {
		log.Printf("chartHandler: template execute error: %v", err)
	}
}
