package settings

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/storage"
)

func TestLoadOfficialPrice2027(t *testing.T) {
	original := current
	t.Cleanup(func() {
		mu.Lock()
		current = original
		mu.Unlock()
	})
	t.Chdir(t.TempDir())

	tests := []struct {
		name   string
		prices map[string]float64
		want   float64
	}{
		{"no saved prices", nil, 0.327},
		{"existing configuration", map[string]float64{"2026": 0.4}, 0.327},
		{"month override", map[string]float64{"2027-06": 0.45}, 0.327},
		{"year override", map[string]float64{"2027": 0.42}, 0.42},
		{"zero override", map[string]float64{"2027": 0}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Defaults()
			s.OfficialPrices = tt.prices
			data, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if err := (storage.LocalStore{}).Write(context.Background(), settingsPath, data); err != nil {
				t.Fatal(err)
			}
			Load(context.Background())
			if got, ok := Get().OfficialPrices["2027"]; !ok || got != tt.want {
				t.Errorf("loaded 2027 rate = %v (present=%v), want %v", got, ok, tt.want)
			}
			if got := GetOfficialPrice("2027-01"); got != tt.want {
				t.Errorf("GetOfficialPrice(2027-01) = %v, want %v", got, tt.want)
			}
			for key, want := range tt.prices {
				if got := GetOfficialPrice(key); got != want {
					t.Errorf("saved override %s = %v, want %v", key, got, want)
				}
			}
		})
	}
}

func TestGetOfficialPrice2027Overrides(t *testing.T) {
	original := current
	t.Cleanup(func() {
		mu.Lock()
		current = original
		mu.Unlock()
	})
	s := Defaults()
	s.OfficialPrices = map[string]float64{"2027-06": 0.45}
	mu.Lock()
	current = &s
	mu.Unlock()

	for _, tt := range []struct {
		key  string
		want float64
	}{
		{"2027", 0.327},
		{"2027-01", 0.327},
		{"2027-06", 0.45},
		{"2027-12", 0.327},
		{"2026-12", 0.32806},
	} {
		if got := GetOfficialPrice(tt.key); got != tt.want {
			t.Errorf("GetOfficialPrice(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGeocodeAddress(t *testing.T) {
	originalClient := settingsHTTPClient
	t.Cleanup(func() { settingsHTTPClient = originalClient })

	settingsHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.String(), "name=Herrengasse") {
			t.Fatalf("unexpected geocoding URL: %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{
				"results": [
					{
						"name": "Herrengasse 1",
						"latitude": 47.0707,
						"longitude": 15.4395,
						"country": "Austria",
						"admin1": "Styria"
					}
				]
			}`)),
			Header: make(http.Header),
		}, nil
	})}

	lat, lon, label, err := GeocodeAddress(context.Background(), "Herrengasse 1, Graz")
	if err != nil {
		t.Fatalf("GeocodeAddress() error = %v", err)
	}
	if lat != 47.0707 || lon != 15.4395 {
		t.Fatalf("GeocodeAddress() = (%v, %v), want (47.0707, 15.4395)", lat, lon)
	}
	if label != "Herrengasse 1, Styria, Austria" {
		t.Fatalf("GeocodeAddress() label = %q, want %q", label, "Herrengasse 1, Styria, Austria")
	}
}

func TestGeocodeAddressFallsBackToNominatim(t *testing.T) {
	originalClient := settingsHTTPClient
	t.Cleanup(func() { settingsHTTPClient = originalClient })

	settingsHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Host, "geocoding-api.open-meteo.com"):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"results":[]}`)),
				Header:     make(http.Header),
			}, nil
		case strings.Contains(req.URL.Host, "nominatim.openstreetmap.org"):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`[
					{
						"lat":"48.3620709",
						"lon":"14.5217050",
						"display_name":"21, Althannstraße, Hagenberg im Mühlkreis, Bezirk Freistadt, Oberösterreich, 4232, Österreich"
					}
				]`)),
				Header: make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected geocoding host: %s", req.URL.Host)
			return nil, nil
		}
	})}

	lat, lon, label, err := GeocodeAddress(context.Background(), "Althannstrasse 21, Hagenberg, Austria")
	if err != nil {
		t.Fatalf("GeocodeAddress() error = %v", err)
	}
	if lat != 48.3620709 || lon != 14.5217050 {
		t.Fatalf("GeocodeAddress() = (%v, %v), want (48.3620709, 14.5217050)", lat, lon)
	}
	if !strings.Contains(label, "Althannstraße") {
		t.Fatalf("GeocodeAddress() label = %q, expected street name from fallback", label)
	}
}
