package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/storage"
)

const (
	settingsPath = "data/config.json"
	geocodeURL   = "https://geocoding-api.open-meteo.com/v1/search"
	nominatimURL = "https://nominatim.openstreetmap.org/search"
)

var settingsHTTPClient = &http.Client{}

// Settings holds all user-configurable application settings.
type Settings struct {
	CarModel                  string             `json:"carModel"`
	OfficialPrices            map[string]float64 `json:"officialPrices"`
	PurchasePrices            map[string]float64 `json:"purchasePrices"`
	NetworkFeeMonthly         float64            `json:"networkFeeMonthly"`
	LiveChargingWindowMinutes int                `json:"liveChargingWindowMinutes"`
	DataTTLMinutes            int                `json:"dataTTLMinutes"`
	HomeAddress               string             `json:"homeAddress"`
	HomeLatitude              float64            `json:"homeLatitude"`
	HomeLongitude             float64            `json:"homeLongitude"`
	PVPeakKW                  float64            `json:"pvPeakKw"`
	PVPerformanceFactor       float64            `json:"pvPerformanceFactor"`
	ForecastTTLMinutes        int                `json:"forecastTTLMinutes"`
}

type geocodeResponse struct {
	Results []struct {
		Name      string  `json:"name"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Country   string  `json:"country"`
		Admin1    string  `json:"admin1"`
	} `json:"results"`
}

type nominatimResponse []struct {
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	DisplayName string `json:"display_name"`
}

var (
	current *Settings
	mu      sync.RWMutex
)

// Defaults returns settings with the original hardcoded values.
func Defaults() Settings {
	return Settings{
		CarModel: "Volvo EX40",
		OfficialPrices: map[string]float64{
			"2024": 0.33182,
			"2025": 0.35889,
			"2026": 0.32806,
		},
		PurchasePrices: map[string]float64{
			"2024": 0.2824,
			"2025": 0.25,
			"2026": 0.25,
		},
		NetworkFeeMonthly:         4.20,
		LiveChargingWindowMinutes: 5,
		DataTTLMinutes:            30,
		PVPerformanceFactor:       0.82,
		ForecastTTLMinutes:        120,
	}
}

// Get returns the current cached settings.
func Get() Settings {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		d := Defaults()
		return d
	}
	return *current
}

// Load reads settings from storage (local filesystem or Azure Blob Storage).
// Falls back to defaults if settings file doesn't exist or storage is unavailable.
func Load(ctx context.Context) {
	store := storage.Store()
	data, err := store.Read(ctx, settingsPath)
	if err != nil {
		slog.InfoContext(ctx, "settings: no existing config found, using defaults", "path", settingsPath)
		d := Defaults()
		mu.Lock()
		current = &d
		mu.Unlock()
		return
	}

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		slog.WarnContext(ctx, "settings: failed to parse config, using defaults", "error", err)
		d := Defaults()
		mu.Lock()
		current = &d
		mu.Unlock()
		return
	}

	// Merge with defaults to ensure all keys exist
	d := Defaults()
	if s.CarModel == "" {
		s.CarModel = d.CarModel
	}
	if s.OfficialPrices == nil {
		s.OfficialPrices = d.OfficialPrices
	}
	if s.PurchasePrices == nil {
		s.PurchasePrices = d.PurchasePrices
	}
	// For NetworkFeeMonthly: only apply default when the key is absent in JSON
	// (distinguishes "not configured yet" from "explicitly set to 0").
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err == nil {
		if _, ok := rawMap["networkFeeMonthly"]; !ok {
			s.NetworkFeeMonthly = d.NetworkFeeMonthly
		}
	}
	// For int settings: 0 is always invalid, so use default when unset.
	if s.LiveChargingWindowMinutes <= 0 {
		s.LiveChargingWindowMinutes = d.LiveChargingWindowMinutes
	}
	if s.DataTTLMinutes <= 0 {
		s.DataTTLMinutes = d.DataTTLMinutes
	}
	if s.PVPerformanceFactor <= 0 || s.PVPerformanceFactor > 1.2 {
		s.PVPerformanceFactor = d.PVPerformanceFactor
	}
	if s.ForecastTTLMinutes <= 0 {
		s.ForecastTTLMinutes = d.ForecastTTLMinutes
	}

	mu.Lock()
	current = &s
	mu.Unlock()
	slog.InfoContext(ctx, "settings: loaded from storage", "path", settingsPath)
}

// Save writes the given settings to storage (local filesystem or Azure Blob Storage) and updates the cache.
func Save(ctx context.Context, s Settings) error {
	store := storage.Store()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	if err := store.Write(ctx, settingsPath, data); err != nil {
		return err
	}

	mu.Lock()
	current = &s
	mu.Unlock()
	slog.InfoContext(ctx, "settings: saved to storage", "path", settingsPath)
	return nil
}

// GetOfficialPrice returns the official price for a given key.
// Accepts "YYYY-MM" or "YYYY". Lookup: exact match → year fallback → defaults.
func GetOfficialPrice(key string) float64 {
	return getPrice(key, func(s Settings) map[string]float64 { return s.OfficialPrices })
}

// GetPurchasePrice returns the purchase price for a given key.
// Accepts "YYYY-MM" or "YYYY". Lookup: exact match → year fallback → defaults.
func GetPurchasePrice(key string) float64 {
	return getPrice(key, func(s Settings) map[string]float64 { return s.PurchasePrices })
}

func getPrice(key string, prices func(Settings) map[string]float64) float64 {
	s := Get()
	m := prices(s)

	// 1. Exact match (works for both "YYYY-MM" and "YYYY" keys)
	if price, ok := m[key]; ok {
		return price
	}

	// 2. If key is "YYYY-MM", try year-only fallback
	if len(key) == 7 && key[4] == '-' {
		yearOnly := key[:4]
		if price, ok := m[yearOnly]; ok {
			return price
		}
	}

	// 3. Fall back to defaults
	d := Defaults()
	dm := prices(d)
	if price, ok := dm[key]; ok {
		return price
	}
	if len(key) == 7 && key[4] == '-' {
		if price, ok := dm[key[:4]]; ok {
			return price
		}
	}
	return dm["2025"]
}

const steirerStromFlexURL = "https://www.tarife.at/energie/anbieter/energie-steiermark/steirerstrom-flex-391"

// FetchSteirerStromFlexPrice fetches the current Arbeitspreis from tarife.at
// and returns it in €/kWh (e.g. 0.1362 for 13,62 ct/kWh).
func FetchSteirerStromFlexPrice() (float64, error) {
	resp, err := http.Get(steirerStromFlexURL)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch tarife.at page: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			slog.Warn("settings: failed to close tarife.at response body", "error", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read tarife.at response: %w", err)
	}

	// Look for Arbeitspreis value in the HTML — price may have whitespace around it
	re := regexp.MustCompile(`Arbeitspreis[\s\S]*?<span[^>]*class="[^"]*align-baseline[^"]*"[^>]*>\s*(\d+,\d+)\s*</span>`)
	matches := re.FindSubmatch(body)
	if len(matches) < 2 {
		return 0, fmt.Errorf("could not find Arbeitspreis on tarife.at page")
	}

	priceStr := strings.Replace(string(matches[1]), ",", ".", 1)
	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse price %q: %w", priceStr, err)
	}

	return price / 100, nil // ct/kWh → €/kWh
}

// GetCarModel returns the configured car model name.
func GetCarModel() string {
	return Get().CarModel
}

// GetNetworkFeeMonthly returns the monthly network infrastructure fee in €.
// Returns zero when the user has explicitly configured a zero fee.
func GetNetworkFeeMonthly() float64 {
	return Get().NetworkFeeMonthly
}

// GetLiveChargingWindowMinutes returns the number of minutes within which a
// session is considered "currently charging".
func GetLiveChargingWindowMinutes() int {
	s := Get()
	if s.LiveChargingWindowMinutes <= 0 {
		return Defaults().LiveChargingWindowMinutes
	}
	return s.LiveChargingWindowMinutes
}

// GetDataTTLMinutes returns the number of minutes after which cached data is
// considered stale and an auto-refresh should be attempted.
func GetDataTTLMinutes() int {
	s := Get()
	if s.DataTTLMinutes <= 0 {
		return Defaults().DataTTLMinutes
	}
	return s.DataTTLMinutes
}

// GetForecastAddress returns the configured home address.
func GetForecastAddress() string {
	return Get().HomeAddress
}

// GetForecastCoordinates returns the configured home latitude and longitude.
func GetForecastCoordinates() (float64, float64) {
	s := Get()
	return s.HomeLatitude, s.HomeLongitude
}

// GetPVPeakKW returns the configured PV system size in kWp.
func GetPVPeakKW() float64 {
	return Get().PVPeakKW
}

// GetPVPerformanceFactor returns the configured PV performance factor.
func GetPVPerformanceFactor() float64 {
	s := Get()
	if s.PVPerformanceFactor <= 0 || s.PVPerformanceFactor > 1.2 {
		return Defaults().PVPerformanceFactor
	}
	return s.PVPerformanceFactor
}

// GetForecastTTLMinutes returns the forecast cache TTL in minutes.
func GetForecastTTLMinutes() int {
	s := Get()
	if s.ForecastTTLMinutes <= 0 {
		return Defaults().ForecastTTLMinutes
	}
	return s.ForecastTTLMinutes
}

// ToJSON returns the settings as a formatted JSON reader (for blob upload).
func (s Settings) ToJSON() (*bytes.Reader, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// GeocodeAddress resolves a free-form street address into latitude and longitude.
func GeocodeAddress(ctx context.Context, address string) (float64, float64, string, error) {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return 0, 0, "", fmt.Errorf("address is empty")
	}

	lat, lon, label, err := geocodeWithOpenMeteo(ctx, trimmed)
	if err == nil {
		return lat, lon, label, nil
	}

	lat, lon, label, fallbackErr := geocodeWithNominatim(ctx, trimmed)
	if fallbackErr == nil {
		return lat, lon, label, nil
	}

	return 0, 0, "", fmt.Errorf("%w; fallback geocoder also failed: %v", err, fallbackErr)
}

func geocodeWithOpenMeteo(ctx context.Context, address string) (float64, float64, string, error) {

	query := url.Values{}
	query.Set("name", address)
	query.Set("count", "1")
	query.Set("language", "en")
	query.Set("format", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, geocodeURL+"?"+query.Encode(), nil)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to create geocoding request: %w", err)
	}

	resp, err := settingsHTTPClient.Do(req)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to geocode address: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, 0, "", fmt.Errorf("geocoding API returned status %d", resp.StatusCode)
	}

	var data geocodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, 0, "", fmt.Errorf("failed to decode geocoding response: %w", err)
	}
	if len(data.Results) == 0 {
		return 0, 0, "", fmt.Errorf("no coordinates found for %q", address)
	}

	match := data.Results[0]
	parts := []string{match.Name, match.Admin1, match.Country}
	locationName := strings.Join(filterEmpty(parts), ", ")
	return match.Latitude, match.Longitude, locationName, nil
}

func geocodeWithNominatim(ctx context.Context, address string) (float64, float64, string, error) {
	query := url.Values{}
	query.Set("q", address)
	query.Set("format", "jsonv2")
	query.Set("limit", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nominatimURL+"?"+query.Encode(), nil)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to create fallback geocoding request: %w", err)
	}
	req.Header.Set("User-Agent", "wattpilot-exporter/1.0")

	resp, err := settingsHTTPClient.Do(req)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to geocode address with fallback provider: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, 0, "", fmt.Errorf("fallback geocoding API returned status %d", resp.StatusCode)
	}

	var data nominatimResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, 0, "", fmt.Errorf("failed to decode fallback geocoding response: %w", err)
	}
	if len(data) == 0 {
		return 0, 0, "", fmt.Errorf("no coordinates found for %q", address)
	}

	lat, err := strconv.ParseFloat(data[0].Lat, 64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to parse fallback latitude %q: %w", data[0].Lat, err)
	}
	lon, err := strconv.ParseFloat(data[0].Lon, 64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to parse fallback longitude %q: %w", data[0].Lon, err)
	}

	return lat, lon, data[0].DisplayName, nil
}

func filterEmpty(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}
