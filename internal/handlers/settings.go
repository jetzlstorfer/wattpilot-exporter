package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/settings"
)

// PriceEntry is a key+price pair for template rendering.
// Month holds "YYYY-MM" for monthly entries or "YYYY" for year-level fallbacks.
type PriceEntry struct {
	Month string
	Price float64
}

// SettingsData is the template context for the settings page.
type SettingsData struct {
	CarModel          string
	OfficialPrices    []PriceEntry
	PurchasePrices    []PriceEntry
	NetworkFeeMonthly float64
	Success           bool
	Error             string
}

func sortedPriceEntries(prices map[string]float64) []PriceEntry {
	entries := make([]PriceEntry, 0, len(prices))
	for key, price := range prices {
		entries = append(entries, PriceEntry{Month: key, Price: price})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Month < entries[j].Month })
	return entries
}

// parseDynamicPrices reads indexed form fields (e.g. official_month_0, official_price_0)
// and returns the collected price map.
func parseDynamicPrices(r *http.Request, prefix string) map[string]float64 {
	prices := make(map[string]float64)
	for i := 0; i < 200; i++ {
		month := r.FormValue(fmt.Sprintf("%s_month_%d", prefix, i))
		priceStr := r.FormValue(fmt.Sprintf("%s_price_%d", prefix, i))
		if month == "" || priceStr == "" {
			continue
		}
		if price, err := strconv.ParseFloat(priceStr, 64); err == nil {
			prices[month] = price
		}
	}
	return prices
}

// SettingsHandler handles GET and POST /settings.
func SettingsHandler(templateDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				renderSettings(w, templateDir, SettingsData{Error: "Failed to parse form"})
				return
			}

			s := settings.Get()

			// Update car model
			if carModel := r.FormValue("carModel"); carModel != "" {
				s.CarModel = carModel
			}

			// Update prices from dynamic rows
			if op := parseDynamicPrices(r, "official"); len(op) > 0 {
				s.OfficialPrices = op
			}
			if pp := parseDynamicPrices(r, "purchase"); len(pp) > 0 {
				s.PurchasePrices = pp
			}

			// Update network fee
			if nf := r.FormValue("networkFee"); nf != "" {
				if fee, err := strconv.ParseFloat(nf, 64); err == nil {
					s.NetworkFeeMonthly = fee
				}
			}

			if err := settings.Save(ctx, s); err != nil {
				slog.ErrorContext(ctx, "settingsHandler: failed to save settings", "error", err)
				renderSettings(w, templateDir, SettingsData{
					CarModel:          s.CarModel,
					OfficialPrices:    sortedPriceEntries(s.OfficialPrices),
					PurchasePrices:    sortedPriceEntries(s.PurchasePrices),
					NetworkFeeMonthly: s.NetworkFeeMonthly,
					Error:             "Failed to save settings: " + err.Error(),
				})
				return
			}

			// Reload settings and show success
			current := settings.Get()
			renderSettings(w, templateDir, SettingsData{
				CarModel:          current.CarModel,
				OfficialPrices:    sortedPriceEntries(current.OfficialPrices),
				PurchasePrices:    sortedPriceEntries(current.PurchasePrices),
				NetworkFeeMonthly: current.NetworkFeeMonthly,
				Success:        true,
			})
			return
		}

		// GET: show current settings
		current := settings.Get()
		renderSettings(w, templateDir, SettingsData{
			CarModel:          current.CarModel,
			OfficialPrices:    sortedPriceEntries(current.OfficialPrices),
			PurchasePrices:    sortedPriceEntries(current.PurchasePrices),
			NetworkFeeMonthly: current.NetworkFeeMonthly,
		})
	}
}

// FetchPriceHandler fetches the current SteirerStrom Flex price, adds it as
// a monthly entry for the current month, saves, and returns the result as JSON.
func FetchPriceHandler(w http.ResponseWriter, r *http.Request) {
	price, err := settings.FetchSteirerStromFlexPrice()
	if err != nil {
		slog.Error("fetchPriceHandler: failed to fetch price", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Add the fetched price as a monthly entry and persist
	month := time.Now().Format("2006-01")
	s := settings.Get()
	if s.PurchasePrices == nil {
		s.PurchasePrices = make(map[string]float64)
	}
	s.PurchasePrices[month] = price
	if err := settings.Save(r.Context(), s); err != nil {
		slog.Error("fetchPriceHandler: failed to save settings", "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"price": price, "month": month})
}

func renderSettings(w http.ResponseWriter, templateDir string, data SettingsData) {
	tmpl, err := template.ParseFiles(filepath.Join(templateDir, "settings.html"))
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		slog.Error("settingsHandler: template parse error", "error", err)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		slog.Error("settingsHandler: template execute error", "error", err)
	}
}
