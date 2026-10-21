package snapshot

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const RetryDelay = time.Minute

// Value is an immutable snapshot and the timestamp of its persisted source.
type Value[T any] struct {
	Data      T
	UpdatedAt time.Time
}

type operation[T any] struct {
	done  chan struct{}
	value Value[T]
	err   error
}

type Status struct {
	UpdatedAt  time.Time
	Refreshing bool
	Err        error
}

// Cache coalesces initial reads and refreshes. Callers must not mutate its values.
type Cache[T any] struct {
	mu          sync.Mutex
	value       *Value[T]
	loading     *operation[T]
	refreshing  *operation[T]
	loadErr     error
	loadRetryAt time.Time
	refreshErr  error
	retryAt     time.Time
	ctx         context.Context
	timeout     time.Duration
	name        string
	load        func(context.Context) (Value[T], error)
	refresh     func(context.Context) (Value[T], error)
}

func New[T any](ctx context.Context, name string, timeout time.Duration,
	load, refresh func(context.Context) (Value[T], error),
) *Cache[T] {
	return &Cache[T]{ctx: ctx, name: name, timeout: timeout, load: load, refresh: refresh}
}

func (c *Cache[T]) Get(ctx context.Context) (Value[T], error) {
	c.mu.Lock()
	if c.value != nil {
		value := *c.value
		c.mu.Unlock()
		return value, nil
	}
	op := c.loading
	if op == nil {
		if c.loadErr != nil && time.Now().Before(c.loadRetryAt) {
			err := c.loadErr
			c.mu.Unlock()
			return Value[T]{}, err
		}
		op = &operation[T]{done: make(chan struct{})}
		c.loading = op
		go c.run(op, false)
	}
	c.mu.Unlock()
	return wait(ctx, op)
}

// Revalidate starts at most one background refresh if the snapshot is stale.
func (c *Cache[T]) Revalidate(ttl time.Duration) {
	c.startRefresh(false, ttl)
}

func (c *Cache[T]) RefreshIfStale(ctx context.Context, ttl time.Duration) (Value[T], error) {
	return wait(ctx, c.startRefresh(false, ttl))
}

// Refresh bypasses failure backoff, but joins any refresh already in progress.
func (c *Cache[T]) Refresh(ctx context.Context) (Value[T], error) {
	return wait(ctx, c.startRefresh(true, 0))
}

func (c *Cache[T]) startRefresh(force bool, ttl time.Duration) *operation[T] {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.refreshing != nil {
		return c.refreshing
	}
	if !force && c.value != nil && time.Since(c.value.UpdatedAt) <= ttl {
		return completed(*c.value, nil)
	}
	if !force && c.refreshErr != nil && time.Now().Before(c.retryAt) {
		return completed(Value[T]{}, c.refreshErr)
	}
	op := &operation[T]{done: make(chan struct{})}
	c.refreshing = op
	go c.run(op, true)
	return op
}

func (c *Cache[T]) run(op *operation[T], refresh bool) {
	// Shared work follows the application's lifetime, not an individual request.
	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()
	fn := c.load
	if refresh {
		fn = c.refresh
	}
	value, err := fn(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Snapshot operation failed", "cache", c.name, "refresh", refresh, "error", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if refresh {
		c.refreshing = nil
		c.refreshErr = err
		if err != nil {
			c.retryAt = time.Now().Add(RetryDelay)
		} else {
			c.value = &value
			c.loadErr = nil
			c.retryAt = time.Time{}
		}
	} else {
		c.loading = nil
		if c.value != nil {
			// A manual refresh may have completed while the initial read ran.
			value, err = *c.value, nil
		} else if err == nil {
			c.value = &value
		}
		c.loadErr = err
		if err != nil {
			c.loadRetryAt = time.Now().Add(RetryDelay)
		}
	}
	op.value, op.err = value, err
	close(op.done)
}

func (c *Cache[T]) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := Status{Refreshing: c.refreshing != nil, Err: c.refreshErr}
	if status.Err == nil {
		status.Err = c.loadErr
	}
	if c.value != nil {
		status.UpdatedAt = c.value.UpdatedAt
	}
	return status
}

func completed[T any](value Value[T], err error) *operation[T] {
	op := &operation[T]{done: make(chan struct{}), value: value, err: err}
	close(op.done)
	return op
}

func wait[T any](ctx context.Context, op *operation[T]) (Value[T], error) {
	select {
	case <-ctx.Done():
		return Value[T]{}, ctx.Err()
	case <-op.done:
		return op.value, op.err
	}
}
