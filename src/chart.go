package main

import (
	"net/http"

	wattpilotutils "github.com/jetzlstorfer/wattpilot-exporter/utils"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/go-echarts/go-echarts/v2/types"
)

// generate random data for bar chart
func generateBarItems(data []float64) []opts.BarData {
	items := make([]opts.BarData, 0)
	for i := 0; i < len(data); i++ {
		items = append(items, opts.BarData{Value: data[i]})
	}
	return items
}

func barChart() *charts.Bar {
	// create a new bar instance
	bar := charts.NewBar()

	// set some global options like Title/Legend/ToolTip or anything else
	bar.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Theme: types.ThemeWesteros,
			// Width:  "1200px",
			// Height: "600px",
		}),
		charts.WithTitleOpts(opts.Title{
			Title:    "Wattpilot Consumption Stats",
			Subtitle: "Stats calculated from Wattpilot data",
		}),
		charts.WithTooltipOpts(opts.Tooltip{
			Show:      opts.Bool(true),
			Trigger:   "item",
			Formatter: "{c} {a}",
		}),
		charts.WithDataZoomOpts(opts.DataZoom{
			Type:  "slider",
			Start: 0,
			End:   100,
		}),
		charts.WithLegendOpts(opts.Legend{
			Selected: map[string]bool{
				"kWh":      false,
				"€":        true,
				"€ Margin": true,
			}}),
	)

	// generate all months since June 2024
	firstMonthWithData := "2024-06"
	months := []string{firstMonthWithData}
	for nextMonth := wattpilotutils.GetNextMonth(firstMonthWithData); nextMonth != ""; nextMonth = wattpilotutils.GetNextMonth(nextMonth) {
		months = append(months, nextMonth)
	}

	// get data from wattpilot
	data := wattpilotutils.GetStatsForMonths(months)
	kwhData := make([]float64, 0)
	euroData := make([]float64, 0)
	marginData := make([]float64, 0)

	for _, monthData := range data {
		//fmt.Println(month.Data)
		totalEnergy := 0.0
		totalEuro := 0.0
		totalMargin := 0.0
		for _, month := range monthData.Data {
			totalEnergy += month.Energy
			totalEuro += wattpilotutils.CalculatePrice(month.End, month.Energy, 100)
			totalMargin += wattpilotutils.CalculatePriceMargin(month.End, month.Energy, month.Eco)
		}

		kwhData = append(kwhData, wattpilotutils.RoundFloat(totalEnergy, 2))
		euroData = append(euroData, wattpilotutils.RoundFloat(totalEuro, 2))
		marginData = append(marginData, wattpilotutils.RoundFloat(totalMargin, 2))
	}
	// Put data into instance
	bar.SetXAxis(months).
		AddSeries("kWh", generateBarItems(kwhData)).
		AddSeries("€", generateBarItems(euroData)).
		AddSeries("€ Margin", generateBarItems(marginData))
	return bar
}

func chartHandler(w http.ResponseWriter, _ *http.Request) {
	page := components.NewPage()

	page.AddCharts(barChart())
	//page.AddCharts(liquidArrow())

	page.Render(w)
}
