package snapshot

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for cache operation")
		var zero T
		return zero
	}
}

func TestConcurrentGetLoadsOnce(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	cache := New(context.Background(), "test", time.Second,
		func(ctx context.Context) (Value[string], error) {
			calls.Add(1)
			close(started)
			<-release
			return Value[string]{Data: "cached", UpdatedAt: time.Now()}, nil
		}, nil)

	results := make(chan string, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			value, err := cache.Get(context.Background())
			if err != nil {
				results <- err.Error()
			} else {
				results <- value.Data
			}
		})
	}
	receive(t, started)
	close(release)
	wg.Wait()
	for range 32 {
		if got := receive(t, results); got != "cached" {
			t.Fatalf("Get() = %q", got)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("load calls = %d, want 1", got)
	}
}

func TestStaleReadsDoNotWaitAndRefreshIsCoalesced(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	old := time.Now().Add(-time.Hour)
	cache := New(context.Background(), "test", time.Second,
		func(ctx context.Context) (Value[string], error) {
			return Value[string]{Data: "old", UpdatedAt: old}, nil
		},
		func(ctx context.Context) (Value[string], error) {
			calls.Add(1)
			close(started)
			<-release
			return Value[string]{Data: "new", UpdatedAt: time.Now()}, nil
		})
	if _, err := cache.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 32 {
		cache.Revalidate(time.Minute)
	}
	receive(t, started)
	value, err := cache.Get(context.Background())
	if err != nil || value.Data != "old" {
		t.Fatalf("stale Get() = %+v, %v", value, err)
	}
	if status := cache.Status(); !status.Refreshing || !status.UpdatedAt.Equal(old) {
		t.Fatalf("status during refresh = %+v", status)
	}
	joined := cache.startRefresh(true, 0)
	close(release)
	value, err = wait(context.Background(), joined)
	if err != nil || value.Data != "new" {
		t.Fatalf("refresh = %+v, %v", value, err)
	}
	cache.Revalidate(time.Minute)
	if got := calls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

func TestRefreshFailureBackoffAndManualOverride(t *testing.T) {
	var calls atomic.Int32
	upstreamErr := errors.New("upstream unavailable")
	cache := New(context.Background(), "test", time.Second,
		func(ctx context.Context) (Value[string], error) {
			return Value[string]{Data: "old", UpdatedAt: time.Now().Add(-time.Hour)}, nil
		},
		func(ctx context.Context) (Value[string], error) {
			if calls.Add(1) <= 2 {
				return Value[string]{}, upstreamErr
			}
			return Value[string]{Data: "new", UpdatedAt: time.Now()}, nil
		})
	_, _ = cache.Get(context.Background())
	for range 10 {
		if _, err := cache.RefreshIfStale(context.Background(), time.Minute); !errors.Is(err, upstreamErr) {
			t.Fatalf("refresh error = %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("refreshes during backoff = %d, want 1", calls.Load())
	}
	if value, _ := cache.Get(context.Background()); value.Data != "old" {
		t.Fatal("failed refresh replaced valid snapshot")
	}
	if !errors.Is(cache.Status().Err, upstreamErr) {
		t.Fatal("refresh failure missing from status")
	}
	cache.mu.Lock()
	cache.retryAt = time.Now().Add(-time.Second)
	cache.mu.Unlock()
	_, _ = cache.RefreshIfStale(context.Background(), time.Minute)
	if calls.Load() != 2 {
		t.Fatal("automatic refresh did not retry after backoff expired")
	}
	value, err := cache.Refresh(context.Background())
	if err != nil || value.Data != "new" || cache.Status().Err != nil {
		t.Fatalf("manual retry = %+v, %v, status=%+v", value, err, cache.Status())
	}
}

func TestFailedLoadIsBackedOffWithoutBlockingOnRefresh(t *testing.T) {
	var loads atomic.Int32
	release := make(chan struct{})
	cache := New(context.Background(), "test", time.Second,
		func(ctx context.Context) (Value[string], error) {
			loads.Add(1)
			return Value[string]{}, errors.New("missing main file")
		},
		func(ctx context.Context) (Value[string], error) {
			<-release
			return Value[string]{Data: "new", UpdatedAt: time.Now()}, nil
		})
	for range 10 {
		if _, err := cache.Get(context.Background()); err == nil {
			t.Fatal("missing snapshot should return an error")
		}
	}
	cache.Revalidate(time.Minute)
	op := cache.startRefresh(false, time.Minute)
	if _, err := cache.Get(context.Background()); err == nil {
		t.Fatal("Get should let caller use backups while refresh is pending")
	}
	close(release)
	_, _ = wait(context.Background(), op)
	if loads.Load() != 1 {
		t.Fatalf("initial storage reads = %d, want 1", loads.Load())
	}
}

func TestRequestCancellationDoesNotCancelSharedWork(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	cache := New(context.Background(), "test", time.Second,
		func(ctx context.Context) (Value[string], error) {
			close(started)
			select {
			case <-release:
				return Value[string]{Data: "loaded"}, nil
			case <-ctx.Done():
				return Value[string]{}, ctx.Err()
			}
		}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := cache.Get(ctx)
		done <- err
	}()
	receive(t, started)
	cancel()
	if err := receive(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("request error = %v", err)
	}
	close(release)
	if value, err := cache.Get(context.Background()); err != nil || value.Data != "loaded" {
		t.Fatalf("shared load = %+v, %v", value, err)
	}
}

func TestRefreshHonorsTimeoutAndApplicationShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "shutdown"}[shutdown], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			cache := New(ctx, "test", 20*time.Millisecond, nil,
				func(ctx context.Context) (Value[string], error) {
					close(started)
					<-ctx.Done()
					return Value[string]{}, ctx.Err()
				})
			op := cache.startRefresh(true, 0)
			receive(t, started)
			want := context.DeadlineExceeded
			if shutdown {
				want = context.Canceled
				cancel()
			}
			if _, err := wait(context.Background(), op); !errors.Is(err, want) {
				t.Fatalf("refresh error = %v, want %v", err, want)
			}
		})
	}
}

func TestInitialReadCannotOverwriteRefreshedSnapshot(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	cache := New(context.Background(), "test", time.Second,
		func(ctx context.Context) (Value[string], error) {
			close(started)
			<-release
			return Value[string]{Data: "old"}, nil
		},
		func(ctx context.Context) (Value[string], error) {
			return Value[string]{Data: "new"}, nil
		})
	result := make(chan string, 1)
	go func() {
		value, _ := cache.Get(context.Background())
		result <- value.Data
	}()
	receive(t, started)
	if _, err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if got := receive(t, result); got != "new" {
		t.Fatalf("initial read replaced refreshed data with %q", got)
	}
}
