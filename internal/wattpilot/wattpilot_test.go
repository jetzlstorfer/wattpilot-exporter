package wattpilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientHasTimeout(t *testing.T) {
	if httpClient.Timeout <= 0 {
		t.Errorf("httpClient.Timeout must be > 0, got %v", httpClient.Timeout)
	}
}

func TestFetchJSON_TimesOut(t *testing.T) {
	// Create a test server that never responds.
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the test context is cancelled.
		<-r.Context().Done()
	}))
	defer slowServer.Close()

	testTimeout := 100 * time.Millisecond
	original := httpClient
	httpClient = &http.Client{Timeout: testTimeout}
	defer func() { httpClient = original }()

	start := time.Now()
	_, err := FetchJSON(context.Background(), slowServer.URL)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("FetchJSON should return an error when the server does not respond")
	}
	// Allow generous headroom (10x the configured timeout) to account for CI variability,
	// while still catching a true hang.
	if elapsed > 10*testTimeout {
		t.Errorf("FetchJSON hung for %v; expected it to time out within ~%v", elapsed, testTimeout)
	}
}


func TestRoundFloat(t *testing.T) {
	tests := []struct {
		val       float64
		precision uint
		want      float64
	}{
		{1.234567, 2, 1.23},
		{1.235, 2, 1.24},
		{1.0, 0, 1.0},
		{0.0, 2, 0.0},
		{99.999, 1, 100.0},
		{-1.555, 2, -1.56},
	}
	for _, tt := range tests {
		got := RoundFloat(tt.val, tt.precision)
		if got != tt.want {
			t.Errorf("RoundFloat(%v, %d) = %v, want %v", tt.val, tt.precision, got, tt.want)
		}
	}
}

func TestGetPrevMonth(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2024-07", "2024-06"},
		{"2024-06", ""},        // lower bound
		{"2025-01", "2024-12"},
		{"2024-05", ""},        // before data start
	}
	for _, tt := range tests {
		got := GetPrevMonth(tt.input)
		if got != tt.want {
			t.Errorf("GetPrevMonth(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGetNextMonth(t *testing.T) {
	// GetNextMonth returns "" if result is after current month,
	// so we test with historical months that are always valid.
	tests := []struct {
		input string
		want  string
	}{
		{"2024-06", "2024-07"},
		{"2024-12", "2025-01"},
		{"2025-06", "2025-07"},
	}
	for _, tt := range tests {
		got := GetNextMonth(tt.input)
		if got != tt.want {
			t.Errorf("GetNextMonth(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGetOfficialPricePerKwhForMonth(t *testing.T) {
	tests := []struct {
		yearMonth string
		want      float64
	}{
		{"2024-01", OfficialPricePerKwh2024},
		{"2024-12", OfficialPricePerKwh2024},
		{"2025-06", OfficialPricePerKwh2025},
		{"2026-03", OfficialPricePerKwh2026},
		{"2099-01", OfficialPricePerKwh2025}, // unknown year defaults to 2025
	}
	for _, tt := range tests {
		got := GetOfficialPricePerKwhForMonth(tt.yearMonth)
		if got != tt.want {
			t.Errorf("GetOfficialPricePerKwhForMonth(%q) = %v, want %v", tt.yearMonth, got, tt.want)
		}
	}
}

func TestCalculatePrice(t *testing.T) {
	tests := []struct {
		endTime string
		energy  float64
		eco     float64
		want    float64
	}{
		{"15.06.2024 10:00:00", 10.0, 100, 10.0 * OfficialPricePerKwh2024},
		{"15.01.2025 10:00:00", 5.0, 50, 5.0 * OfficialPricePerKwh2025},
		{"15.03.2026 10:00:00", 20.0, 0, 20.0 * OfficialPricePerKwh2026},
	}
	for _, tt := range tests {
		got := CalculatePrice(tt.endTime, tt.energy, tt.eco)
		if RoundFloat(got, 5) != RoundFloat(tt.want, 5) {
			t.Errorf("CalculatePrice(%q, %v, %v) = %v, want %v", tt.endTime, tt.energy, tt.eco, got, tt.want)
		}
	}
}

func TestCalculatePriceMargin(t *testing.T) {
	// eco == 100: margin = energy * sellingPrice
	got := CalculatePriceMargin("15.06.2024 10:00:00", 10.0, 100)
	want := 10.0 * OfficialPricePerKwh2024
	if RoundFloat(got, 5) != RoundFloat(want, 5) {
		t.Errorf("CalculatePriceMargin(eco=100) = %v, want %v", got, want)
	}

	// eco != 100: margin = energy * (sellingPrice - purchasePrice)
	got = CalculatePriceMargin("15.06.2024 10:00:00", 10.0, 50)
	want = 10.0 * (OfficialPricePerKwh2024 - PurchasePricePerKwh2024)
	if RoundFloat(got, 5) != RoundFloat(want, 5) {
		t.Errorf("CalculatePriceMargin(eco=50) = %v, want %v", got, want)
	}
}

func TestPrepUrl(t *testing.T) {
	result := PrepUrl(WattpilotDataUrl, "1000", "2000", "mykey")
	if result == "" {
		t.Fatal("PrepUrl returned empty string")
	}
	// Should contain the key
	if !contains(result, "e=mykey") {
		t.Errorf("PrepUrl result should contain e=mykey, got %s", result)
	}
	// Should contain from and to
	if !contains(result, "from=1000") {
		t.Errorf("PrepUrl result should contain from=1000, got %s", result)
	}
	if !contains(result, "to=2000") {
		t.Errorf("PrepUrl result should contain to=2000, got %s", result)
	}

	// Empty from/to should remove them
	result2 := PrepUrl(WattpilotDataUrl, "", "", "mykey")
	if contains(result2, "from=") {
		t.Errorf("PrepUrl with empty from should not contain from=, got %s", result2)
	}
}

func TestGetUnixTimestampStart(t *testing.T) {
	result := GetUnixTimestampStart("2024-06")
	if result == "" {
		t.Fatal("GetUnixTimestampStart returned empty string")
	}
	// Should be a numeric string
	for _, c := range result {
		if c < '0' || c > '9' {
			t.Fatalf("GetUnixTimestampStart returned non-numeric: %s", result)
		}
	}
}

func TestGetUnixTimestampEnd(t *testing.T) {
	start := GetUnixTimestampStart("2024-06")
	end := GetUnixTimestampEnd("2024-06")
	if end <= start {
		t.Errorf("GetUnixTimestampEnd(%q) = %s should be > start = %s", "2024-06", end, start)
	}
}

func TestParseJSON(t *testing.T) {
	jsonData := []byte(`{
		"columns": [{"key": "energy", "caption": "Energy"}],
		"data": [{"session_number": 1, "energy": 12.5, "start": "01.06.2024 10:00:00", "end": "01.06.2024 12:00:00"}]
	}`)

	parsed, err := ParseJSON(jsonData)
	if err != nil {
		t.Fatalf("ParseJSON failed: %v", err)
	}
	if len(parsed.Columns) != 1 {
		t.Errorf("expected 1 column, got %d", len(parsed.Columns))
	}
	if len(parsed.Data) != 1 {
		t.Errorf("expected 1 data entry, got %d", len(parsed.Data))
	}
	if parsed.Data[0].Energy != 12.5 {
		t.Errorf("expected energy 12.5, got %v", parsed.Data[0].Energy)
	}
}

func TestParseJSON_Invalid(t *testing.T) {
	_, err := ParseJSON([]byte(`{invalid json`))
	if err == nil {
		t.Error("ParseJSON should fail on invalid JSON")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
