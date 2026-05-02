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

// QueryFilter narrows audit_events results.
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
}
