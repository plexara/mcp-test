package audit

import (
	"context"
	"sort"
	"sync"
	"time"
)

// breakdownKeyFn picks the per-event key used by Breakdown.
//
// Defined here so both MemoryLogger and PostgresStore can share dimension
// validation logic.

// MemoryLogger is an in-memory Logger used by tests.
type MemoryLogger struct {
	mu     sync.Mutex
	events []Event
}

// NewMemoryLogger returns an empty logger.
func NewMemoryLogger() *MemoryLogger { return &MemoryLogger{} }

// Log appends the event.
func (m *MemoryLogger) Log(_ context.Context, ev Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

// Query returns matching events ordered by timestamp DESC. Only ToolName,
// UserID, From, To, Success, and Limit are honored; other filter fields are
// ignored. Sufficient for tests; the Postgres store covers the full filter
// surface.
func (m *MemoryLogger) Query(_ context.Context, f QueryFilter) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, 0, len(m.events))
	for _, ev := range m.events {
		if !matchesFilter(ev, f) {
			continue
		}
		out = append(out, ev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.After(out[j].Timestamp) })
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// Count returns the number of matching events, ignoring Limit/Offset so
// pagination metadata is correct.
func (m *MemoryLogger) Count(ctx context.Context, f QueryFilter) (int64, error) {
	f.Limit = 0
	f.Offset = 0
	evs, err := m.Query(ctx, f)
	if err != nil {
		return 0, err
	}
	return int64(len(evs)), nil
}

// Snapshot returns a copy of all events in insertion order, for assertions.
func (m *MemoryLogger) Snapshot() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.events))
	copy(out, m.events)
	return out
}

// TimeSeries buckets events by `bucket` and returns Count / Errors / AvgDurMS.
func (m *MemoryLogger) TimeSeries(_ context.Context, from, to time.Time, bucket time.Duration) ([]TimePoint, error) {
	if bucket <= 0 {
		bucket = time.Minute
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	type acc struct {
		count, errors, totalDur int64
	}
	buckets := map[time.Time]*acc{}
	for _, ev := range m.events {
		if !from.IsZero() && ev.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && ev.Timestamp.After(to) {
			continue
		}
		t := ev.Timestamp.Truncate(bucket)
		a := buckets[t]
		if a == nil {
			a = &acc{}
			buckets[t] = a
		}
		a.count++
		if !ev.Success {
			a.errors++
		}
		a.totalDur += ev.DurationMS
	}
	keys := make([]time.Time, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })
	out := make([]TimePoint, 0, len(keys))
	for _, k := range keys {
		a := buckets[k]
		var avg float64
		if a.count > 0 {
			avg = float64(a.totalDur) / float64(a.count)
		}
		out = append(out, TimePoint{Time: k, Count: a.count, Errors: a.errors, AvgDurMS: avg})
	}
	return out, nil
}

// Breakdown groups events by tool / user / success.
func (m *MemoryLogger) Breakdown(_ context.Context, from, to time.Time, dimension string) ([]BreakdownPoint, error) {
	keyFn := breakdownKeyFn(dimension)
	if keyFn == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	groups := map[string]*BreakdownPoint{}
	for _, ev := range m.events {
		if !from.IsZero() && ev.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && ev.Timestamp.After(to) {
			continue
		}
		k := keyFn(ev)
		g := groups[k]
		if g == nil {
			g = &BreakdownPoint{Key: k}
			groups[k] = g
		}
		g.Count++
		if !ev.Success {
			g.Errors++
		}
	}
	out := make([]BreakdownPoint, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out, nil
}

// Stats computes summary metrics for the dashboard.
func (m *MemoryLogger) Stats(_ context.Context, from, to time.Time) (Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var (
		total, errs, totalDur int64
		durations             []int64
		subjects              = map[string]struct{}{}
		tools                 = map[string]struct{}{}
	)
	for _, ev := range m.events {
		if !from.IsZero() && ev.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && ev.Timestamp.After(to) {
			continue
		}
		total++
		if !ev.Success {
			errs++
		}
		totalDur += ev.DurationMS
		durations = append(durations, ev.DurationMS)
		if ev.UserSubject != "" {
			subjects[ev.UserSubject] = struct{}{}
		}
		if ev.ToolName != "" {
			tools[ev.ToolName] = struct{}{}
		}
	}
	s := Stats{
		Total:          total,
		Errors:         errs,
		UniqueSubjects: int64(len(subjects)),
		UniqueTools:    int64(len(tools)),
	}
	if total > 0 {
		s.ErrorRate = float64(errs) / float64(total)
		s.AvgDurationMS = float64(totalDur) / float64(total)
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		s.P50DurationMS = durations[len(durations)/2]
		idx := int(float64(len(durations)-1) * 0.95)
		s.P95DurationMS = durations[idx]
	}
	return s, nil
}

func breakdownKeyFn(dimension string) func(Event) string {
	switch dimension {
	case "tool":
		return func(ev Event) string { return ev.ToolName }
	case "user":
		return func(ev Event) string { return ev.UserSubject }
	case "success":
		return func(ev Event) string {
			if ev.Success {
				return "success"
			}
			return "error"
		}
	case "auth_type":
		return func(ev Event) string { return ev.AuthType }
	}
	return nil
}

func matchesFilter(ev Event, f QueryFilter) bool {
	if f.ToolName != "" && ev.ToolName != f.ToolName {
		return false
	}
	if f.UserID != "" && ev.UserSubject != f.UserID {
		return false
	}
	if !f.From.IsZero() && ev.Timestamp.Before(f.From) {
		return false
	}
	if !f.To.IsZero() && ev.Timestamp.After(f.To) {
		return false
	}
	if f.Success != nil && ev.Success != *f.Success {
		return false
	}
	return true
}
