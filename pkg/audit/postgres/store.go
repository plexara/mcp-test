// Package auditpg provides a pgx-backed implementation of audit.Logger.
package auditpg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/plexara/mcp-test/pkg/audit"
)

// Store is a pgxpool-backed audit.Logger.
type Store struct {
	pool *pgxpool.Pool
}

// New constructs a Store.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Log inserts a single event.
func (s *Store) Log(ctx context.Context, ev audit.Event) error {
	if ev.ID == "" {
		ev.ID = uuid.NewString()
	}
	var paramsJSON []byte
	if ev.Parameters != nil {
		b, err := json.Marshal(ev.Parameters)
		if err != nil {
			return fmt.Errorf("marshal parameters: %w", err)
		}
		paramsJSON = b
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_events (
			id, ts, duration_ms, request_id, session_id,
			user_subject, user_email, auth_type, api_key_name,
			tool_name, tool_group, parameters,
			success, error_message, error_category,
			request_chars, response_chars, content_blocks,
			transport, source, remote_addr, user_agent
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8,$9,
			$10,$11,$12,
			$13,$14,$15,
			$16,$17,$18,
			$19,$20,$21,$22
		)
	`,
		ev.ID, ev.Timestamp, ev.DurationMS, ev.RequestID, ev.SessionID,
		ev.UserSubject, ev.UserEmail, ev.AuthType, ev.APIKeyName,
		ev.ToolName, ev.ToolGroup, paramsJSON,
		ev.Success, ev.ErrorMessage, ev.ErrorCategory,
		ev.RequestChars, ev.ResponseChars, ev.ContentBlocks,
		ev.Transport, ev.Source, ev.RemoteAddr, ev.UserAgent,
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

// Query returns matching events. ORDER is by ts DESC unless f.OrderDesc is false.
func (s *Store) Query(ctx context.Context, f audit.QueryFilter) ([]audit.Event, error) {
	q, args := buildSelect(f, false)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []audit.Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// Count returns the number of matching events without paging.
func (s *Store) Count(ctx context.Context, f audit.QueryFilter) (int64, error) {
	q, args := buildSelect(f, true)
	var n int64
	if err := s.pool.QueryRow(ctx, q, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// maxTimeSeriesBuckets caps the (to-from)/bucket count so a degenerate
// query like "from=2000, to=2030, bucket=1s" can't force Postgres to
// materialize billions of rows.
const maxTimeSeriesBuckets = 5000

// TimeSeries buckets events by interval. Uses date_trunc-like math via
// interval arithmetic so we can support arbitrary bucket sizes (e.g. 5m).
// Rejects requests whose bucket count would exceed maxTimeSeriesBuckets.
func (s *Store) TimeSeries(ctx context.Context, from, to time.Time, bucket time.Duration) ([]audit.TimePoint, error) {
	if bucket <= 0 {
		bucket = time.Minute
	}
	// Bound the bucket count when both from and to are set.
	if !from.IsZero() && !to.IsZero() {
		span := to.Sub(from)
		if span > 0 && bucket > 0 {
			if span/bucket > maxTimeSeriesBuckets {
				return nil, fmt.Errorf("requested time series exceeds %d buckets", maxTimeSeriesBuckets)
			}
		}
	}
	q := `
		SELECT
			to_timestamp(floor(extract(epoch from ts) / $1::float8) * $1::float8) AT TIME ZONE 'UTC' AS bucket,
			count(*) AS n,
			count(*) FILTER (WHERE NOT success) AS errors,
			COALESCE(avg(duration_ms), 0) AS avg_dur
		FROM audit_events
		WHERE ($2::timestamptz IS NULL OR ts >= $2)
		  AND ($3::timestamptz IS NULL OR ts <= $3)
		GROUP BY bucket
		ORDER BY bucket ASC
	`
	bucketSecs := bucket.Seconds()
	rows, err := s.pool.Query(ctx, q, bucketSecs, nullableTime(from), nullableTime(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []audit.TimePoint
	for rows.Next() {
		var p audit.TimePoint
		if err := rows.Scan(&p.Time, &p.Count, &p.Errors, &p.AvgDurMS); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Breakdown groups events by the requested dimension.
func (s *Store) Breakdown(ctx context.Context, from, to time.Time, dimension string) ([]audit.BreakdownPoint, error) {
	col := breakdownColumn(dimension)
	if col == "" {
		return nil, nil
	}
	q := fmt.Sprintf(`
		SELECT COALESCE(%s::text, '') AS k,
		       count(*) AS n,
		       count(*) FILTER (WHERE NOT success) AS errors
		FROM audit_events
		WHERE ($1::timestamptz IS NULL OR ts >= $1)
		  AND ($2::timestamptz IS NULL OR ts <= $2)
		GROUP BY k
		ORDER BY n DESC
		LIMIT 50
	`, col)
	rows, err := s.pool.Query(ctx, q, nullableTime(from), nullableTime(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []audit.BreakdownPoint
	for rows.Next() {
		var p audit.BreakdownPoint
		if err := rows.Scan(&p.Key, &p.Count, &p.Errors); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Stats returns dashboard summary metrics.
func (s *Store) Stats(ctx context.Context, from, to time.Time) (audit.Stats, error) {
	q := `
		SELECT
			count(*) AS total,
			count(*) FILTER (WHERE NOT success) AS errors,
			COALESCE(avg(duration_ms), 0) AS avg_dur,
			COALESCE(percentile_cont(0.5)  WITHIN GROUP (ORDER BY duration_ms), 0)::bigint AS p50,
			COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::bigint AS p95,
			count(DISTINCT user_subject) FILTER (WHERE user_subject <> '') AS unique_subjects,
			count(DISTINCT tool_name) AS unique_tools
		FROM audit_events
		WHERE ($1::timestamptz IS NULL OR ts >= $1)
		  AND ($2::timestamptz IS NULL OR ts <= $2)
	`
	var s2 audit.Stats
	err := s.pool.QueryRow(ctx, q, nullableTime(from), nullableTime(to)).Scan(
		&s2.Total, &s2.Errors, &s2.AvgDurationMS, &s2.P50DurationMS, &s2.P95DurationMS,
		&s2.UniqueSubjects, &s2.UniqueTools,
	)
	if err != nil {
		return s2, err
	}
	if s2.Total > 0 {
		s2.ErrorRate = float64(s2.Errors) / float64(s2.Total)
	}
	return s2, nil
}

// escapeLike escapes the LIKE/ILIKE meta-characters so user-supplied
// search terms can't expand to wildcards. Postgres uses `\` as the LIKE
// escape character by default.
func escapeLike(s string) string {
	if s == "" {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	// Cap at 200 chars; longer terms are operator typos or attempts to
	// thrash the planner.
	if len(s) > 200 {
		s = s[:200]
	}
	return r.Replace(s)
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func breakdownColumn(dim string) string {
	switch dim {
	case "tool":
		return "tool_name"
	case "user":
		return "user_subject"
	case "success":
		return "CASE WHEN success THEN 'success' ELSE 'error' END"
	case "auth_type":
		return "auth_type"
	}
	return ""
}

func buildSelect(f audit.QueryFilter, count bool) (string, []any) {
	var (
		conds []string
		args  []any
		i     = 1
	)
	add := func(cond string, v any) {
		conds = append(conds, fmt.Sprintf(cond, i))
		args = append(args, v)
		i++
	}
	if !f.From.IsZero() {
		add("ts >= $%d", f.From)
	}
	if !f.To.IsZero() {
		add("ts <= $%d", f.To)
	}
	if f.ToolName != "" {
		add("tool_name = $%d", f.ToolName)
	}
	if f.UserID != "" {
		add("user_subject = $%d", f.UserID)
	}
	if f.SessionID != "" {
		add("session_id = $%d", f.SessionID)
	}
	if f.Success != nil {
		add("success = $%d", *f.Success)
	}
	if f.Search != "" {
		conds = append(conds, fmt.Sprintf(
			"(tool_name ILIKE $%d OR error_message ILIKE $%d OR user_subject ILIKE $%d)",
			i, i+1, i+2,
		))
		// LIKE meta-characters in user input would otherwise let a single `%`
		// match the entire table and double our DoS surface; escape them
		// before wrapping with our own `%...%`.
		like := "%" + escapeLike(f.Search) + "%"
		args = append(args, like, like, like)
		i += 3
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	if count {
		return "SELECT count(*) FROM audit_events" + where, args
	}

	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	args = append(args, limit, f.Offset)
	return "SELECT id, ts, duration_ms, " +
		"COALESCE(request_id, ''), COALESCE(session_id, ''), " +
		"COALESCE(user_subject, ''), COALESCE(user_email, ''), " +
		"COALESCE(auth_type, ''), COALESCE(api_key_name, ''), " +
		"tool_name, COALESCE(tool_group, ''), parameters, " +
		"success, COALESCE(error_message, ''), COALESCE(error_category, ''), " +
		"request_chars, response_chars, content_blocks, " +
		"transport, source, COALESCE(remote_addr, ''), COALESCE(user_agent, '') " +
		"FROM audit_events" + where +
		" ORDER BY ts DESC" +
		fmt.Sprintf(" LIMIT $%d OFFSET $%d", i, i+1), args
}

func scanEvent(row pgx.Row) (audit.Event, error) {
	var (
		ev      audit.Event
		paramsB []byte
	)
	err := row.Scan(
		&ev.ID, &ev.Timestamp, &ev.DurationMS, &ev.RequestID, &ev.SessionID,
		&ev.UserSubject, &ev.UserEmail, &ev.AuthType, &ev.APIKeyName,
		&ev.ToolName, &ev.ToolGroup, &paramsB,
		&ev.Success, &ev.ErrorMessage, &ev.ErrorCategory,
		&ev.RequestChars, &ev.ResponseChars, &ev.ContentBlocks,
		&ev.Transport, &ev.Source, &ev.RemoteAddr, &ev.UserAgent,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ev, err
		}
		return ev, err
	}
	if len(paramsB) > 0 {
		_ = json.Unmarshal(paramsB, &ev.Parameters)
	}
	return ev, nil
}
