package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

const (
	containerName = "settings"
	blobName      = "config.json"
)

// Settings holds all user-configurable application settings.
type Settings struct {
	CarModel          string             `json:"carModel"`
	OfficialPrices    map[string]float64 `json:"officialPrices"`
	PurchasePrices    map[string]float64 `json:"purchasePrices"`
	NetworkFeeMonthly float64            `json:"networkFeeMonthly"`
}

var (
	current *Settings
	mu      sync.RWMutex
)

// Defaults returns settings with the original hardcoded values.
func Defaults() Settings {
	return Settings{
		CarModel: "BMW iX3",
		OfficialPrices: map[string]float64{
			"2024": 0.33182,
			"2025": 0.35889,
			"2026": 0.32806,
		},
		PurchasePrices: map[string]float64{
			"2024": 0.2824,
			"2025": 0.25,
			"2026": 0.25,
		},
		NetworkFeeMonthly: 4.20,
	}
}

// Get returns the current cached settings.
func Get() Settings {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		d := Defaults()
		return d
	}
	return *current
}

func newBlobClient() (*azblob.Client, error) {
	endpoint := os.Getenv("AZURE_STORAGE_ENDPOINT")
	if endpoint == "" {
		return nil, nil
	}
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}
	return azblob.NewClient(endpoint, cred, nil)
}

// Load reads settings from Azure Blob Storage into memory.
// Falls back to defaults if blob doesn't exist or storage is unavailable.
func Load(ctx context.Context) {
	client, err := newBlobClient()
	if err != nil || client == nil {
		if err != nil {
			slog.WarnContext(ctx, "settings: failed to create blob client, using defaults", "error", err)
		} else {
			slog.InfoContext(ctx, "settings: no AZURE_STORAGE_ENDPOINT set, using defaults")
		}
		d := Defaults()
		mu.Lock()
		current = &d
		mu.Unlock()
		return
	}

	resp, err := client.DownloadStream(ctx, containerName, blobName, nil)
	if err != nil {
		slog.WarnContext(ctx, "settings: failed to download config, using defaults", "error", err)
		d := Defaults()
		mu.Lock()
		current = &d
		mu.Unlock()
		return
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.WarnContext(ctx, "settings: failed to read config blob, using defaults", "error", err)
		d := Defaults()
		mu.Lock()
		current = &d
		mu.Unlock()
		return
	}

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		slog.WarnContext(ctx, "settings: failed to parse config, using defaults", "error", err)
		d := Defaults()
		mu.Lock()
		current = &d
		mu.Unlock()
		return
	}

	// Merge with defaults to ensure all keys exist
	d := Defaults()
	if s.CarModel == "" {
		s.CarModel = d.CarModel
	}
	if s.OfficialPrices == nil {
		s.OfficialPrices = d.OfficialPrices
	}
	if s.PurchasePrices == nil {
		s.PurchasePrices = d.PurchasePrices
	}

	mu.Lock()
	current = &s
	mu.Unlock()
	slog.InfoContext(ctx, "settings: loaded from blob storage")
}

// Save writes the given settings to Azure Blob Storage and updates the cache.
func Save(ctx context.Context, s Settings) error {
	client, err := newBlobClient()
	if err != nil {
		return err
	}
	if client == nil {
		slog.WarnContext(ctx, "settings: no storage endpoint, saving to memory only")
		mu.Lock()
		current = &s
		mu.Unlock()
		return nil
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	_, err = client.UploadBuffer(ctx, containerName, blobName, data, &azblob.UploadBufferOptions{})
	if err != nil {
		return err
	}

	mu.Lock()
	current = &s
	mu.Unlock()
	slog.InfoContext(ctx, "settings: saved to blob storage")
	return nil
}

// GetOfficialPrice returns the official price for a given key.
// Accepts "YYYY-MM" or "YYYY". Lookup: exact match → year fallback → defaults.
func GetOfficialPrice(key string) float64 {
	return getPrice(key, func(s Settings) map[string]float64 { return s.OfficialPrices })
}

// GetPurchasePrice returns the purchase price for a given key.
// Accepts "YYYY-MM" or "YYYY". Lookup: exact match → year fallback → defaults.
func GetPurchasePrice(key string) float64 {
	return getPrice(key, func(s Settings) map[string]float64 { return s.PurchasePrices })
}

func getPrice(key string, prices func(Settings) map[string]float64) float64 {
	s := Get()
	m := prices(s)

	// 1. Exact match (works for both "YYYY-MM" and "YYYY" keys)
	if price, ok := m[key]; ok {
		return price
	}

	// 2. If key is "YYYY-MM", try year-only fallback
	if len(key) == 7 && key[4] == '-' {
		yearOnly := key[:4]
		if price, ok := m[yearOnly]; ok {
			return price
		}
	}

	// 3. Fall back to defaults
	d := Defaults()
	dm := prices(d)
	if price, ok := dm[key]; ok {
		return price
	}
	if len(key) == 7 && key[4] == '-' {
		if price, ok := dm[key[:4]]; ok {
			return price
		}
	}
	return dm["2025"]
}

const steirerStromFlexURL = "https://www.tarife.at/energie/anbieter/energie-steiermark/steirerstrom-flex-391"

// FetchSteirerStromFlexPrice fetches the current Arbeitspreis from tarife.at
// and returns it in €/kWh (e.g. 0.1362 for 13,62 ct/kWh).
func FetchSteirerStromFlexPrice() (float64, error) {
	resp, err := http.Get(steirerStromFlexURL)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch tarife.at page: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read tarife.at response: %w", err)
	}

	// Look for Arbeitspreis value in the HTML — price may have whitespace around it
	re := regexp.MustCompile(`Arbeitspreis[\s\S]*?<span[^>]*class="[^"]*align-baseline[^"]*"[^>]*>\s*(\d+,\d+)\s*</span>`)
	matches := re.FindSubmatch(body)
	if len(matches) < 2 {
		return 0, fmt.Errorf("could not find Arbeitspreis on tarife.at page")
	}

	priceStr := strings.Replace(string(matches[1]), ",", ".", 1)
	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse price %q: %w", priceStr, err)
	}

	return price / 100, nil // ct/kWh → €/kWh
}

// GetCarModel returns the configured car model name.
func GetCarModel() string {
	return Get().CarModel
}

// GetNetworkFeeMonthly returns the monthly network infrastructure fee in €.
func GetNetworkFeeMonthly() float64 {
	s := Get()
	if s.NetworkFeeMonthly > 0 {
		return s.NetworkFeeMonthly
	}
	return Defaults().NetworkFeeMonthly
}

// ToJSON returns the settings as a formatted JSON reader (for blob upload).
func (s Settings) ToJSON() (*bytes.Reader, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}
