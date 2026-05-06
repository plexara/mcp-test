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
// matchAll applies every QueryFilter predicate EXCEPT Limit/Offset and
// returns the matching events in canonical Query order (ts DESC, id ASC).
// Shared by Query, Count, and Stream so all three agree on filter
// semantics and ordering regardless of pagination.
//
// Caller must hold m.mu.
func (m *MemoryLogger) matchAll(f QueryFilter) []Event {
	out := make([]Event, 0, len(m.events))
	for _, ev := range m.events {
		if !matchesFilter(ev, f) {
			continue
		}
		out = append(out, ev)
	}
	// ts DESC, id ASC. Mirrors the Postgres store's ordering so the
	// two backends agree under tied timestamps; without the tiebreaker,
	// Stream pagination can duplicate or skip rows at page boundaries.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Timestamp.After(out[j].Timestamp)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Query returns matching events ordered ts DESC, id ASC, with the
// limit-clamp rules described inline. Calls matchAll to share filter
// semantics with Count and Stream.
func (m *MemoryLogger) Query(_ context.Context, f QueryFilter) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.matchAll(f)
	// Limit-clamp rules, mirroring Postgres buildSelect:
	//   Limit <= 0 (unset / negative) -> default page size (100)
	//   0 < Limit <= MaxQueryLimit    -> honored as given
	//   Limit > MaxQueryLimit         -> clamped to MaxQueryLimit
	// Count and Stream do NOT route through here; they use matchAll
	// directly so the page-size default doesn't truncate them.
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > MaxQueryLimit {
		limit = MaxQueryLimit
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Count returns the number of matching events. Ignores f.Limit and
// f.Offset so pagination metadata is correct (Postgres uses
// SELECT count(*); MemoryLogger uses len() over matchAll). Routing
// through Query would silently cap at the page-size default.
func (m *MemoryLogger) Count(_ context.Context, f QueryFilter) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.matchAll(f))), nil
}

// Stream calls fn once per matching event, in the same order Query would
// return them. Used by the NDJSON export to avoid loading the full
// result set into memory; for the in-memory logger the savings are
// theoretical, but the contract matches the Postgres store's cursor.
//
// Per the StreamingLogger contract, f.Limit and f.Offset are ignored:
// Stream iterates the whole filtered set; callers stop early by
// returning a sentinel error from fn. Uses matchAll directly so the
// 100-row page-size default in Query never reaches this path.
func (m *MemoryLogger) Stream(ctx context.Context, f QueryFilter, fn func(Event) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	evs := m.matchAll(f)
	m.mu.Unlock()
	for _, ev := range evs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fn(ev); err != nil {
			return err
		}
	}
	return nil
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
	if f.EventID != "" && ev.ID != f.EventID {
		return false
	}
	if f.ToolName != "" && ev.ToolName != f.ToolName {
		return false
	}
	if f.UserID != "" && ev.UserSubject != f.UserID {
		return false
	}
	if f.SessionID != "" && ev.SessionID != f.SessionID {
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
	if !matchesJSONFilters(ev, f.JSONFilters) {
		return false
	}
	if !matchesHasKeys(ev, f.HasKeys) {
		return false
	}
	return true
}

// matchesJSONFilters runs each JSONPathFilter against ev.Payload using
// the same path semantics the Postgres store will compile to JSONB
// containment. Returns false the moment any filter misses; an event
// with no payload row fails any non-empty filter.
func matchesJSONFilters(ev Event, fs []JSONPathFilter) bool {
	if len(fs) == 0 {
		return true
	}
	if ev.Payload == nil {
		return false
	}
	for _, jf := range fs {
		var src map[string]any
		switch jf.Source {
		case "param":
			src = ev.Payload.RequestParams
		case "response":
			src = ev.Payload.ResponseResult
		case "header":
			// Headers are map[string][]string in Go and serialize to
			// JSONB as {"Name": ["value", ...]}. Postgres-side @>
			// containment matches when the array contains the wanted
			// value; mirror that here by accepting a hit on any of the
			// stored values for the named header.
			if ev.Payload.RequestHeaders == nil {
				return false
			}
			if len(jf.Path) == 0 {
				return false
			}
			values, ok := ev.Payload.RequestHeaders[jf.Path[0]]
			if !ok || len(values) == 0 {
				return false
			}
			// Header values are always strings on the wire; compare the
			// raw filter value as a string rather than running it through
			// ParseJSONFilterValue. Mirrors what the Postgres compiler does.
			matched := false
			for _, v := range values {
				if v == jf.Value {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
			continue
		default:
			return false
		}
		if src == nil {
			return false
		}
		want := ParseJSONFilterValue(jf.Value)
		if !MatchJSONPath(src, jf.Path, want) {
			return false
		}
	}
	return true
}

// matchesHasKeys returns false when any required has= column is missing
// from the event's payload. AllowedHasKeys is the closed set; anything
// else returns false (the HTTP layer should have rejected it earlier,
// but defense in depth).
func matchesHasKeys(ev Event, keys []string) bool {
	if len(keys) == 0 {
		return true
	}
	if ev.Payload == nil {
		return false
	}
	p := ev.Payload
	for _, k := range keys {
		switch k {
		case "request_params":
			if len(p.RequestParams) == 0 {
				return false
			}
		case "request_headers":
			if len(p.RequestHeaders) == 0 {
				return false
			}
		case "response_result":
			if len(p.ResponseResult) == 0 {
				return false
			}
		case "response_error":
			if len(p.ResponseError) == 0 {
				return false
			}
		case "notifications":
			if len(p.Notifications) == 0 {
				return false
			}
		case "replayed_from":
			if p.ReplayedFrom == "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}
