package wattpilot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/settings"
	"github.com/jetzlstorfer/wattpilot-exporter/internal/snapshot"
	"github.com/jetzlstorfer/wattpilot-exporter/internal/storage"
)

var tracer = otel.Tracer("github.com/jetzlstorfer/wattpilot-exporter/internal/wattpilot")

const OfficialPricePerKwh2024 = 0.33182
const OfficialPricePerKwh2025 = 0.35889 // https://www.bmf.gv.at/themen/steuern/arbeitnehmerinnenveranlagung/pendlerfoerderung-das-pendlerpauschale/sachbezug-kraftfahrzeug.html
const OfficialPricePerKwh2026 = 0.32806
const PurchasePricePerKwh2024 = 0.2824
const PurchasePricePerKwh2025 = 0.25
const PurchasePricePerKwh2026 = 0.25
const JSONFileName = "data.json"
const WattpilotDataUrl = "https://data.wattpilot.io/api/v1/direct_json?e=TBD&from=TBD&to=TBD&timezone=Europe%2FVienna"
const DataTTLMinutes = 60     // Auto-refresh data if older than this many minutes
const FetchTimeoutSeconds = 5 // Timeout for outbound API requests

// httpClient is a shared HTTP client with an explicit timeout so that
// requests to the Wattpilot API never hang indefinitely.
var httpClient = &http.Client{
	Timeout: FetchTimeoutSeconds * time.Second,
}

func dataRootDir() string {
	if root := strings.TrimSpace(os.Getenv("WATTPILOT_DATA_DIR")); root != "" {
		return root
	}
	return "data"
}

func dataFilePath(name string) string {
	return filepath.Join(dataRootDir(), name)
}

type WattpilotColumn struct {
	Key       string `json:"key"`
	Caption   string `json:"caption,omitempty"`
	Hide      bool   `json:"hide,omitempty"`
	HideInCsv bool   `json:"hideInCsv,omitempty"`
	Unit      string `json:"unit,omitempty"`
	Type      string `json:"type,omitempty"`
}

type WattpilotData struct {
	Columns []WattpilotColumn `json:"columns"`
	Data    []WattpilotEntry  `json:"data"`
}

type WattpilotEntry struct {
	SessionNumber     int     `json:"session_number"`
	SessionIdentifier string  `json:"session_identifier"`
	IDChip            string  `json:"id_chip"`
	IDChipName        string  `json:"id_chip_name"`
	Eco               float64 `json:"eco"`
	Nexttrip          int     `json:"nexttrip"`
	Start             string  `json:"start"`
	End               string  `json:"end"`
	StartUnix         int64   `json:"startUnix"`
	EndUnix           int64   `json:"endUnix"`
	SecondsTotal      string  `json:"seconds_total"`
	SecondsCharged    string  `json:"seconds_charged"`
	MaxPower          float64 `json:"max_power"`
	MaxCurrent        float64 `json:"max_current"`
	Energy            float64 `json:"energy"`
	EtoStart          float64 `json:"eto_start"`
	EtoEnd            float64 `json:"eto_end"`
	Link              string  `json:"link"`
}

type wattpilotAPIError struct {
	Success *bool  `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

// ParseJSON takes a JSON document as input and returns a parsed representation of the JSON data.
func ParseJSON(jsonData []byte) (WattpilotData, error) {
	var parsedData WattpilotData
	if err := json.Unmarshal(jsonData, &parsedData); err != nil {
		return parsedData, fmt.Errorf("failed to parse JSON: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return parsedData, fmt.Errorf("failed to inspect JSON payload: %v", err)
	}

	_, hasColumns := raw["columns"]
	_, hasData := raw["data"]
	if hasColumns && hasData {
		return parsedData, nil
	}

	var apiErr wattpilotAPIError
	if err := json.Unmarshal(jsonData, &apiErr); err == nil {
		switch {
		case apiErr.Message != "":
			return parsedData, fmt.Errorf("wattpilot API error: %s", apiErr.Message)
		case apiErr.Error != "":
			return parsedData, fmt.Errorf("wattpilot API error: %s", apiErr.Error)
		case apiErr.Success != nil && !*apiErr.Success:
			return parsedData, fmt.Errorf("wattpilot API returned an unsuccessful response")
		}
	}

	return parsedData, fmt.Errorf("invalid Wattpilot JSON payload: missing columns or data")
}

// FetchJSON fetches a JSON document from the specified URL.
func FetchJSON(ctx context.Context, fetchURL string) ([]byte, error) {
	ctx, span := tracer.Start(ctx, "FetchJSON")
	defer span.End()

	span.SetAttributes(attribute.String("url", fetchURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	response, err := httpClient.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("failed to fetch JSON: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	span.SetAttributes(attribute.Int("http.status_code", response.StatusCode))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("wattpilot API returned status %d", response.StatusCode)
	}

	jsonData, err := io.ReadAll(response.Body)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("failed to read JSON data: %v", err)
	}
	return jsonData, nil
}

func GetJSONData(ctx context.Context) ([]byte, error) {
	cache := activeCache()
	value, err := cache.main.Get(ctx)
	if err != nil && canRefresh() && ctx.Err() == nil {
		value, err = cache.main.RefreshIfStale(ctx, dataTTL())
	}
	if err != nil {
		return nil, err
	}
	if canRefresh() {
		cache.main.Revalidate(dataTTL())
	}
	return slices.Clone(value.Data.json), nil
}

func getMonthlyBackupFilename(yearMonth string) string {
	return dataFilePath(fmt.Sprintf("data-%s_backup.json", yearMonth))
}

// createMonthlyBackups extracts data for each month and saves it to a backup file
func createMonthlyBackups(ctx context.Context, store storage.DataStore, allData indexedData) error {
	for monthKey, entries := range allData.months {
		monthlyBackupData := WattpilotData{
			Columns: allData.data.Columns,
			Data:    entries,
		}

		backupBytes, err := json.Marshal(monthlyBackupData)
		if err != nil {
			return fmt.Errorf("failed to marshal backup data for %s: %v", monthKey, err)
		}

		backupFilename := getMonthlyBackupFilename(monthKey)
		err = store.Write(ctx, backupFilename, backupBytes)
		if err != nil {
			slog.Warn("Failed to create backup", "month", monthKey, "error", err)
			// Don't fail the entire operation if one backup fails
		}
	}

	return nil
}

func PrepUrl(wattpilotDataUrl string, from string, to string, key string) string {
	myUrl, err := url.Parse(wattpilotDataUrl)
	if err != nil {
		// WattpilotDataUrl is a hardcoded constant; a parse failure is a programming error.
		panic(fmt.Sprintf("wattpilotutils: invalid WattpilotDataUrl constant: %v", err))
	}
	values := myUrl.Query()
	if from == "" || to == "" {
		values.Del("from")
		values.Del("to")
	} else {
		values.Set("from", from)
		values.Set(("to"), to)
	}
	values.Set("e", key)
	myUrl.RawQuery = values.Encode()
	return myUrl.String()
}

func GetUnixTimestampStart(yearMonth string) string {
	// year-month into unix timestamp
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		// time/tzdata is embedded; a failure here is a programming error.
		panic(fmt.Sprintf("wattpilotutils: failed to load timezone Europe/Berlin: %v", err))
	}
	t, _ := time.Parse("2006-01", yearMonth)
	return strconv.FormatInt(t.In(loc).Unix()*1000, 10)
}

func GetUnixTimestampEnd(yearMonth string) string {
	// year-month into unix timestamp
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		// time/tzdata is embedded; a failure here is a programming error.
		panic(fmt.Sprintf("wattpilotutils: failed to load timezone Europe/Berlin: %v", err))
	}
	t, _ := time.Parse("2006-01", yearMonth)

	// add one month
	t = t.AddDate(0, 1, 0)
	// subtract one second to get last date of the month
	t = t.Add(-1 * time.Second)
	return strconv.FormatInt(t.In(loc).Unix()*1000, 10)
}

func GetPrevMonth(yearMonth string) string {
	t, _ := time.Parse("2006-01", yearMonth)
	t = t.AddDate(0, -1, 0)
	// only allow dates after 2024-06
	if t.Before(time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)) {
		return ""
	}
	return t.Format("2006-01")
}

func GetNextMonth(yearMonth string) string {
	t, _ := time.Parse("2006-01", yearMonth)
	t = t.AddDate(0, 1, 0)
	// only allow dates before or equal to current month
	if t.After(time.Now()) {
		return ""
	}
	return t.Format("2006-01")
}

func GetStatsForMonth(ctx context.Context, monthToCalculate string) (WattpilotData, error) {
	ctx, span := tracer.Start(ctx, "GetStatsForMonth",
		oteltrace.WithAttributes(attribute.String("month", monthToCalculate)),
	)
	defer span.End()

	data, err := statsForMonths(ctx, []string{monthToCalculate})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return WattpilotData{}, err
	}
	span.SetAttributes(attribute.Int("session.count", len(data[0].Data)))
	return data[0], nil
}

func GetStatsForMonths(ctx context.Context, months []string) ([]WattpilotData, error) {
	ctx, span := tracer.Start(ctx, "GetStatsForMonths",
		oteltrace.WithAttributes(attribute.Int("month.count", len(months))),
	)
	defer span.End()

	if len(months) == 0 {
		return []WattpilotData{}, nil
	}
	requested := make([]string, len(months))
	for i, month := range months {
		requested[i] = strings.TrimSpace(month)
	}
	data, err := statsForMonths(ctx, requested)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return data, err
}

func RoundFloat(val float64, precision uint) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

// GetOfficialPricePerKwhForMonth returns the official price per kWh for
// the given year-month string (format "2006-01"), reading from settings.
func GetOfficialPricePerKwhForMonth(yearMonth string) float64 {
	return settings.GetOfficialPrice(yearMonth)
}

func getSellingPriceOfYear(timestamp string) float64 {
	t, _ := time.Parse("02.01.2006 15:04:05", timestamp)
	return settings.GetOfficialPrice(fmt.Sprintf("%d-%02d", t.Year(), t.Month()))
}

func getPurchasePriceOfYear(timestamp string) float64 {
	t, _ := time.Parse("02.01.2006 15:04:05", timestamp)
	return settings.GetPurchasePrice(fmt.Sprintf("%d-%02d", t.Year(), t.Month()))
}

func CalculatePrice(endTime string, energy float64, eco float64) float64 {
	return energy * getSellingPriceOfYear(endTime)
}

func CalculatePriceMargin(endTime string, energy float64, eco float64) float64 {
	if eco == 100 {
		return energy * getSellingPriceOfYear(endTime)
	} else {
		return energy * (getSellingPriceOfYear(endTime) - getPurchasePriceOfYear(endTime))
	}
}

// GetNetworkFeeMonthly returns the monthly network infrastructure fee in €.
func GetNetworkFeeMonthly() float64 {
	return settings.GetNetworkFeeMonthly()
}

func RefreshData(ctx context.Context) error {
	_, err := activeCache().main.Refresh(ctx)
	return err
}

func refreshSnapshot(ctx context.Context, store storage.DataStore, path string) (snapshot.Value[indexedData], error) {
	ctx, span := tracer.Start(ctx, "RefreshData")
	defer span.End()

	key := os.Getenv("WATTPILOT_KEY")

	// Validate that we have a WATTPILOT_KEY before attempting to fetch
	if key == "" {
		err := fmt.Errorf("WATTPILOT_KEY environment variable is not set - cannot fetch data from API")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return snapshot.Value[indexedData]{}, err
	}

	// Fetch all historical data by using date range from June 2024 to current month
	firstMonth := "2024-06"
	currentMonth := time.Now().Format("2006-01")
	fromTimestamp := GetUnixTimestampStart(firstMonth)
	toTimestamp := GetUnixTimestampEnd(currentMonth)

	myUrl := PrepUrl(WattpilotDataUrl, fromTimestamp, toTimestamp, key)
	slog.InfoContext(ctx, "Fetching Wattpilot data", "fromMonth", firstMonth, "toMonth", currentMonth)

	// Fetch JSON document from the web
	jsonData, err := FetchJSON(ctx, myUrl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return snapshot.Value[indexedData]{}, fmt.Errorf("failed to fetch JSON from API: %w", err)
	}

	// Parse the data to validate it before saving
	parsedData, err := indexData(jsonData)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return snapshot.Value[indexedData]{}, fmt.Errorf("failed to parse fetched JSON: %w", err)
	}

	// Save JSON document to main file
	err = store.Write(ctx, path, jsonData)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return snapshot.Value[indexedData]{}, fmt.Errorf("failed to save JSON file: %w", err)
	}
	updatedAt := time.Now()

	// Create monthly backups for each month in the fetched data
	err = createMonthlyBackups(ctx, store, parsedData)
	if err != nil {
		// Log the error but don't fail - we successfully saved the main file
		slog.WarnContext(ctx, "Failed to create some backups", "error", err)
	} else {
		slog.InfoContext(ctx, "Successfully created monthly backups")
	}

	slog.InfoContext(ctx, "Data refreshed successfully")
	return snapshot.Value[indexedData]{Data: parsedData, UpdatedAt: updatedAt}, nil
}
