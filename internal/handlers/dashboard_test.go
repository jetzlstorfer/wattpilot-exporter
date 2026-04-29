package handlers

import (
	"testing"
	"time"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/wattpilot"
)

func TestIsActiveCharging(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}

	now := time.Date(2026, 4, 29, 12, 0, 0, 0, loc)
	currentMonth := now.Format("2006-01")
	prevMonth := now.AddDate(0, -1, 0).Format("2006-01")

	tests := []struct {
		name             string
		monthToCalculate string
		latestSessionEnd time.Time
		want             bool
	}{
		{
			name:             "active when recent in current month",
			monthToCalculate: currentMonth,
			latestSessionEnd: time.Date(2026, 4, 29, 11, 55, 0, 0, loc),
			want:             true,
		},
		{
			name:             "inactive when stale in current month",
			monthToCalculate: currentMonth,
			latestSessionEnd: time.Date(2026, 4, 29, 11, 40, 0, 0, loc),
			want:             false,
		},
		{
			name:             "inactive for historical month",
			monthToCalculate: prevMonth,
			latestSessionEnd: time.Date(2026, 3, 29, 11, 55, 0, 0, loc),
			want:             false,
		},
		{
			name:             "inactive when timestamp missing",
			monthToCalculate: currentMonth,
			latestSessionEnd: time.Time{},
			want:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isActiveCharging(tt.monthToCalculate, tt.latestSessionEnd, now, loc)
			if got != tt.want {
				t.Fatalf("isActiveCharging(...) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionEndTime(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}

	t.Run("uses EndUnix when available", func(t *testing.T) {
		expected := time.Date(2026, 4, 29, 8, 48, 57, 0, loc)
		entry := wattpilot.WattpilotEntry{
			End:     "29.04.2026 06:48:57",
			EndUnix: expected.UTC().UnixMilli(),
		}

		got := sessionEndTime(entry, loc)
		if !got.Equal(expected) {
			t.Fatalf("sessionEndTime() = %v, want %v", got, expected)
		}
	})

	t.Run("falls back to End string", func(t *testing.T) {
		expected := time.Date(2026, 4, 29, 6, 48, 57, 0, loc)
		entry := wattpilot.WattpilotEntry{End: "29.04.2026 06:48:57"}

		got := sessionEndTime(entry, loc)
		if !got.Equal(expected) {
			t.Fatalf("sessionEndTime() = %v, want %v", got, expected)
		}
	})
}
