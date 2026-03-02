package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	wattpilotutils "github.com/jetzlstorfer/wattpilot-exporter/utils"
	"github.com/xuri/excelize/v2"
)

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01")
	}

	parsedData := wattpilotutils.GetStatsForMonth(date)
	pricePerKwh := wattpilotutils.GetOfficialPricePerKwhForMonth(date)

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

	// Header row – mirrors the full CSV export from wattpilot.io
	headers := []string{
		"#", "Start", "End",
		"Duration (total)", "Duration (charged)",
		"Max Power (kW)", "Max Current (A)",
		"Energy (kWh)", "Eco (%)",
		"ID Chip", "ID Chip Name",
		"Price (€)",
	}
	for i, h := range headers {
		if err := setCellOrError(i+1, 1, h); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			log.Printf("downloadHandler: header cell error: %v", err)
			return
		}
	}

	// Session rows
	totalEnergy := 0.0
	for i, entry := range parsedData.Data {
		row := i + 2
		price := wattpilotutils.RoundFloat(entry.Energy*pricePerKwh, 2)
		totalEnergy += entry.Energy
		rowVals := []interface{}{
			entry.SessionNumber,
			entry.Start,
			entry.End,
			entry.SecondsTotal,
			entry.SecondsCharged,
			entry.MaxPower,
			entry.MaxCurrent,
			wattpilotutils.RoundFloat(entry.Energy, 2),
			entry.Eco,
			entry.IDChip,
			entry.IDChipName,
			price,
		}
		for col, val := range rowVals {
			if err := setCellOrError(col+1, row, val); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				log.Printf("downloadHandler: session cell error: %v", err)
				return
			}
		}
	}

	// Summary section (separated by a blank row)
	summaryRow := len(parsedData.Data) + 3
	totalEnergyRounded := wattpilotutils.RoundFloat(totalEnergy, 2)
	totalCost := wattpilotutils.RoundFloat(totalEnergy*pricePerKwh, 2)

	summaryRows := [][]interface{}{
		{"Total kWh", totalEnergyRounded, "kWh"},
		{"Price per kWh (€)", wattpilotutils.RoundFloat(pricePerKwh, 5), "€/kWh"},
		{"Total Cost (€)", totalCost, fmt.Sprintf("= %.2f kWh × %.5f €/kWh", totalEnergyRounded, pricePerKwh)},
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

	filename := fmt.Sprintf("%s-ladeabrechnung.xlsx", date)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	if err := f.Write(w); err != nil {
		log.Printf("downloadHandler: failed to write xlsx: %v", err)
	}
}
