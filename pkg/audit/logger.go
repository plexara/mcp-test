package audit

import (
	"context"
	"time"
)

// Logger writes events and queries them back for the portal. Loggers
// that capture the audit_payloads sibling row implement PayloadLogger
// for the detail-fetch path; basic implementations (memory, noop) only
// hold the indexable summary.
type Logger interface {
	Log(ctx context.Context, ev Event) error
	Query(ctx context.Context, f QueryFilter) ([]Event, error)
	Count(ctx context.Context, f QueryFilter) (int64, error)
	TimeSeries(ctx context.Context, from, to time.Time, bucket time.Duration) ([]TimePoint, error)
	Breakdown(ctx context.Context, from, to time.Time, dimension string) ([]BreakdownPoint, error)
	Stats(ctx context.Context, from, to time.Time) (Stats, error)
}

// PayloadLogger is the optional capability for detail fetch. Stores that
// persist the audit_payloads sibling row implement it; consumers type-
// assert for it before calling GetPayload.
type PayloadLogger interface {
	GetPayload(ctx context.Context, eventID string) (*Payload, error)
}

// StreamingLogger is the optional capability for cursor-style iteration
// over a filtered event set. The NDJSON export endpoint type-asserts
// for it so we don't load the entire result set into memory.
//
// Semantics:
//   - f.Limit and f.Offset are IGNORED by implementations that can
//     truly stream (Postgres, MemoryLogger). Stream iterates the whole
//     filtered set; the caller stops early by returning a sentinel
//     error from fn. The export endpoint does this with its own cap.
//   - fn is called once per event; returning a non-nil error stops the
//     iteration and bubbles back to Stream's caller. ctx cancellation
//     is honored at page boundaries.
//
// Implementations that wrap a Logger which does NOT itself implement
// StreamingLogger (e.g. AsyncLogger over a custom non-streaming inner)
// degrade to a single bounded Query() call capped at MaxQueryLimit;
// such implementations can deliver fewer events than the filter would
// match. See AsyncLogger.Stream for the fallback contract.
type StreamingLogger interface {
	Stream(ctx context.Context, f QueryFilter, fn func(Event) error) error
}

// MaxQueryLimit is the largest LIMIT any backend will honor on a single
// SELECT. Larger values get silently reduced. Defined here (not in the
// Postgres package) so AsyncLogger and any other Logger wrapper can
// honor the same cap when falling back to Query()-driven iteration,
// avoiding the silent-cap-mismatch where a wrapper promises N rows
// and the underlying backend delivers fewer.
const MaxQueryLimit = 1000

// TimePoint is one bucket of an audit time series.
type TimePoint struct {
	Time     time.Time `json:"time"`
	Count    int64     `json:"count"`
	Errors   int64     `json:"errors"`
	AvgDurMS float64   `json:"avg_duration_ms"`
}

// BreakdownPoint groups events by a dimension (tool, user_subject, success).
type BreakdownPoint struct {
	Key    string `json:"key"`
	Count  int64  `json:"count"`
	Errors int64  `json:"errors"`
}

// Stats is a summary panel for the portal dashboard.
type Stats struct {
	Total          int64   `json:"total"`
	Errors         int64   `json:"errors"`
	ErrorRate      float64 `json:"error_rate"`
	AvgDurationMS  float64 `json:"avg_duration_ms"`
	P50DurationMS  int64   `json:"p50_duration_ms"`
	P95DurationMS  int64   `json:"p95_duration_ms"`
	UniqueSubjects int64   `json:"unique_subjects"`
	UniqueTools    int64   `json:"unique_tools"`
}

// QueryFilter narrows audit_events results. Filters are AND-combined.
//
// JSONFilters and HasKeys reach into the audit_payloads sibling row.
// The Postgres store compiles them to JSONB containment / column-not-null
// predicates against audit_payloads via EXISTS subqueries; the
// MemoryLogger evaluates them in Go against Event.Payload. Loggers that
// don't carry payloads (NoopLogger) ignore them.
type QueryFilter struct {
	From      time.Time
	To        time.Time
	ToolName  string
	UserID    string
	SessionID string
	EventID   string // exact-match on audit_events.id (single-event fetch)
	Success   *bool
	Search    string
	Limit     int
	Offset    int
	OrderDesc bool

	// JSONFilters narrow by JSONB content of audit_payloads.
	// Each filter checks that the value at Path inside Source equals
	// Value (with light type detection on Value, see ParseJSONValue).
	JSONFilters []JSONPathFilter

	// HasKeys narrows to events whose audit_payloads row has the named
	// payload column populated (NOT NULL). Allowed keys come from
	// AllowedHasKeys; anything else is rejected at parse time.
	HasKeys []string
}

// JSONPathFilter narrows by a value inside an audit_payloads JSONB column.
//
// Source picks the column: "param" -> request_params, "response" ->
// response_result, "header" -> request_headers. Path is the dotted
// path inside that column ("user.id"); Value is the URL string,
// type-detected at compile time so ?response.isError=true matches the
// JSON literal true rather than the string "true".
type JSONPathFilter struct {
	Source string
	Path   []string
	Value  string
}

// AllowedHasKeys is the closed set of payload-column names accepted by
// the ?has=<key> query parameter. Restricted to columns the audit
// middleware actually populates today; new columns must be added here
// before they're queryable. Strings (not constants) so the HTTP layer
// can validate user input directly.
var AllowedHasKeys = []string{
	"request_params",
	"request_headers",
	"response_result",
	"response_error",
	"notifications",
	"replayed_from",
}

// AllowedJSONSources is the closed set of {Source} values JSONPathFilter
// accepts; any other value is rejected at parse time.
var AllowedJSONSources = []string{"param", "response", "header"}
