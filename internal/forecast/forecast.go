package forecast

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/settings"
	"github.com/jetzlstorfer/wattpilot-exporter/internal/storage"
)

var tracer = otel.Tracer("github.com/jetzlstorfer/wattpilot-exporter/internal/forecast")

const (
	cacheFileName         = "forecast/open-meteo.json"
	openMeteoBaseURL      = "https://api.open-meteo.com/v1/forecast"
	forecastTimeout       = 6 * time.Second
	recommendedWindowSize = 3
)

var httpClient = &http.Client{Timeout: forecastTimeout}

type HourForecast struct {
	TimeLabel         string
	TemperatureC      float64
	CloudCover        int
	PrecipitationRisk int
	PVKwh             float64
	WeatherLabel      string
	Recommended       bool
}

type DashboardForecast struct {
	Configured         bool
	HasWeather         bool
	HasPV              bool
	Available          bool
	Warning            string
	LastUpdated        string
	CurrentTemperature float64
	CurrentCloudCover  int
	CurrentRainRisk    int
	CurrentWeather     string
	ExpectedPVToday    float64
	ExpectedPVTomorrow float64
	BestWindowLabel    string
	BestWindowEnergy   float64
	Hours              []HourForecast
}

type openMeteoForecast struct {
	Hourly struct {
		Time                     []string  `json:"time"`
		Temperature2M            []float64 `json:"temperature_2m"`
		CloudCover               []int     `json:"cloud_cover"`
		PrecipitationProbability []int     `json:"precipitation_probability"`
		ShortwaveRadiation       []float64 `json:"shortwave_radiation"`
		WeatherCode              []int     `json:"weather_code"`
	} `json:"hourly"`
}

// GetDashboardForecast returns weather and PV planning data for the dashboard.
func GetDashboardForecast(ctx context.Context) DashboardForecast {
	return getDashboardForecastAt(ctx, time.Now())
}

func getDashboardForecastAt(ctx context.Context, now time.Time) DashboardForecast {
	ctx, span := tracer.Start(ctx, "GetDashboardForecast")
	defer span.End()

	lat, lon := settings.GetForecastCoordinates()
	peakKW := settings.GetPVPeakKW()
	configured := forecastConfigured(lat, lon)

	result := DashboardForecast{
		Configured: configured,
		HasWeather: configured,
		HasPV:      configured && peakKW > 0,
	}
	if !configured {
		result.Warning = "Add your home coordinates in Settings to enable weather and PV forecasts."
		return result
	}

	payload, lastUpdated, err := loadForecastPayload(ctx, lat, lon)
	if err != nil {
		slog.WarnContext(ctx, "forecast: unable to load forecast data", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result.Warning = "Forecast data is currently unavailable."
		return result
	}

	forecast, err := buildDashboardForecast(now, payload, lastUpdated, peakKW, settings.GetPVPerformanceFactor())
	if err != nil {
		slog.WarnContext(ctx, "forecast: unable to build dashboard forecast", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result.Warning = "Forecast data could not be processed."
		return result
	}

	span.SetAttributes(
		attribute.Bool("forecast.configured", configured),
		attribute.Int("forecast.hours", len(forecast.Hours)),
	)
	return forecast
}

func forecastConfigured(lat float64, lon float64) bool {
	if lat == 0 && lon == 0 {
		return false
	}
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

func buildDashboardForecast(now time.Time, payload []byte, lastUpdated time.Time, peakKW float64, performanceFactor float64) (DashboardForecast, error) {
	var api openMeteoForecast
	if err := json.Unmarshal(payload, &api); err != nil {
		return DashboardForecast{}, fmt.Errorf("failed to parse forecast payload: %w", err)
	}

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		loc = time.UTC
	}
	now = now.In(loc)

	hours := make([]HourForecast, 0, 24)
	todayKey := now.Format("2006-01-02")
	tomorrowKey := now.AddDate(0, 0, 1).Format("2006-01-02")
	var todayPV float64
	var tomorrowPV float64

	for index, rawTime := range api.Hourly.Time {
		ts, err := time.ParseInLocation("2006-01-02T15:04", rawTime, loc)
		if err != nil {
			continue
		}
		if ts.Before(now.Add(-30 * time.Minute)) {
			continue
		}
		if ts.After(now.Add(24 * time.Hour)) {
			break
		}

		temperature := valueAt(api.Hourly.Temperature2M, index)
		cloudCover := intValueAt(api.Hourly.CloudCover, index)
		precipitationRisk := intValueAt(api.Hourly.PrecipitationProbability, index)
		weatherCode := intValueAt(api.Hourly.WeatherCode, index)
		pvKwh := calculatePVEstimateKwh(valueAt(api.Hourly.ShortwaveRadiation, index), peakKW, performanceFactor)

		hour := HourForecast{
			TimeLabel:         ts.Format("Mon 15:04"),
			TemperatureC:      temperature,
			CloudCover:        cloudCover,
			PrecipitationRisk: precipitationRisk,
			PVKwh:             pvKwh,
			WeatherLabel:      weatherCodeLabel(weatherCode),
		}
		hours = append(hours, hour)

		switch ts.Format("2006-01-02") {
		case todayKey:
			todayPV += pvKwh
		case tomorrowKey:
			tomorrowPV += pvKwh
		}
	}

	forecast := DashboardForecast{
		Configured:         true,
		HasWeather:         true,
		HasPV:              peakKW > 0,
		Available:          len(hours) > 0,
		LastUpdated:        lastUpdated.In(loc).Format("02 Jan 15:04"),
		ExpectedPVToday:    roundFloat(todayPV, 2),
		ExpectedPVTomorrow: roundFloat(tomorrowPV, 2),
		Hours:              hours,
	}
	if len(hours) == 0 {
		forecast.Warning = "No forecast hours returned by the provider."
		return forecast, nil
	}

	current := hours[0]
	forecast.CurrentTemperature = roundFloat(current.TemperatureC, 1)
	forecast.CurrentCloudCover = current.CloudCover
	forecast.CurrentRainRisk = current.PrecipitationRisk
	forecast.CurrentWeather = current.WeatherLabel

	startIndex, endIndex, energy := recommendWindow(hours, recommendedWindowSize)
	if energy > 0 && startIndex >= 0 && endIndex >= startIndex {
		for i := startIndex; i <= endIndex; i++ {
			hours[i].Recommended = true
		}
		forecast.Hours = hours
		forecast.BestWindowLabel = fmt.Sprintf("%s to %s", hours[startIndex].TimeLabel, hours[endIndex].TimeLabel)
		forecast.BestWindowEnergy = roundFloat(energy, 2)
	} else if peakKW > 0 {
		forecast.Warning = "PV production is expected to stay low in the next 24 hours."
	}

	return forecast, nil
}

func loadForecastPayload(ctx context.Context, lat float64, lon float64) ([]byte, time.Time, error) {
	cachePath := cacheFilePath()
	modTime, modErr := storage.Store().ModTime(ctx, cachePath)
	cacheFresh := modErr == nil && time.Since(modTime) <= time.Duration(settings.GetForecastTTLMinutes())*time.Minute
	if cacheFresh {
		payload, err := storage.Store().Read(ctx, cachePath)
		if err == nil {
			return payload, modTime, nil
		}
	}

	payload, err := fetchForecast(ctx, lat, lon)
	if err == nil {
		if writeErr := storage.Store().Write(ctx, cachePath, payload); writeErr != nil {
			slog.WarnContext(ctx, "forecast: failed to cache payload", "error", writeErr)
		}
		return payload, time.Now(), nil
	}

	if modErr == nil {
		cachedPayload, readErr := storage.Store().Read(ctx, cachePath)
		if readErr == nil {
			return cachedPayload, modTime, nil
		}
	}

	return nil, time.Time{}, err
}

func fetchForecast(ctx context.Context, lat float64, lon float64) ([]byte, error) {
	ctx, span := tracer.Start(ctx, "fetchForecast")
	defer span.End()

	query := url.Values{}
	query.Set("latitude", fmt.Sprintf("%.5f", lat))
	query.Set("longitude", fmt.Sprintf("%.5f", lon))
	query.Set("timezone", "Europe/Berlin")
	query.Set("forecast_days", "3")
	query.Set("hourly", strings.Join([]string{
		"temperature_2m",
		"cloud_cover",
		"precipitation_probability",
		"shortwave_radiation",
		"weather_code",
	}, ","))

	requestURL := openMeteoBaseURL + "?" + query.Encode()
	span.SetAttributes(attribute.String("forecast.url", requestURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create forecast request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch forecast: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("forecast API returned status %d", resp.StatusCode)
	}

	var payload openMeteoForecast
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode forecast response: %w", err)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to store forecast response: %w", err)
	}
	return encoded, nil
}

func cacheFilePath() string {
	root := strings.TrimSpace(os.Getenv("WATTPILOT_DATA_DIR"))
	if root == "" {
		root = "data"
	}
	return filepath.Join(root, cacheFileName)
}

func calculatePVEstimateKwh(shortwaveRadiation float64, peakKW float64, performanceFactor float64) float64 {
	if peakKW <= 0 || shortwaveRadiation <= 0 {
		return 0
	}
	if performanceFactor <= 0 {
		performanceFactor = 0.82
	}
	return roundFloat((shortwaveRadiation/1000.0)*peakKW*performanceFactor, 2)
}

func recommendWindow(hours []HourForecast, windowSize int) (int, int, float64) {
	if len(hours) < windowSize || windowSize <= 0 {
		return -1, -1, 0
	}

	bestStart := -1
	bestEnergy := 0.0
	for start := 0; start <= len(hours)-windowSize; start++ {
		total := 0.0
		for offset := 0; offset < windowSize; offset++ {
			total += hours[start+offset].PVKwh
		}
		if total > bestEnergy {
			bestStart = start
			bestEnergy = total
		}
	}
	if bestStart == -1 {
		return -1, -1, 0
	}
	return bestStart, bestStart + windowSize - 1, bestEnergy
}

func weatherCodeLabel(code int) string {
	switch code {
	case 0:
		return "Clear"
	case 1:
		return "Mostly clear"
	case 2:
		return "Partly cloudy"
	case 3:
		return "Overcast"
	case 45, 48:
		return "Fog"
	case 51, 53, 55:
		return "Drizzle"
	case 61, 63, 65, 80, 81, 82:
		return "Rain"
	case 71, 73, 75:
		return "Snow"
	case 95:
		return "Thunderstorm"
	default:
		return "Mixed"
	}
}

func valueAt(values []float64, index int) float64 {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func intValueAt(values []int, index int) int {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func roundFloat(val float64, precision uint) float64 {
	ratio := 1.0
	for i := uint(0); i < precision; i++ {
		ratio *= 10
	}
	return float64(int(val*ratio+0.5)) / ratio
}
