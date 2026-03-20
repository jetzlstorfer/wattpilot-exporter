package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/wattpilot"
	"github.com/xuri/excelize/v2"
)

// getEntryValue maps a wattpilot column key to the corresponding field value
// from a WattpilotEntry, preserving the original CSV structure.
func getEntryValue(key string, entry wattpilot.WattpilotEntry) interface{} {
	switch key {
	case "session_number":
		return entry.SessionNumber
	case "session_identifier":
		return entry.SessionIdentifier
	case "id_chip":
		return entry.IDChip
	case "id_chip_name":
		return "Volvo EX40"
	case "eco":
		return entry.Eco
	case "nexttrip":
		return entry.Nexttrip
	case "start":
		return entry.Start
	case "end":
		return entry.End
	case "seconds_total":
		return entry.SecondsTotal
	case "seconds_charged":
		return entry.SecondsCharged
	case "max_power":
		return entry.MaxPower
	case "max_current":
		return entry.MaxCurrent
	case "energy":
		return entry.Energy
	case "eto_start":
		return entry.EtoStart
	case "eto_end":
		return entry.EtoEnd
	default:
		return ""
	}
}

// DownloadHandler handles GET /download.
func DownloadHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	date := r.URL.Query().Get("date")

	monthToCalculate := time.Now().Format("2006-01")
	if date != "" {
		monthToCalculate = date
	}

	// Get the full wattpilot data for the month (preserving CSV structure)
	parsedData, err := wattpilot.GetStatsForMonth(ctx, monthToCalculate)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to generate report: %v", err), http.StatusInternalServerError)
		slog.ErrorContext(ctx, "downloadHandler: failed to get stats", "error", err)
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

	// Build visible column list (skip columns with hideInCsv=true)
	type visibleCol struct {
		Key     string
		Caption string
	}
	var columns []visibleCol
	for _, col := range parsedData.Columns {
		if col.HideInCsv {
			continue
		}
		caption := col.Caption
		if caption == "" {
			caption = col.Key
		}
		columns = append(columns, visibleCol{Key: col.Key, Caption: caption})
	}

	// Header row using original wattpilot captions
	for i, col := range columns {
		if err := setCellOrError(i+1, 1, col.Caption); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			slog.ErrorContext(ctx, "downloadHandler: header cell error", "error", err)
			return
		}
	}

	// Data rows preserving all visible fields
	var energyColIdx int // track energy column index for summary formula
	for i, col := range columns {
		if col.Key == "energy" {
			energyColIdx = i + 1
			break
		}
	}

	for i, entry := range parsedData.Data {
		row := i + 2
		for j, col := range columns {
			val := getEntryValue(col.Key, entry)
			if err := setCellOrError(j+1, row, val); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				slog.ErrorContext(ctx, "downloadHandler: data cell error", "error", err)
				return
			}
		}
	}

	// Summary section (separated by a blank row)
	numDataRows := len(parsedData.Data)
	summaryRow := numDataRows + 3

	// Calculate totals
	totalEnergy := 0.0
	for _, entry := range parsedData.Data {
		totalEnergy += entry.Energy
	}
	totalEnergy = wattpilot.RoundFloat(totalEnergy, 2)

	pricePerKwh := wattpilot.GetOfficialPricePerKwhForMonth(monthToCalculate)
	totalCost := wattpilot.RoundFloat(totalEnergy*pricePerKwh, 2)

	// Energy column SUM formula (e.g. =SUM(M2:M25))
	energyColName, _ := excelize.ColumnNumberToName(energyColIdx)
	energySumFormula := fmt.Sprintf("SUM(%s2:%s%d)", energyColName, energyColName, numDataRows+1)
	energySumCell, _ := excelize.CoordinatesToCellName(2, summaryRow)

	summaryRows := [][]interface{}{
		{"Total kWh", nil, "kWh"},
		{"Price per kWh (€)", wattpilot.RoundFloat(pricePerKwh, 5), "€/kWh"},
		{"Total Cost (€)", totalCost, fmt.Sprintf("= %.2f kWh × %.5f €/kWh", totalEnergy, pricePerKwh)},
	}
	for i, rowVals := range summaryRows {
		for col, val := range rowVals {
			if val == nil {
				continue
			}
			if err := setCellOrError(col+1, summaryRow+i, val); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				slog.ErrorContext(ctx, "downloadHandler: summary cell error", "error", err)
				return
			}
		}
	}

	// Set the energy SUM formula in the Total kWh row
	if err := f.SetCellFormula(sheet, energySumCell, energySumFormula); err != nil {
		slog.ErrorContext(ctx, "downloadHandler: formula error", "error", err)
	}

	filename := fmt.Sprintf("%s ladeabrechnung.xlsx", monthToCalculate)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	if err := f.Write(w); err != nil {
		slog.ErrorContext(ctx, "downloadHandler: failed to write xlsx", "error", err)
	}
}
