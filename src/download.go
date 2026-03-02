package main

import (
	"fmt"
	"log"
	"net/http"

	wattpilotutils "github.com/jetzlstorfer/wattpilot-exporter/utils"
	"github.com/xuri/excelize/v2"
)

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	data, err := calculateData(date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	f := excelize.NewFile()
	defer f.Close()

	sheet := "Sheet1"

	setCellOrError := func(col, row int, value interface{}) error {
		cell, err := excelize.CoordinatesToCellName(col, row)
		if err != nil {
			return err
		}
		return f.SetCellValue(sheet, cell, value)
	}

	// Header row
	headers := []string{"Date/Time", "Energy (kWh)", "Price (€)"}
	for i, h := range headers {
		if err := setCellOrError(i+1, 1, h); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			log.Printf("downloadHandler: header cell error: %v", err)
			return
		}
	}

	// Session rows
	for i, session := range data.Sessions {
		row := i + 2
		for col, val := range []interface{}{session.EndTime, session.Energy, session.Price} {
			if err := setCellOrError(col+1, row, val); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				log.Printf("downloadHandler: session cell error: %v", err)
				return
			}
		}
	}

	// Summary section (separated by a blank row)
	summaryRow := len(data.Sessions) + 3

	totalEnergy := wattpilotutils.RoundFloat(data.TotalEnergy, 2)
	totalPrice := wattpilotutils.RoundFloat(data.TotalPrice, 2)

	var pricePerKwh float64
	if data.TotalEnergy > 0 {
		pricePerKwh = data.TotalPrice / data.TotalEnergy
	}

	summaryRows := [][]interface{}{
		{"Total kWh", totalEnergy, "kWh"},
		{"Price per kWh (€)", wattpilotutils.RoundFloat(pricePerKwh, 5), "€/kWh"},
		{"Total Cost (€)", totalPrice, fmt.Sprintf("= %.2f kWh × %.5f €/kWh", totalEnergy, pricePerKwh)},
	}
	for i, rowVals := range summaryRows {
		for col, val := range rowVals {
			if err := setCellOrError(col+1, summaryRow+i, val); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				log.Printf("downloadHandler: summary cell error: %v", err)
				return
			}
		}
	}

	filename := fmt.Sprintf("%s-ladeabrechnung.xlsx", data.Date)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	if err := f.Write(w); err != nil {
		log.Printf("downloadHandler: failed to write xlsx: %v", err)
	}
}
