package forecast

import (
	"testing"
	"time"
)

func TestCalculatePVEstimateKwh(t *testing.T) {
	got := calculatePVEstimateKwh(600, 8.5, 0.82)
	if got != 4.18 {
		t.Fatalf("calculatePVEstimateKwh() = %v, want 4.18", got)
	}
}

func TestRecommendWindow(t *testing.T) {
	hours := []HourForecast{
		{PVKwh: 0.1},
		{PVKwh: 1.2},
		{PVKwh: 2.1},
		{PVKwh: 2.4},
		{PVKwh: 1.3},
	}

	start, end, energy := recommendWindow(hours, 3)
	if start != 2 || end != 4 {
		t.Fatalf("recommendWindow() = (%d, %d), want (2, 4)", start, end)
	}
	if energy != 5.8 {
		t.Fatalf("recommendWindow() energy = %v, want 5.8", energy)
	}
}

func TestBuildDashboardForecast(t *testing.T) {
	payload := []byte(`{
		"hourly": {
			"time": ["2026-05-11T10:00", "2026-05-11T11:00", "2026-05-11T12:00", "2026-05-11T13:00"],
			"temperature_2m": [18.2, 19.5, 21.1, 22.0],
			"cloud_cover": [70, 50, 20, 15],
			"precipitation_probability": [10, 10, 5, 5],
			"shortwave_radiation": [250, 400, 700, 650],
			"weather_code": [3, 2, 1, 0]
		}
	}`)

	now := time.Date(2026, 5, 11, 10, 15, 0, 0, time.FixedZone("CEST", 2*60*60))
	lastUpdated := time.Date(2026, 5, 11, 9, 55, 0, 0, time.FixedZone("CEST", 2*60*60))

	forecast, err := buildDashboardForecast(now, payload, lastUpdated, 8.0, 0.8)
	if err != nil {
		t.Fatalf("buildDashboardForecast() error = %v", err)
	}
	if !forecast.Available {
		t.Fatal("forecast should be available")
	}
	if forecast.CurrentWeather == "" {
		t.Fatal("current weather should be populated")
	}
	if forecast.BestWindowLabel == "" {
		t.Fatal("best window should be populated")
	}
	if forecast.ExpectedPVToday <= 0 {
		t.Fatalf("expected PV today = %v, want > 0", forecast.ExpectedPVToday)
	}
	if len(forecast.Hours) != 4 {
		t.Fatalf("len(hours) = %d, want 4", len(forecast.Hours))
	}
	if !forecast.Hours[1].Recommended || !forecast.Hours[2].Recommended || !forecast.Hours[3].Recommended {
		t.Fatal("recommended window hours were not marked")
	}
	if forecast.Hours[0].Recommended {
		t.Fatal("non-recommended hour was incorrectly marked")
	}
	if forecast.LastUpdated == "" {
		t.Fatal("last updated should be populated")
	}
}
