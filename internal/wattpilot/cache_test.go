package wattpilot

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const cachedJSON = `{"columns":[{"key":"energy"}],"data":[
	{"energy":1,"end":"01.06.2024 12:00:00"},
	{"energy":2,"end":"01.07.2024 12:00:00"}]}`
const refreshedJSON = `{"columns":[{"key":"energy"}],"data":[
	{"energy":9,"end":"01.06.2024 12:00:00"}]}`

type memoryStore struct {
	mu       sync.Mutex
	files    map[string][]byte
	modified time.Time
	reads    map[string]int
	stats    int
	writes   int
	writeErr error
}

func testStore() *memoryStore {
	return &memoryStore{
		files:    map[string][]byte{dataFilePath(JSONFileName): []byte(cachedJSON)},
		modified: time.Now(),
		reads:    make(map[string]int),
	}
}

func (s *memoryStore) Read(_ context.Context, name string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads[name]++
	data, ok := s.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return bytes.Clone(data), nil
}

func (s *memoryStore) Write(_ context.Context, name string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if s.writeErr != nil {
		return s.writeErr
	}
	s.files[name] = bytes.Clone(data)
	return nil
}

func (s *memoryStore) ModTime(_ context.Context, _ string) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats++
	return s.modified, nil
}

type cacheTransport func(*http.Request) (*http.Response, error)

func (fn cacheTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func useDataCache(t *testing.T, store *memoryStore) *dataCache {
	t.Helper()
	t.Setenv("WATTPILOT_KEY", "")
	ctx, cancel := context.WithCancel(context.Background())
	original := cachedData
	cachedData = newDataCache(ctx, store)
	t.Cleanup(func() {
		cancel()
		cachedData = original
	})
	return cachedData
}

func useAPI(t *testing.T, transport cacheTransport) {
	t.Helper()
	original := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: time.Second}
	t.Cleanup(func() { httpClient = original })
	t.Setenv("WATTPILOT_KEY", "test-key")
}

func response(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}
}

func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for refresh")
	}
}

func TestStatsForMonthsUsesOneIndexedSnapshot(t *testing.T) {
	store := testStore()
	useDataCache(t, store)
	months := make([]string, 29)
	for i := range months {
		months[i] = time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC).AddDate(0, i, 0).Format("2006-01")
	}
	for range 2 {
		got, err := GetStatsForMonths(context.Background(), months)
		if err != nil || len(got) != 29 {
			t.Fatalf("GetStatsForMonths() length=%d, error=%v", len(got), err)
		}
		if len(got[0].Data) != 1 || got[0].Data[0].Energy != 1 || got[1].Data[0].Energy != 2 {
			t.Fatalf("incorrect month partition: %+v", got[:2])
		}
		if len(got[2].Data) != 0 {
			t.Fatal("empty month contains sessions")
		}
		got[0].Data[0].Energy = 100
		got[0].Columns[0].Key = "mutated"
	}
	got, err := GetStatsForMonths(context.Background(), []string{" 2024-07 ", "2024-06", "2024-07"})
	if err != nil || got[0].Data[0].Energy != 2 || got[1].Data[0].Energy != 1 || got[2].Data[0].Energy != 2 {
		t.Fatalf("month ordering/duplicates = %+v, %v", got, err)
	}
	raw, err := GetJSONData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = '!'
	raw, _ = GetJSONData(context.Background())
	if string(raw) != cachedJSON {
		t.Fatal("caller mutated raw snapshot")
	}
	if store.reads[dataFilePath(JSONFileName)] != 1 || store.stats != 1 || store.writes != 0 {
		t.Fatalf("storage operations: reads=%v stats=%d writes=%d; want one read and one stat total",
			store.reads, store.stats, store.writes)
	}
}

func TestMonthlyBackupFallback(t *testing.T) {
	for _, main := range []string{"", "{invalid", `{"success":false,"message":"invalid key"}`} {
		t.Run(main, func(t *testing.T) {
			store := testStore()
			if main == "" {
				delete(store.files, dataFilePath(JSONFileName))
			} else {
				store.files[dataFilePath(JSONFileName)] = []byte(main)
			}
			store.files[getMonthlyBackupFilename("2024-06")] = []byte(cachedJSON)
			store.files[getMonthlyBackupFilename("2024-07")] = []byte(cachedJSON)
			useDataCache(t, store)
			for range 2 {
				got, err := GetStatsForMonths(context.Background(), []string{"2024-07", "2024-06"})
				if err != nil || got[0].Data[0].Energy != 2 || got[1].Data[0].Energy != 1 {
					t.Fatalf("backup data = %+v, %v", got, err)
				}
			}
			for path, reads := range store.reads {
				if reads != 1 {
					t.Fatalf("%s read %d times, want once", path, reads)
				}
			}
			partial, err := GetStatsForMonths(context.Background(), []string{"2024-06", "2024-08"})
			if err == nil || len(partial) != 1 {
				t.Fatalf("missing backup should preserve partial result and error: %+v, %v", partial, err)
			}
		})
	}
}

func TestInvalidMonthsDoNotAllocateBackupCaches(t *testing.T) {
	store := testStore()
	cache := useDataCache(t, store)
	for _, month := range []string{"", "../config", "2024-99", "2024-6"} {
		if _, err := GetStatsForMonth(context.Background(), month); err == nil {
			t.Errorf("month %q should be rejected", month)
		}
	}
	if len(store.reads) != 0 || len(cache.backups) != 0 {
		t.Fatal("invalid month read storage or allocated a backup cache")
	}
	if got, err := GetStatsForMonths(context.Background(), nil); err != nil || len(got) != 0 {
		t.Fatalf("empty month selection = %+v, %v", got, err)
	}
}

func TestStaleDataAndBackupsReturnDuringRefresh(t *testing.T) {
	for _, backup := range []bool{false, true} {
		t.Run(map[bool]string{false: "main", true: "backup"}[backup], func(t *testing.T) {
			store := testStore()
			store.modified = time.Now().Add(-24 * time.Hour)
			if backup {
				delete(store.files, dataFilePath(JSONFileName))
				store.files[getMonthlyBackupFilename("2024-06")] = []byte(cachedJSON)
			}
			cache := useDataCache(t, store)
			started, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			useAPI(t, func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				close(started)
				select {
				case <-release:
					return response(refreshedJSON), nil
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			})
			got, err := GetStatsForMonth(context.Background(), "2024-06")
			if err != nil || got.Data[0].Energy != 1 {
				t.Fatalf("initial stale data = %+v, %v", got, err)
			}
			if !got.UpdatedAt.Equal(store.modified) {
				t.Fatalf("rendered snapshot timestamp = %v, want %v", got.UpdatedAt, store.modified)
			}
			await(t, started)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			for range 10 {
				got, err = GetStatsForMonth(ctx, "2024-06")
				if err != nil || got.Data[0].Energy != 1 {
					t.Fatalf("read waited for refresh: %+v, %v", got, err)
				}
			}
			close(release)
			if _, err := cache.main.RefreshIfStale(context.Background(), dataTTL()); err != nil {
				t.Fatal(err)
			}
			got, err = GetStatsForMonth(context.Background(), "2024-06")
			if err != nil || got.Data[0].Energy != 9 || calls.Load() != 1 {
				t.Fatalf("refreshed data = %+v, %v, calls=%d", got, err, calls.Load())
			}
			if !got.UpdatedAt.Equal(cache.main.Status().UpdatedAt) || !got.UpdatedAt.After(store.modified) {
				t.Fatal("refreshed data does not identify its new snapshot")
			}
			store.mu.Lock()
			if string(store.files[dataFilePath(JSONFileName)]) != refreshedJSON {
				t.Error("refresh was not persisted")
			}
			store.mu.Unlock()
		})
	}
}

func TestFreshnessChecksRespectTTL(t *testing.T) {
	for _, test := range []struct {
		name    string
		age     time.Duration
		key     bool
		refresh bool
	}{
		{"within TTL", dataTTL() - time.Second, true, false},
		{"past TTL", dataTTL() + time.Second, true, true},
		{"offline cache", dataTTL() + time.Second, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := testStore()
			store.modified = time.Now().Add(-test.age)
			cache := useDataCache(t, store)
			started, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			if test.key {
				useAPI(t, func(r *http.Request) (*http.Response, error) {
					calls.Add(1)
					close(started)
					select {
					case <-release:
						return response(refreshedJSON), nil
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
				})
			}
			// Status checks alone must hydrate a cold process and refresh an open
			// page, without requiring another dashboard request.
			for range 10 {
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				status := CheckDataFreshness(ctx)
				cancel()
				if status.Err != nil || status.Refreshing != test.refresh || !status.UpdatedAt.Equal(store.modified) {
					t.Fatalf("freshness status = %+v", status)
				}
			}
			if test.refresh {
				await(t, started)
				close(release)
				if _, err := cache.main.RefreshIfStale(context.Background(), dataTTL()); err != nil {
					t.Fatal(err)
				}
				status := CheckDataFreshness(context.Background())
				if status.Refreshing || status.Err != nil || !status.UpdatedAt.After(store.modified) || calls.Load() != 1 {
					t.Fatalf("completed refresh status=%+v, calls=%d", status, calls.Load())
				}
			} else if calls.Load() != 0 {
				t.Fatal("freshness check fetched before the TTL or without a key")
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			if store.reads[dataFilePath(JSONFileName)] != 1 || store.stats != 1 {
				t.Fatalf("status checks repeated storage reads: reads=%v, stats=%d", store.reads, store.stats)
			}
		})
	}
}

func TestColdMissSharesOneFetch(t *testing.T) {
	store := testStore()
	delete(store.files, dataFilePath(JSONFileName))
	cache := useDataCache(t, store)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	useAPI(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-release
		return response(refreshedJSON), nil
	})
	results := make(chan error, 16)
	for range 16 {
		go func() {
			value, err := GetStatsForMonth(context.Background(), "2024-06")
			if err == nil && (len(value.Data) != 1 || value.Data[0].Energy != 9) {
				err = errors.New("unexpected fetched data")
			}
			results <- err
		}()
	}
	await(t, started)
	close(release)
	for range 16 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 || cache.main.Status().Err != nil {
		t.Fatalf("fetch count=%d, status=%+v", calls.Load(), cache.main.Status())
	}
}

func TestRefreshFailuresPreserveSnapshotsAndBackOff(t *testing.T) {
	for _, failure := range []string{"network", "invalid JSON", "HTTP error", "write failure"} {
		t.Run(failure, func(t *testing.T) {
			store := testStore()
			store.modified = time.Now().Add(-24 * time.Hour)
			cache := useDataCache(t, store)
			if _, err := GetStatsForMonth(context.Background(), "2024-06"); err != nil {
				t.Fatal(err)
			}
			if failure == "write failure" {
				store.writeErr = errors.New("disk full")
			}
			var calls atomic.Int32
			useAPI(t, func(r *http.Request) (*http.Response, error) {
				if calls.Add(1) > 1 {
					return response(refreshedJSON), nil
				}
				switch failure {
				case "network":
					return nil, errors.New("network unavailable")
				case "invalid JSON":
					return response(`{"success":false,"message":"invalid key"}`), nil
				case "HTTP error":
					resp := response(refreshedJSON)
					resp.StatusCode = http.StatusServiceUnavailable
					return resp, nil
				default:
					return response(refreshedJSON), nil
				}
			})
			if err := RefreshData(context.Background()); err == nil {
				t.Fatal("refresh should fail")
			}
			for range 10 {
				if status := CheckDataFreshness(context.Background()); status.Err == nil || status.Refreshing {
					t.Fatalf("status checks bypassed refresh failure backoff: %+v", status)
				}
				got, err := GetStatsForMonth(context.Background(), "2024-06")
				if err != nil || got.Data[0].Energy != 1 {
					t.Fatalf("failed refresh replaced snapshot: %+v, %v", got, err)
				}
			}
			if calls.Load() != 1 || cache.main.Status().Err == nil {
				t.Fatalf("refresh backoff/status: calls=%d, status=%+v", calls.Load(), cache.main.Status())
			}
			if string(store.files[dataFilePath(JSONFileName)]) != cachedJSON {
				t.Fatal("failed refresh replaced persisted data")
			}
			store.writeErr = nil
			if err := RefreshData(context.Background()); err != nil {
				t.Fatalf("manual refresh should bypass backoff: %v", err)
			}
			got, err := GetStatsForMonth(context.Background(), "2024-06")
			if err != nil || got.Data[0].Energy != 9 {
				t.Fatalf("manual refresh did not invalidate snapshot: %+v, %v", got, err)
			}
		})
	}
}
