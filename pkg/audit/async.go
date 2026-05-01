package audit

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// AsyncLogger wraps a Logger and writes events through a buffered channel
// drained by a background worker. The synchronous request path enqueues an
// event in O(1); the goroutine handles the actual database write.
//
// On a full buffer the event is dropped (and counted) so the audit pipeline
// can never block a tools/call. Operators preferring lossless audit must
// size the buffer for their peak rate or wrap the underlying Logger
// differently.
type AsyncLogger struct {
	inner    Logger
	logger   *slog.Logger
	ch       chan Event
	wg       sync.WaitGroup
	timeout  time.Duration
	stop     chan struct{}
	stopOnce sync.Once

	mu      sync.Mutex
	dropped uint64
}

// NewAsyncLogger returns a buffered async wrapper around inner. bufferSize
// is the channel depth; perCallTimeout bounds each underlying Log call.
// Call Close() during shutdown to drain the queue.
func NewAsyncLogger(inner Logger, bufferSize int, perCallTimeout time.Duration, logger *slog.Logger) *AsyncLogger {
	if bufferSize <= 0 {
		bufferSize = 1024
	}
	if perCallTimeout <= 0 {
		perCallTimeout = 5 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	a := &AsyncLogger{
		inner:   inner,
		logger:  logger,
		ch:      make(chan Event, bufferSize),
		timeout: perCallTimeout,
		stop:    make(chan struct{}),
	}
	a.wg.Add(1)
	go a.run()
	return a
}

// Log enqueues; non-blocking. Returns nil even when the buffer is full so
// the request path is never gated on the audit pipeline.
func (a *AsyncLogger) Log(_ context.Context, ev Event) error {
	select {
	case a.ch <- ev:
	default:
		a.mu.Lock()
		a.dropped++
		dropped := a.dropped
		a.mu.Unlock()
		// Log every 1000th drop so operators see the signal without spam.
		if dropped%1000 == 1 {
			a.logger.Warn("audit buffer full; dropping events", "dropped_total", dropped)
		}
	}
	return nil
}

// Query delegates to the inner Logger; reads don't need buffering.
func (a *AsyncLogger) Query(ctx context.Context, f QueryFilter) ([]Event, error) {
	return a.inner.Query(ctx, f)
}

// Count delegates to the inner Logger.
func (a *AsyncLogger) Count(ctx context.Context, f QueryFilter) (int64, error) {
	return a.inner.Count(ctx, f)
}

// TimeSeries delegates to the inner Logger.
func (a *AsyncLogger) TimeSeries(ctx context.Context, from, to time.Time, bucket time.Duration) ([]TimePoint, error) {
	return a.inner.TimeSeries(ctx, from, to, bucket)
}

// Breakdown delegates to the inner Logger.
func (a *AsyncLogger) Breakdown(ctx context.Context, from, to time.Time, dim string) ([]BreakdownPoint, error) {
	return a.inner.Breakdown(ctx, from, to, dim)
}

// Stats delegates to the inner Logger.
func (a *AsyncLogger) Stats(ctx context.Context, from, to time.Time) (Stats, error) {
	return a.inner.Stats(ctx, from, to)
}

// GetPayload delegates to the inner Logger when it implements
// PayloadLogger. Returns (nil, nil) when the underlying logger doesn't
// persist payloads (memory, noop) so the portal API can render the
// summary view without falling over.
func (a *AsyncLogger) GetPayload(ctx context.Context, eventID string) (*Payload, error) {
	pl, ok := a.inner.(PayloadLogger)
	if !ok {
		return nil, nil
	}
	return pl.GetPayload(ctx, eventID)
}

// Close stops accepting new events and waits for the queue to drain.
func (a *AsyncLogger) Close() {
	a.stopOnce.Do(func() { close(a.stop) })
	a.wg.Wait()
}

// Dropped reports the cumulative drop count for monitoring.
func (a *AsyncLogger) Dropped() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dropped
}

func (a *AsyncLogger) run() {
	defer a.wg.Done()
	for {
		select {
		case ev := <-a.ch:
			a.write(ev)
		case <-a.stop:
			// Drain remaining events on shutdown.
			for {
				select {
				case ev := <-a.ch:
					a.write(ev)
				default:
					return
				}
			}
		}
	}
}

func (a *AsyncLogger) write(ev Event) {
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()
	if err := a.inner.Log(ctx, ev); err != nil {
		a.logger.Warn("audit write failed", "tool", ev.ToolName, "err", err)
	}
}

// NoopLogger is a Logger that drops everything. Used when audit.enabled=false.
type NoopLogger struct{}

// Log discards the event.
func (NoopLogger) Log(context.Context, Event) error { return nil }

// Query returns no events.
func (NoopLogger) Query(context.Context, QueryFilter) ([]Event, error) {
	return nil, nil
}

// Count returns 0.
func (NoopLogger) Count(context.Context, QueryFilter) (int64, error) { return 0, nil }

// TimeSeries returns no points.
func (NoopLogger) TimeSeries(context.Context, time.Time, time.Time, time.Duration) ([]TimePoint, error) {
	return nil, nil
}

// Breakdown returns no points.
func (NoopLogger) Breakdown(context.Context, time.Time, time.Time, string) ([]BreakdownPoint, error) {
	return nil, nil
}

// Stats returns zeroed stats.
func (NoopLogger) Stats(context.Context, time.Time, time.Time) (Stats, error) {
	return Stats{}, nil
}
