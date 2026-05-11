package settings

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

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
