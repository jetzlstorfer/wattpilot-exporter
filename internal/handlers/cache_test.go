package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/wattpilot"
	"github.com/xuri/excelize/v2"
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
				workbook, err := excelize.OpenReader(bytes.NewReader(recorder.Body.Bytes()))
				if err != nil {
					t.Fatalf("open downloaded workbook: %v", err)
				}
				t.Cleanup(func() {
					if err := workbook.Close(); err != nil {
						t.Errorf("close downloaded workbook: %v", err)
					}
				})
				for _, cell := range []struct {
					name string
					want string
				}{
					{"A1", "Energy"},
					{"A2", "12.5"},
					{"A4", "Total kWh"},
				} {
					if got, err := workbook.GetCellValue("Sheet1", cell.name); err != nil || got != cell.want {
						t.Errorf("cell %s = %q, %v; want %q", cell.name, got, err, cell.want)
					}
				}
				if got, err := workbook.GetCellFormula("Sheet1", "B4"); err != nil || got != "SUM(A2:A2)" {
					t.Errorf("energy formula = %q, %v; want SUM(A2:A2)", got, err)
				}
				if got, err := workbook.CalcCellValue("Sheet1", "B4"); err != nil || got != "12.5" {
					t.Errorf("total energy = %q, %v; want 12.5", got, err)
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
