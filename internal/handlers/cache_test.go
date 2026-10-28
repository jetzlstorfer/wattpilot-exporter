package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/wattpilot"
)

type statusTransport func(*http.Request) (*http.Response, error)

func (fn statusTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestDataStatusRefreshesOpenPage(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
		state   string
		energy  string
	}{
		{"success", `{"columns":[],"data":[{"energy":42,"end":"01.06.2024 12:00:00"}]}`, "idle", "42"},
		{"invalid upstream data", `{"success":false,"message":"invalid key"}`, "failed", "12.5"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("WATTPILOT_DATA_DIR", dir)
			t.Setenv("WATTPILOT_KEY", "test-key")
			path := filepath.Join(dir, "data.json")
			if err := os.WriteFile(path, []byte(`{"columns":[],"data":[{"energy":12.5,"end":"01.06.2024 12:00:00"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-24 * time.Hour)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
			release := make(chan struct{})
			original := http.DefaultTransport
			http.DefaultTransport = statusTransport(func(r *http.Request) (*http.Response, error) {
				select {
				case <-release:
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.payload)), Header: make(http.Header)}, nil
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			wattpilot.InitCache(ctx)
			t.Cleanup(func() {
				cancel()
				http.DefaultTransport = original
			})
			check := func() *httptest.ResponseRecorder {
				recorder := httptest.NewRecorder()
				DataStatusHandler(recorder, httptest.NewRequest(http.MethodGet, "/data-status", nil))
				if recorder.Code != http.StatusNoContent || recorder.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("unexpected status response: %+v", recorder.Result())
				}
				return recorder
			}
			// A tab left open must trigger a refresh through status checks alone.
			initial := check()
			if initial.Header().Get("X-Wattpilot-Refresh-Status") != "refreshing" {
				t.Fatal("stale snapshot did not start refreshing")
			}
			oldVersion := initial.Header().Get("X-Wattpilot-Data-Updated-At")
			close(release)
			deadline := time.Now().Add(3 * time.Second)
			var completed *httptest.ResponseRecorder
			for {
				completed = check()
				if completed.Header().Get("X-Wattpilot-Refresh-Status") != "refreshing" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("refresh did not complete")
				}
				time.Sleep(time.Millisecond)
			}
			if got := completed.Header().Get("X-Wattpilot-Refresh-Status"); got != test.state {
				t.Fatalf("completed refresh state = %q, want %q", got, test.state)
			}
			newVersion := completed.Header().Get("X-Wattpilot-Data-Updated-At")
			if (newVersion != oldVersion) != (test.state == "idle") {
				t.Fatal("snapshot version did not reflect refresh success/failure")
			}
			page := httptest.NewRecorder()
			DashboardHandler(filepath.Join("..", "..", "templates")).ServeHTTP(page,
				httptest.NewRequest(http.MethodGet, "/?date=2024-06", nil))
			if !strings.Contains(page.Body.String(), `data-updated-at="`+newVersion+`"`) ||
				!strings.Contains(page.Body.String(), test.energy) {
				t.Fatal("reloaded dashboard does not render the expected snapshot")
			}
		})
	}
}

func TestDataRoutesReuseSnapshotAndExposeFreshness(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WATTPILOT_DATA_DIR", dir)
	t.Setenv("WATTPILOT_KEY", "")
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte(`{"columns":[{"key":"energy","caption":"Energy"}],
		"data":[{"energy":12.5,"end":"01.06.2024 12:00:00"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	wattpilot.InitCache(ctx)
	t.Cleanup(cancel)
	templateDir := filepath.Join("..", "..", "templates")
	for _, test := range []struct {
		name    string
		url     string
		handler http.HandlerFunc
	}{
		{"status", "/data-status", DataStatusHandler},
		{"dashboard", "/?date=2024-06", DashboardHandler(templateDir)},
		{"charts", "/charts", ChartHandler(templateDir)},
		{"download", "/download?date=2024-06", DownloadHandler},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			test.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.url, nil))
			wantStatus := http.StatusOK
			if test.name == "status" {
				wantStatus = http.StatusNoContent
			}
			if recorder.Code != wantStatus {
				t.Fatalf("status=%d, body=%s", recorder.Code, recorder.Body.String())
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("data response can be cached without checking freshness")
			}
			if state := recorder.Header().Get("X-Wattpilot-Refresh-Status"); state != "idle" {
				t.Fatalf("refresh status = %q, want idle", state)
			}
			if _, err := time.Parse(time.RFC3339, recorder.Header().Get("X-Wattpilot-Data-Updated-At")); err != nil {
				t.Fatalf("missing or invalid snapshot timestamp: %v", err)
			}
			if test.name == "status" {
				if recorder.Body.Len() != 0 {
					t.Fatal("status endpoint returned a body")
				}
			} else if test.name == "download" {
				if !strings.HasPrefix(recorder.Body.String(), "PK") {
					t.Fatal("download is not an XLSX archive")
				}
			} else if !strings.Contains(recorder.Body.String(), "12.5") {
				t.Fatal("cached charging energy is missing from page")
			}
			if test.name == "dashboard" || test.name == "charts" {
				if !strings.Contains(recorder.Body.String(), `<meta id="data-freshness" data-updated-at="`+recorder.Header().Get("X-Wattpilot-Data-Updated-At")+`">`) ||
					!strings.Contains(recorder.Body.String(), `/static/data-freshness.js`) {
					t.Fatal("page does not track the rendered snapshot for automatic updates")
				}
				if strings.Contains(recorder.Body.String(), `role="status"`) ||
					strings.Contains(recorder.Body.String(), "Checking data freshness...") {
					t.Fatal("automatic updates must not add visible freshness status UI")
				}
			}
			if test.name == "dashboard" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestInvalidDashboardDateDoesNotAutoReload(t *testing.T) {
	page := httptest.NewRecorder()
	DashboardHandler(filepath.Join("..", "..", "templates")).ServeHTTP(page,
		httptest.NewRequest(http.MethodGet, "/?date=invalid", nil))
	if !strings.Contains(page.Body.String(), "The requested month is invalid.") {
		t.Fatal("invalid date error is missing")
	}
	if strings.Contains(page.Body.String(), `id="data-freshness"`) {
		t.Fatal("invalid date page could enter a reload loop when charging data exists")
	}
}
