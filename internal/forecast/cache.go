package forecast

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/settings"
	"github.com/jetzlstorfer/wattpilot-exporter/internal/snapshot"
	"github.com/jetzlstorfer/wattpilot-exporter/internal/storage"
)

var forecastCache struct {
	sync.Mutex
	ctx   context.Context
	path  string
	cache *snapshot.Cache[[]byte]
}

func InitCache(ctx context.Context) {
	forecastCache.Lock()
	defer forecastCache.Unlock()
	forecastCache.ctx = ctx
	forecastCache.path = ""
	forecastCache.cache = nil
}

func activeForecastCache(lat, lon float64) *snapshot.Cache[[]byte] {
	path := cacheFilePath(lat, lon)
	forecastCache.Lock()
	defer forecastCache.Unlock()
	if forecastCache.cache != nil && forecastCache.path == path {
		return forecastCache.cache
	}
	ctx := forecastCache.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	forecastCache.path = path
	forecastCache.cache = newForecastCache(ctx, storage.Store(), path, lat, lon)
	return forecastCache.cache
}

func newForecastCache(ctx context.Context, store storage.DataStore, path string, lat, lon float64) *snapshot.Cache[[]byte] {
	return snapshot.New(ctx, "weather forecast", 15*time.Second,
		func(ctx context.Context) (snapshot.Value[[]byte], error) {
			payload, err := store.Read(ctx, path)
			if err != nil {
				return snapshot.Value[[]byte]{}, err
			}
			if err := validateForecastPayload(payload); err != nil {
				return snapshot.Value[[]byte]{}, err
			}
			modified, err := store.ModTime(ctx, path)
			if err != nil {
				slog.WarnContext(ctx, "Unable to determine forecast age; treating it as stale", "error", err)
			}
			return snapshot.Value[[]byte]{Data: payload, UpdatedAt: modified}, nil
		},
		func(ctx context.Context) (snapshot.Value[[]byte], error) {
			payload, err := fetchForecast(ctx, lat, lon)
			if err != nil {
				return snapshot.Value[[]byte]{}, err
			}
			if err := store.Write(ctx, path, payload); err != nil {
				return snapshot.Value[[]byte]{}, fmt.Errorf("failed to persist forecast: %w", err)
			}
			return snapshot.Value[[]byte]{Data: payload, UpdatedAt: time.Now()}, nil
		})
}

func validateForecastPayload(payload []byte) error {
	var forecast openMeteoForecast
	if err := json.Unmarshal(payload, &forecast); err != nil {
		return fmt.Errorf("failed to parse forecast: %w", err)
	}
	n := len(forecast.Hourly.Time)
	if n == 0 || len(forecast.Hourly.Temperature2M) != n || len(forecast.Hourly.CloudCover) != n ||
		len(forecast.Hourly.PrecipitationProbability) != n || len(forecast.Hourly.ShortwaveRadiation) != n ||
		len(forecast.Hourly.WeatherCode) != n {
		return fmt.Errorf("forecast contains missing or inconsistent hourly data")
	}
	return nil
}

func loadForecastPayload(ctx context.Context, lat, lon float64) ([]byte, time.Time, error) {
	cache := activeForecastCache(lat, lon)
	value, err := cache.Get(ctx)
	ttl := time.Duration(settings.GetForecastTTLMinutes()) * time.Minute
	if err != nil && ctx.Err() == nil {
		value, err = cache.RefreshIfStale(ctx, ttl)
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	cache.Revalidate(ttl)
	return value.Data, value.UpdatedAt, nil
}
