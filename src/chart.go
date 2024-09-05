package main

import (
	"net/http"

	wattpilotutils "github.com/jetzlstorfer/wattpilot-exporter/utils"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/go-echarts/go-echarts/v2/types"
)

func genLiquidItems(data []float32) []opts.LiquidData {
	items := make([]opts.LiquidData, 0)
	for i := 0; i < len(data); i++ {
		items = append(items, opts.LiquidData{Value: data[i]})
	}
	return items
}

func liquidArrow() *charts.Liquid {
	liquid := charts.NewLiquid()
	liquid.SetGlobalOptions(
		charts.WithTitleOpts(opts.Title{
			Title: "shape(Arrow)",
		}),
	)

	liquid.AddSeries("electricity", genLiquidItems([]float32{0.3, 0.4, 0.5})).
		SetSeriesOptions(
			charts.WithLiquidChartOpts(opts.LiquidChart{
				IsWaveAnimation: opts.Bool(true),
				Shape:           "arrow",
			}),
		)
	return liquid
}

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
		charts.WithInitializationOpts(opts.Initialization{Theme: types.ThemeWesteros}),
		charts.WithTitleOpts(opts.Title{
			Title:    "Stats",
			Subtitle: "Bar chart rendered by the http server this time",
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
				"kWh": true,
				"€":   false,
			}}),
	)

	months := []string{"2024-06", "2024-07", "2024-08", "2024-09"}
	// get data from wattpilot
	data := wattpilotutils.GetStatsForMonths(months)
	kwhData := make([]float64, 0)
	euroData := make([]float64, 0)
	for _, monthData := range data {
		//fmt.Println(month.Data)
		totalEnergy := 0.0
		totalEuro := 0.0
		for _, month := range monthData.Data {
			totalEnergy += month.Energy
			totalEuro += month.Energy * (month.Eco / 100) * wattpilotutils.OfficialPricePerKwh
		}

		kwhData = append(kwhData, wattpilotutils.RoundFloat(totalEnergy, 2))
		euroData = append(euroData, wattpilotutils.RoundFloat(totalEuro, 2))
	}
	// Put data into instance
	bar.SetXAxis(months).
		AddSeries("kWh", generateBarItems(kwhData)).
		AddSeries("€", generateBarItems(euroData))
	return bar
}

func chartHandler(w http.ResponseWriter, _ *http.Request) {
	page := components.NewPage()

	page.AddCharts(barChart())
	//page.AddCharts(liquidArrow())

	page.Render(w)
}
