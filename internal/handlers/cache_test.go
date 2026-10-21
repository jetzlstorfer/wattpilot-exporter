package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/wattpilot"
)

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
		{"dashboard", "/?date=2024-06", DashboardHandler(templateDir)},
		{"charts", "/charts", ChartHandler(templateDir)},
		{"download", "/download?date=2024-06", DownloadHandler},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			test.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.url, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d, body=%s", recorder.Code, recorder.Body.String())
			}
			if state := recorder.Header().Get("X-Wattpilot-Refresh-Status"); state != "idle" {
				t.Fatalf("refresh status = %q, want idle", state)
			}
			if _, err := time.Parse(time.RFC3339, recorder.Header().Get("X-Wattpilot-Data-Updated-At")); err != nil {
				t.Fatalf("missing or invalid snapshot timestamp: %v", err)
			}
			if test.name == "download" {
				if !strings.HasPrefix(recorder.Body.String(), "PK") {
					t.Fatal("download is not an XLSX archive")
				}
			} else if !strings.Contains(recorder.Body.String(), "12.5") {
				t.Fatal("cached charging energy is missing from page")
			}
			if test.name == "dashboard" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
