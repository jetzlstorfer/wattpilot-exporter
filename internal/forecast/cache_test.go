package forecast

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

const validForecast = `{"hourly":{"time":["2026-10-21T12:00"],
	"temperature_2m":[12],"cloud_cover":[20],"precipitation_probability":[0],
	"shortwave_radiation":[400],"weather_code":[1]}}`

type forecastStore struct {
	mu       sync.Mutex
	payload  []byte
	modified time.Time
	reads    int
	stats    int
	writes   int
	writeErr error
}

func (s *forecastStore) Read(context.Context, string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.payload == nil {
		return nil, os.ErrNotExist
	}
	return bytes.Clone(s.payload), nil
}

func (s *forecastStore) Write(_ context.Context, _ string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if s.writeErr != nil {
		return s.writeErr
	}
	s.payload = bytes.Clone(data)
	return nil
}

func (s *forecastStore) ModTime(context.Context, string) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats++
	return s.modified, nil
}

type forecastTransport func(*http.Request) (*http.Response, error)

func (fn forecastTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func useForecastAPI(t *testing.T, transport forecastTransport) {
	t.Helper()
	original := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: time.Second}
	t.Cleanup(func() { httpClient = original })
}

func TestForecastCacheWarmReadsAndBackgroundRefresh(t *testing.T) {
	store := &forecastStore{payload: []byte(validForecast), modified: time.Now().Add(-24 * time.Hour)}
	cache := newForecastCache(context.Background(), store, "test.json", 48, 14)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	useForecastAPI(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-release
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(validForecast))}, nil
	})
	for range 10 {
		value, err := cache.Get(context.Background())
		if err != nil || !bytes.Equal(value.Data, []byte(validForecast)) {
			t.Fatalf("cached forecast = %+v, %v", value, err)
		}
		cache.Revalidate(time.Hour)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := cache.Get(ctx); err != nil {
		t.Fatalf("stale forecast waited for upstream: %v", err)
	}
	close(release)
	value, err := cache.RefreshIfStale(context.Background(), time.Hour)
	if err != nil || time.Since(value.UpdatedAt) > time.Minute {
		t.Fatalf("refreshed forecast = %+v, %v", value, err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if calls.Load() != 1 || store.reads != 1 || store.stats != 1 || store.writes != 1 {
		t.Fatalf("operations: calls=%d reads=%d stats=%d writes=%d", calls.Load(), store.reads, store.stats, store.writes)
	}
}

func TestForecastFailuresPreserveValidPayload(t *testing.T) {
	for _, failure := range []string{"network", "invalid payload", "write failure"} {
		t.Run(failure, func(t *testing.T) {
			store := &forecastStore{payload: []byte(validForecast), modified: time.Now().Add(-24 * time.Hour)}
			if failure == "write failure" {
				store.writeErr = errors.New("storage unavailable")
			}
			var calls atomic.Int32
			useForecastAPI(t, func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if failure == "network" {
					return nil, errors.New("upstream unavailable")
				}
				payload := validForecast
				if failure == "invalid payload" {
					payload = `{}`
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(payload))}, nil
			})
			cache := newForecastCache(context.Background(), store, "test.json", 48, 14)
			if _, err := cache.Get(context.Background()); err != nil {
				t.Fatal(err)
			}
			for range 10 {
				if _, err := cache.RefreshIfStale(context.Background(), time.Hour); err == nil {
					t.Fatal("refresh should fail")
				}
			}
			value, err := cache.Get(context.Background())
			if err != nil || !bytes.Equal(value.Data, []byte(validForecast)) || cache.Status().Err == nil {
				t.Fatalf("stale forecast = %+v, %v, status=%+v", value, err, cache.Status())
			}
			if calls.Load() != 1 || !bytes.Equal(store.payload, []byte(validForecast)) {
				t.Fatal("failed refresh was retried or overwrote valid persisted data")
			}
		})
	}
}

func TestForecastCacheSeparatesLocations(t *testing.T) {
	InitCache(context.Background())
	t.Cleanup(func() { InitCache(context.Background()) })
	first := activeForecastCache(48, 14)
	if activeForecastCache(48, 14) != first {
		t.Fatal("same coordinates should reuse cache")
	}
	firstPath := forecastCache.path
	if activeForecastCache(49, 15) == first || forecastCache.path == firstPath {
		t.Fatal("different coordinates reused forecast memory or persisted path")
	}
}

func TestValidateForecastPayload(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
		valid   bool
	}{
		{"valid", validForecast, true},
		{"invalid JSON", "{", false},
		{"empty response", "{}", false},
		{"inconsistent arrays", `{"hourly":{"time":["2026-10-21T12:00"]}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateForecastPayload([]byte(test.payload)); (err == nil) != test.valid {
				t.Fatalf("validateForecastPayload() = %v, want valid=%v", err, test.valid)
			}
		})
	}
}
