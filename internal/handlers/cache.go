package handlers

import (
	"net/http"
	"time"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/wattpilot"
)

func setDataCacheHeaders(w http.ResponseWriter) {
	status := wattpilot.DataCacheStatus()
	if !status.UpdatedAt.IsZero() {
		w.Header().Set("X-Wattpilot-Data-Updated-At", status.UpdatedAt.UTC().Format(time.RFC3339))
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
