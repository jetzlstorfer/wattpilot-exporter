package handlers

import (
	"net/http"
	"time"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/wattpilot"
)

func snapshotTimestamp(updatedAt time.Time) string {
	if updatedAt.IsZero() {
		return ""
	}
	return updatedAt.UTC().Format(time.RFC3339Nano)
}

func setDataCacheHeaders(w http.ResponseWriter, updatedAt string) {
	status := wattpilot.DataCacheStatus()
	w.Header().Set("Cache-Control", "no-store")
	if updatedAt != "" {
		w.Header().Set("X-Wattpilot-Data-Updated-At", updatedAt)
	}
	state := "idle"
	if status.Err != nil {
		state = "failed"
	}
	if status.Refreshing {
		state = "refreshing"
	}
	w.Header().Set("X-Wattpilot-Refresh-Status", state)
}

// DataStatusHandler rechecks age without rendering a page or returning charging data.
func DataStatusHandler(w http.ResponseWriter, r *http.Request) {
	status := wattpilot.CheckDataFreshness(r.Context())
	setDataCacheHeaders(w, snapshotTimestamp(status.UpdatedAt))
	w.WriteHeader(http.StatusNoContent)
}
