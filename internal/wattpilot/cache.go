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
	main      *snapshot.Cache[indexedData]
	mu        sync.Mutex
	backups   map[string]*snapshot.Cache[indexedData]
	ctx       context.Context
	store     storage.DataStore
	startOnce sync.Once
	startDone chan struct{}
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
	cache := &dataCache{ctx: ctx, store: store, backups: make(map[string]*snapshot.Cache[indexedData]), startDone: make(chan struct{})}
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

func (c *dataCache) refreshTTL() time.Duration {
	if c.main.Status().Err != nil {
		return 0
	}
	return dataTTL()
}

func canRefresh() bool {
	return os.Getenv("WATTPILOT_KEY") != ""
}

// start attempts a live fetch once per process before any persisted data is served.
func (c *dataCache) start(ctx context.Context) error {
	c.startOnce.Do(func() {
		go func() {
			defer close(c.startDone)
			if canRefresh() {
				_, _ = c.main.Refresh(c.ctx)
			}
		}()
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.startDone:
		return nil
	}
}

func (c *dataCache) mainData(ctx context.Context) (snapshot.Value[indexedData], error) {
	if err := c.start(ctx); err != nil {
		return snapshot.Value[indexedData]{}, err
	}
	value, err := c.main.Get(ctx)
	if ctx.Err() != nil {
		return snapshot.Value[indexedData]{}, ctx.Err()
	}
	if canRefresh() {
		fresh, refreshErr := c.main.RefreshIfStale(ctx, c.refreshTTL())
		if ctx.Err() != nil {
			return snapshot.Value[indexedData]{}, ctx.Err()
		}
		if refreshErr == nil {
			return fresh, nil
		}
		if err != nil {
			return snapshot.Value[indexedData]{}, fmt.Errorf("charging data unavailable: cached=%v, refresh=%w", err, refreshErr)
		}
	}
	return value, err
}

func monthlyData(value snapshot.Value[indexedData], month string) WattpilotData {
	return WattpilotData{
		Columns:   slices.Clone(value.Data.data.Columns),
		Data:      slices.Clone(value.Data.months[month]),
		UpdatedAt: value.UpdatedAt,
	}
}

// DataCacheStatus reports process-local charging data freshness without reading storage.
func DataCacheStatus() snapshot.Status {
	status := activeCache().main.Status()
	if !canRefresh() && status.Err == nil {
		status.Err = fmt.Errorf("WATTPILOT_KEY is not set")
	}
	return status
}

// CheckDataFreshness hydrates the cache if needed and starts a background refresh
// using the same TTL, coalescing, and failure backoff as data requests.
func CheckDataFreshness(ctx context.Context) snapshot.Status {
	cache := activeCache()
	if err := cache.start(ctx); err != nil {
		return DataCacheStatus()
	}
	if _, err := cache.main.Get(ctx); err != nil && ctx.Err() != nil {
		return DataCacheStatus()
	}
	if canRefresh() {
		cache.main.Revalidate(cache.refreshTTL())
	}
	return DataCacheStatus()
}

func statsForMonths(ctx context.Context, months []string) ([]WattpilotData, error) {
	for _, month := range months {
		if _, err := time.Parse("2006-01", month); err != nil {
			return nil, fmt.Errorf("invalid charging month %q: %w", month, err)
		}
	}
	cache := activeCache()
	main, mainErr := cache.mainData(ctx)
	if mainErr == nil {
		result := make([]WattpilotData, 0, len(months))
		for _, month := range months {
			result = append(result, monthlyData(main, month))
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
			return result, fmt.Errorf("charging data unavailable: main=%v, backup=%w", mainErr, backupErr)
		}
		result = append(result, monthlyData(backup, month))
	}
	return result, nil
}
