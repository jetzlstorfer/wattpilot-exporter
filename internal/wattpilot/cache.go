package wattpilot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/jetzlstorfer/wattpilot-exporter/internal/settings"
	"github.com/jetzlstorfer/wattpilot-exporter/internal/snapshot"
	"github.com/jetzlstorfer/wattpilot-exporter/internal/storage"
)

const cacheTimeout = 30 * time.Second

type indexedData struct {
	json   []byte
	data   WattpilotData
	months map[string][]WattpilotEntry
}

type dataCache struct {
	main    *snapshot.Cache[indexedData]
	mu      sync.Mutex
	backups map[string]*snapshot.Cache[indexedData]
	ctx     context.Context
	store   storage.DataStore
}

var (
	cacheMu    sync.Mutex
	cachedData *dataCache
)

// InitCache binds background refreshes to server shutdown. It runs before serving requests.
func InitCache(ctx context.Context) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cachedData = newDataCache(ctx, storage.Store())
}

func activeCache() *dataCache {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cachedData == nil {
		cachedData = newDataCache(context.Background(), storage.Store())
	}
	return cachedData
}

func newDataCache(ctx context.Context, store storage.DataStore) *dataCache {
	cache := &dataCache{ctx: ctx, store: store, backups: make(map[string]*snapshot.Cache[indexedData])}
	path := dataFilePath(JSONFileName)
	cache.main = snapshot.New(ctx, "charging data", cacheTimeout,
		func(ctx context.Context) (snapshot.Value[indexedData], error) {
			return loadSnapshot(ctx, store, path)
		},
		func(ctx context.Context) (snapshot.Value[indexedData], error) {
			return refreshSnapshot(ctx, store, path)
		})
	return cache
}

func loadSnapshot(ctx context.Context, store storage.DataStore, path string) (snapshot.Value[indexedData], error) {
	raw, err := store.Read(ctx, path)
	if err != nil {
		return snapshot.Value[indexedData]{}, err
	}
	data, err := indexData(raw)
	if err != nil {
		return snapshot.Value[indexedData]{}, err
	}
	modified, err := store.ModTime(ctx, path)
	if err != nil {
		slog.WarnContext(ctx, "Unable to determine snapshot age; treating it as stale", "path", path, "error", err)
	}
	return snapshot.Value[indexedData]{Data: data, UpdatedAt: modified}, nil
}

func indexData(raw []byte) (indexedData, error) {
	parsed, err := ParseJSON(raw)
	if err != nil {
		return indexedData{}, err
	}
	months := make(map[string][]WattpilotEntry)
	for _, entry := range parsed.Data {
		end, err := time.Parse("02.01.2006 15:04:05", entry.End)
		if err != nil {
			slog.Warn("Skipping entry with invalid End time", "end", entry.End, "error", err)
			continue
		}
		month := end.Format("2006-01")
		months[month] = append(months[month], entry)
	}
	return indexedData{json: raw, data: parsed, months: months}, nil
}

func (c *dataCache) backup(month string) *snapshot.Cache[indexedData] {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.backups[month] == nil {
		path := getMonthlyBackupFilename(month)
		c.backups[month] = snapshot.New(c.ctx, "charging backup "+month, cacheTimeout,
			func(ctx context.Context) (snapshot.Value[indexedData], error) {
				return loadSnapshot(ctx, c.store, path)
			}, nil)
	}
	return c.backups[month]
}

func dataTTL() time.Duration {
	return time.Duration(settings.GetDataTTLMinutes()) * time.Minute
}

func canRefresh() bool {
	return os.Getenv("WATTPILOT_KEY") != ""
}

func monthlyData(data indexedData, month string) WattpilotData {
	return WattpilotData{
		Columns: slices.Clone(data.data.Columns),
		Data:    slices.Clone(data.months[month]),
	}
}

// DataCacheStatus reports process-local charging data freshness without reading storage.
func DataCacheStatus() snapshot.Status {
	return activeCache().main.Status()
}

func statsForMonths(ctx context.Context, months []string) ([]WattpilotData, error) {
	for _, month := range months {
		if _, err := time.Parse("2006-01", month); err != nil {
			return nil, fmt.Errorf("invalid charging month %q: %w", month, err)
		}
	}
	cache := activeCache()
	main, mainErr := cache.main.Get(ctx)
	if mainErr == nil {
		if canRefresh() {
			cache.main.Revalidate(dataTTL())
		}
		result := make([]WattpilotData, 0, len(months))
		for _, month := range months {
			result = append(result, monthlyData(main.Data, month))
		}
		return result, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := make([]WattpilotData, 0, len(months))
	for _, month := range months {
		backup, backupErr := cache.backup(month).Get(ctx)
		if backupErr != nil {
			if canRefresh() {
				refreshed, refreshErr := cache.main.RefreshIfStale(ctx, dataTTL())
				if refreshErr == nil {
					result = result[:0]
					for _, requested := range months {
						result = append(result, monthlyData(refreshed.Data, requested))
					}
					return result, nil
				}
				return result, fmt.Errorf("charging data unavailable: main=%v, backup=%v, refresh=%w", mainErr, backupErr, refreshErr)
			}
			return result, fmt.Errorf("charging data unavailable: main=%v, backup=%w", mainErr, backupErr)
		}
		result = append(result, monthlyData(backup.Data, month))
	}
	if canRefresh() {
		cache.main.Revalidate(dataTTL())
	}
	return result, nil
}
