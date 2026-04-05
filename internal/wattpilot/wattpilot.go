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
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/jetzlstorfer/wattpilot-exporter/internal/wattpilot")

// refreshMu guards RefreshData to prevent concurrent writes to data files.
var refreshMu sync.Mutex

const OfficialPricePerKwh2024 = 0.33182
const OfficialPricePerKwh2025 = 0.35889 // https://www.bmf.gv.at/themen/steuern/arbeitnehmerinnenveranlagung/pendlerfoerderung-das-pendlerpauschale/sachbezug-kraftfahrzeug.html
const OfficialPricePerKwh2026 = 0.32806
const PurchasePricePerKwh2024 = 0.2824
const PurchasePricePerKwh2025 = 0.25
const PurchasePricePerKwh2026 = 0.25
const JSONFileName = "data/data.json"
const WattpilotDataUrl = "https://data.wattpilot.io/api/v1/direct_json?e=TBD&from=TBD&to=TBD&timezone=Europe%2FVienna"
const DataTTLMinutes = 60  // Auto-refresh data if older than this many minutes
const FetchTimeoutSeconds = 30 // Timeout for outbound API requests

// httpClient is a shared HTTP client with an explicit timeout so that
// requests to the Wattpilot API never hang indefinitely.
var httpClient = &http.Client{
	Timeout: FetchTimeoutSeconds * time.Second,
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
	SecondsTotal      string  `json:"seconds_total"`
	SecondsCharged    string  `json:"seconds_charged"`
	MaxPower          float64 `json:"max_power"`
	MaxCurrent        float64 `json:"max_current"`
	Energy            float64 `json:"energy"`
	EtoStart          float64 `json:"eto_start"`
	EtoEnd            float64 `json:"eto_end"`
	Link              string  `json:"link"`
}

// ParseJSON takes a JSON document as input and returns a parsed representation of the JSON data.
func ParseJSON(jsonData []byte) (WattpilotData, error) {
	var parsedData WattpilotData
	err := json.Unmarshal(jsonData, &parsedData)
	if err != nil {
		return parsedData, fmt.Errorf("failed to parse JSON: %v", err)
	}
	return parsedData, nil
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
	defer response.Body.Close()

	span.SetAttributes(attribute.Int("http.status_code", response.StatusCode))

	jsonData, err := io.ReadAll(response.Body)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("failed to read JSON data: %v", err)
	}
	return jsonData, nil
}

// isDataStale checks if the data/data.json file is older than DataTTLMinutes
func isDataStale() bool {
	fileInfo, err := os.Stat(JSONFileName)
	if err != nil {
		// File doesn't exist or can't be accessed - consider it stale
		return true
	}

	fileAge := time.Since(fileInfo.ModTime())
	ttl := time.Duration(DataTTLMinutes) * time.Minute

	return fileAge > ttl
}

// tryAutoRefresh attempts to refresh data if it's stale and WATTPILOT_KEY is set
// Returns true if a refresh was attempted (regardless of success/failure)
func tryAutoRefresh(ctx context.Context) bool {
	if !isDataStale() {
		return false // Data is fresh, no need to refresh
	}

	key := os.Getenv("WATTPILOT_KEY")
	if key == "" {
		slog.InfoContext(ctx, "Data is stale but WATTPILOT_KEY not set - skipping auto-refresh")
		return false
	}

	slog.InfoContext(ctx, "Data is stale, attempting automatic refresh...")
	err := RefreshData(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Auto-refresh failed, will use cached/backup data", "error", err)
		return true // Attempted but failed
	}

	slog.InfoContext(ctx, "Auto-refresh successful")
	return true // Attempted and succeeded
}

func GetJSONData(ctx context.Context) ([]byte, error) {
	// Try to auto-refresh if data is stale
	tryAutoRefresh(ctx)

	// Read JSON document from file (whether it's fresh or stale)
	jsonData, err := readJSONFile(JSONFileName)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read JSON file", "error", err)
		// Don't auto-fetch here anymore - let the caller decide whether to use backup or fetch
		return nil, err
	}

	return jsonData, nil
}

func readJSONFile(filename string) ([]byte, error) {
	jsonFile, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open JSON file: %v", err)
	}
	defer jsonFile.Close()

	jsonData, err := io.ReadAll(jsonFile)
	if err != nil {
		return jsonData, fmt.Errorf("failed to read JSON data: %v", err)
	}
	return jsonData, nil
}

func saveJSONFile(filename string, jsonData []byte) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %v", filename, err)
	}

	// Write to a temp file in the same directory, then rename atomically
	tmpFile, err := os.CreateTemp(filepath.Dir(filename), ".tmp-wattpilot-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	tmpName := tmpFile.Name()

	_, err = tmpFile.Write(jsonData)
	closeErr := tmpFile.Close()
	if err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to write JSON data: %v", err)
	}
	if closeErr != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to close temp file: %v", closeErr)
	}

	if err := os.Rename(tmpName, filename); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to rename temp file to %s: %v", filename, err)
	}
	return nil
}

func getMonthlyBackupFilename(yearMonth string) string {
	return fmt.Sprintf("data/data-%s_backup.json", yearMonth)
}

// createMonthlyBackups extracts data for each month and saves it to a backup file
func createMonthlyBackups(allData WattpilotData) error {
	// Group data by month
	monthDataMap := make(map[string][]WattpilotEntry)

	for _, entry := range allData.Data {
		month, err := time.Parse("02.01.2006 15:04:05", entry.End)
		if err != nil {
			slog.Warn("Skipping entry with invalid End time", "end", entry.End, "error", err)
			continue
		}
		monthKey := month.Format("2006-01")
		monthDataMap[monthKey] = append(monthDataMap[monthKey], entry)
	}

	// Save backup for each month
	for monthKey, entries := range monthDataMap {
		monthlyBackupData := WattpilotData{
			Columns: allData.Columns,
			Data:    entries,
		}

		backupBytes, err := json.Marshal(monthlyBackupData)
		if err != nil {
			return fmt.Errorf("failed to marshal backup data for %s: %v", monthKey, err)
		}

		backupFilename := getMonthlyBackupFilename(monthKey)
		err = saveJSONFile(backupFilename, backupBytes)
		if err != nil {
			slog.Warn("Failed to create backup", "month", monthKey, "error", err)
			// Don't fail the entire operation if one backup fails
		}
	}

	return nil
}

// tryMonthlyBackup attempts to read data from a monthly backup file
func tryMonthlyBackup(monthYearStr string) ([]byte, error) {
	backupFilename := getMonthlyBackupFilename(monthYearStr)
	return readJSONFile(backupFilename)
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

	// Try to get data from the main JSON file
	slog.InfoContext(ctx, "Loading stats for month", "month", monthToCalculate)
	jsonData, err := GetJSONData(ctx)
	usedMainFile := err == nil

	// If main file doesn't exist or is corrupted, try monthly backup
	if err != nil {
		slog.WarnContext(ctx, "Main data file unavailable, trying backup", "month", monthToCalculate, "error", err)
		backupData, backupErr := tryMonthlyBackup(monthToCalculate)
		if backupErr != nil {
			// Both main and backup failed
			span.RecordError(backupErr)
			span.SetStatus(codes.Error, "main and backup data unavailable")
			return WattpilotData{}, fmt.Errorf("failed to fetch JSON from main file and backup: main=%v, backup=%v", err, backupErr)
		}
		jsonData = backupData
		slog.InfoContext(ctx, "Using backup data for month", "month", monthToCalculate)
	}

	// Parse JSON document
	parsedData, err := ParseJSON(jsonData)
	if err != nil {
		// Main parsing failed, try backup if we were using the main file
		if usedMainFile {
			slog.WarnContext(ctx, "Failed to parse main JSON, trying backup", "error", err)
			backupData, backupErr := tryMonthlyBackup(monthToCalculate)
			if backupErr != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return WattpilotData{}, fmt.Errorf("failed to parse JSON: %v", err)
			}
			parsedData, err = ParseJSON(backupData)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return WattpilotData{}, fmt.Errorf("failed to parse backup JSON: %v", err)
			}
			slog.InfoContext(ctx, "Using backup data for month after parse failure", "month", monthToCalculate)
		} else {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return WattpilotData{}, fmt.Errorf("failed to parse JSON: %v", err)
		}
	}

	// now convert the data to the correct format
	monthlyData := WattpilotData{}
	monthlyData.Columns = parsedData.Columns

	newData := []WattpilotEntry{}

	for _, data := range parsedData.Data {
		// fmt 29.06.2024 21:14:32
		// https://gist.github.com/unstppbl/26942512b3ca6a92857c87124445ca0b
		month, _ := time.Parse("02.01.2006 15:04:05", data.End)
		if month.Format("2006-01") == monthToCalculate {
			newData = append(newData, data)
		}
	}

	monthlyData.Data = newData
	span.SetAttributes(attribute.Int("session.count", len(newData)))
	return monthlyData, nil
}

func GetStatsForMonths(ctx context.Context, months []string) ([]WattpilotData, error) {
	ctx, span := tracer.Start(ctx, "GetStatsForMonths",
		oteltrace.WithAttributes(attribute.Int("month.count", len(months))),
	)
	defer span.End()

	var data []WattpilotData
	for _, month := range months {
		monthData, err := GetStatsForMonth(ctx, strings.TrimSpace(month))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			// Return the data collected so far and the error
			return data, fmt.Errorf("failed to get stats for month %s: %v", month, err)
		}
		data = append(data, monthData)
	}
	return data, nil
}

func RoundFloat(val float64, precision uint) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

// GetOfficialPricePerKwhForMonth returns the fixed official price per kWh for
// the given year-month string (format "2006-01").
func GetOfficialPricePerKwhForMonth(yearMonth string) float64 {
	t, _ := time.Parse("2006-01", yearMonth)
	switch t.Year() {
	case 2024:
		return OfficialPricePerKwh2024
	case 2025:
		return OfficialPricePerKwh2025
	case 2026:
		return OfficialPricePerKwh2026
	default:
		return OfficialPricePerKwh2025
	}
}

func getSellingPriceOfYear(timestamp string) float64 {
	year, _ := time.Parse("02.01.2006 15:04:05", timestamp)
	switch year.Year() {
	case 2024:
		return OfficialPricePerKwh2024
	case 2025:
		return OfficialPricePerKwh2025
	case 2026:
		return OfficialPricePerKwh2026
	default:
		// set default to 2025 until we have no data for other years
		return OfficialPricePerKwh2025
	}
}

func getPurchasePriceOfYear(timestamp string) float64 {
	year, _ := time.Parse("02.01.2006 15:04:05", timestamp)
	switch year.Year() {
	case 2024:
		return PurchasePricePerKwh2024
	case 2025:
		return PurchasePricePerKwh2025
	case 2026:
		return PurchasePricePerKwh2026
	default:
		// set default to 2025 until we have no data for other years
		return PurchasePricePerKwh2025
	}
}

func CalculatePrice(endTime string, energy float64, eco float64) float64 {
	return energy * getSellingPriceOfYear(endTime)
}

func CalculatePriceMargin(endTime string, energy float64, eco float64) float64 {
	if eco == 100 {
		return energy * getSellingPriceOfYear(endTime)
	} else {
		// TODO calculate the correct ratio
		return energy * (getSellingPriceOfYear(endTime) - getPurchasePriceOfYear(endTime))
	}
}

func RefreshData(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "RefreshData")
	defer span.End()

	refreshMu.Lock()
	defer refreshMu.Unlock()

	key := os.Getenv("WATTPILOT_KEY")

	// Validate that we have a WATTPILOT_KEY before attempting to fetch
	if key == "" {
		err := fmt.Errorf("WATTPILOT_KEY environment variable is not set - cannot fetch data from API")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	myUrl := PrepUrl(WattpilotDataUrl, "", "", key)

	// Fetch JSON document from the web
	jsonData, err := FetchJSON(ctx, myUrl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to fetch JSON from API: %v", err)
	}

	// Parse the data to validate it before saving
	parsedData, err := ParseJSON(jsonData)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to parse fetched JSON: %v", err)
	}

	// Save JSON document to main file
	err = saveJSONFile(JSONFileName, jsonData)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to save JSON file: %v", err)
	}

	// Create monthly backups for each month in the fetched data
	err = createMonthlyBackups(parsedData)
	if err != nil {
		// Log the error but don't fail - we successfully saved the main file
		slog.WarnContext(ctx, "Failed to create some backups", "error", err)
	} else {
		slog.InfoContext(ctx, "Successfully created monthly backups")
	}

	slog.InfoContext(ctx, "Data refreshed successfully")
	return nil
}
