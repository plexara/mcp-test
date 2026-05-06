// Package auditpg provides a pgx-backed implementation of audit.Logger.
package auditpg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
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

// Log inserts a single event. When ev.Payload is non-nil, the matching
// row is also inserted into audit_payloads in the same transaction so
// the summary and detail are committed atomically. A failure on either
// rolls back both; the async drain treats the pair as a single drop.
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

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	// Roll back if we don't reach the commit; safe no-op after commit.
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
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

	if ev.Payload != nil {
		if err := insertPayload(ctx, tx, ev.ID, ev.Payload); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit audit event: %w", err)
	}
	return nil
}

// insertPayload writes the audit_payloads row for an event. Caller must
// hold an open tx; this function never commits or rolls back on its own.
func insertPayload(ctx context.Context, tx pgx.Tx, eventID string, p *audit.Payload) error {
	requestParams, err := marshalJSONB(p.RequestParams)
	if err != nil {
		return fmt.Errorf("marshal request_params: %w", err)
	}
	requestHeaders, err := marshalJSONB(p.RequestHeaders)
	if err != nil {
		return fmt.Errorf("marshal request_headers: %w", err)
	}
	responseResult, err := marshalJSONB(p.ResponseResult)
	if err != nil {
		return fmt.Errorf("marshal response_result: %w", err)
	}
	responseError, err := marshalJSONB(p.ResponseError)
	if err != nil {
		return fmt.Errorf("marshal response_error: %w", err)
	}
	notifications, err := marshalJSONB(p.Notifications)
	if err != nil {
		return fmt.Errorf("marshal notifications: %w", err)
	}
	var replayedFrom any
	if p.ReplayedFrom != "" {
		replayedFrom = p.ReplayedFrom
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO audit_payloads (
			event_id,
			jsonrpc_method,
			request_params, request_size_bytes, request_truncated,
			request_headers, request_remote_addr,
			response_result, response_error, response_size_bytes, response_truncated,
			notifications, notifications_truncated, replayed_from
		) VALUES (
			$1,
			$2,
			$3, $4, $5,
			$6, $7,
			$8, $9, $10, $11,
			$12, $13, $14
		)
	`,
		eventID,
		p.JSONRPCMethod,
		requestParams, p.RequestSizeBytes, p.RequestTruncated,
		requestHeaders, p.RequestRemoteAddr,
		responseResult, responseError, p.ResponseSizeBytes, p.ResponseTruncated,
		notifications, p.NotificationsTruncated, replayedFrom,
	)
	if err != nil {
		return fmt.Errorf("insert audit payload: %w", err)
	}
	return nil
}

// marshalJSONB returns the JSON encoding of v, or nil for nil-or-empty
// inputs so the column stores SQL NULL (which is friendlier to JSONB
// queries than a literal "null"). Empty-detection uses reflection so it
// handles every map / slice / array type uniformly: no fragile type
// switch to maintain as Payload grows.
func marshalJSONB(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	if isEmptyValue(v) {
		return nil, nil
	}
	return json.Marshal(v)
}

// isEmptyValue reports whether v is the zero-length form of a
// container type (map, slice, array, channel) or a nil pointer.
// Anything else is considered non-empty.
func isEmptyValue(v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Chan, reflect.String:
		return rv.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// GetPayload returns the audit_payloads row for the given event, or
// (nil, nil) if no payload was captured. Errors other than "no rows" are
// returned. Per-column JSON unmarshal failures (corrupt JSONB) are
// logged at WARN and the field stays empty so the rest of the payload
// can still render.
func (s *Store) GetPayload(ctx context.Context, eventID string) (*audit.Payload, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(jsonrpc_method, ''),
			request_params,
			request_size_bytes,
			request_truncated,
			request_headers,
			COALESCE(request_remote_addr, ''),
			response_result,
			response_error,
			response_size_bytes,
			response_truncated,
			notifications,
			notifications_truncated,
			COALESCE(replayed_from, '')
		FROM audit_payloads
		WHERE event_id = $1
	`, eventID)

	var (
		p              audit.Payload
		paramsB        []byte
		headersB       []byte
		resultB        []byte
		errB           []byte
		notificationsB []byte
	)
	err := row.Scan(
		&p.JSONRPCMethod,
		&paramsB,
		&p.RequestSizeBytes,
		&p.RequestTruncated,
		&headersB,
		&p.RequestRemoteAddr,
		&resultB,
		&errB,
		&p.ResponseSizeBytes,
		&p.ResponseTruncated,
		&notificationsB,
		&p.NotificationsTruncated,
		&p.ReplayedFrom,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	unmarshalCol(eventID, "request_params", paramsB, &p.RequestParams)
	unmarshalCol(eventID, "request_headers", headersB, &p.RequestHeaders)
	unmarshalCol(eventID, "response_result", resultB, &p.ResponseResult)
	unmarshalCol(eventID, "response_error", errB, &p.ResponseError)
	unmarshalCol(eventID, "notifications", notificationsB, &p.Notifications)
	return &p, nil
}

// unmarshalCol decodes a JSONB column into dest. Empty input is a no-op.
// Decode failures (corrupt JSONB written by a buggy older binary) are
// logged at WARN with the event ID + column name so operators can spot
// them; the field stays empty so the rest of the payload still renders.
func unmarshalCol(eventID, column string, data []byte, dest any) {
	if len(data) == 0 {
		return
	}
	if err := json.Unmarshal(data, dest); err != nil {
		slog.Warn("audit: corrupt JSONB column",
			"event_id", eventID,
			"column", column,
			"err", err,
			"bytes", len(data),
		)
	}
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

// Stream iterates the filtered event set via the same SELECT used by
// Query, calling fn once per row. This avoids buffering the full set
// in memory for export endpoints; pgx itself uses a network-level
// cursor under the hood.
//
// Stream uses audit.MaxQueryLimit as the page size and walks pages with
// f.Offset until a page returns fewer rows. Sharing one constant with
// buildSelect avoids a silent reset to the much smaller default if
// either side moves.
//
// Concurrent-insert caveat: ORDER BY ts DESC + OFFSET pagination is
// unstable when new rows arrive at the head between pages, since each
// new row shifts every later row down by one offset slot. Under heavy
// concurrent writes during a long export, a row at the page boundary
// can appear in two consecutive pages. Consumers that care should
// dedupe by Event.ID; a future revision can switch to a ts cursor
// (WHERE ts < $cursor) for a true snapshot guarantee.
func (s *Store) Stream(ctx context.Context, f audit.QueryFilter, fn func(audit.Event) error) error {
	// Per the StreamingLogger contract f.Limit and f.Offset are
	// ignored; Stream iterates the whole filtered set and uses an
	// internal page cursor (page.Offset) for pagination.
	page := f
	page.Limit = audit.MaxQueryLimit
	page.Offset = 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		q, args := buildSelect(page, false)
		rows, err := s.pool.Query(ctx, q, args...)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			ev, err := scanEvent(rows)
			if err != nil {
				rows.Close()
				return err
			}
			if err := fn(ev); err != nil {
				rows.Close()
				return err
			}
			count++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if count < audit.MaxQueryLimit {
			return nil
		}
		page.Offset += audit.MaxQueryLimit
	}
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

// truncateForLog returns s capped at max bytes, with an ellipsis
// suffix when truncated. Used by the JSON-filter compile-error log so
// a malformed path doesn't blow out the slog line.
func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// jsonFilterColumn maps a QueryFilter.JSONPathFilter Source into the
// audit_payloads JSONB column name. Unknown sources return "" and are
// skipped at the call site.
func jsonFilterColumn(source string) string {
	switch source {
	case "param":
		return "request_params"
	case "response":
		return "response_result"
	case "header":
		return "request_headers"
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
	if f.EventID != "" {
		add("id = $%d", f.EventID)
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

	// JSON path filters: each compiles to an EXISTS subquery against
	// audit_payloads.<column> @> $N::jsonb. The containment doc is built
	// from the dotted path so a single ?param.user.id=alice produces
	// {"user":{"id":"alice"}}, which the GIN(jsonb_path_ops) index can
	// answer cheaply. Source has already been validated against the
	// closed AllowedJSONSources set at the HTTP layer.
	for _, jf := range f.JSONFilters {
		col := jsonFilterColumn(jf.Source)
		if col == "" {
			continue
		}
		var doc any
		if jf.Source == "header" {
			// Headers are stored as {"User-Agent":["curl/8.0"]}: the
			// containment doc wraps the value in an array at the leaf.
			// Header values are always strings on the wire; force the
			// raw value rather than running it through ParseJSONFilterValue
			// so ?header.X-Count=42 matches a stored "42" not JSON 42.
			if len(jf.Path) == 0 {
				continue
			}
			doc = map[string]any{
				jf.Path[0]: []any{jf.Value},
			}
		} else {
			doc = audit.JSONFilterToContainment(jf.Path, audit.ParseJSONFilterValue(jf.Value))
		}
		b, err := json.Marshal(doc)
		if err != nil {
			// Silently dropping the filter would broaden the result
			// set without telling the operator. Log and skip; the
			// filter inputs are constructed from sanitized URL parts
			// plus ParseJSONFilterValue output, so this branch should
			// be unreachable in practice. We log the joined path
			// (capped) so an operator can correlate against the
			// failing request.
			slog.Warn("audit: json filter compile failed; filter dropped",
				"source", jf.Source,
				"path", truncateForLog(strings.Join(jf.Path, "."), 256),
				"err", err,
			)
			continue
		}
		conds = append(conds, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM audit_payloads p WHERE p.event_id = audit_events.id AND p.%s @> $%d::jsonb)",
			col, i,
		))
		args = append(args, string(b))
		i++
	}

	// has= filters: shorthand for "this payload column is non-empty".
	// We treat NULL, empty-JSONB forms ('{}'::jsonb, '[]'::jsonb,
	// 'null'::jsonb), and empty TEXT ('') as "missing"; only a payload
	// column with actual content counts. This matches MemoryLogger's
	// len/IsZero-based check on every allowlisted column type
	// (request_* / response_* / notifications are JSONB; replayed_from
	// is TEXT) so the two backends agree on edge cases.
	//
	// The empty-string entry covers the TEXT column case (replayed_from)
	// where '' is a valid stored value but semantically "missing." For
	// JSONB columns, '' isn't a valid stored value so the entry is a
	// harmless extra match.
	//
	// Validated against AllowedHasKeys at the HTTP layer; the column
	// name is allow-listed so the verbatim splice is safe. Note: this
	// loop deliberately does NOT increment `i`; no $N placeholder is
	// bound here. A future maintainer adding a bound argument must
	// increment `i` to keep LIMIT/OFFSET aligned.
	for _, k := range f.HasKeys {
		if !audit.IsAllowedHasKey(k) {
			continue
		}
		conds = append(conds, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM audit_payloads p WHERE p.event_id = audit_events.id "+
				"AND p.%s IS NOT NULL "+
				"AND p.%s::text NOT IN ('{}', '[]', 'null', ''))",
			k, k,
		))
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	if count {
		return "SELECT count(*) FROM audit_events" + where, args
	}

	// Two distinct cases, kept separate so the doc on audit.MaxQueryLimit
	// ("larger values get silently reduced") is honored. A caller asking
	// for too much is clamped to MaxQueryLimit, not collapsed back to the
	// page-size default; an unset limit gets the page-size default.
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > audit.MaxQueryLimit {
		limit = audit.MaxQueryLimit
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
		// id ASC is the tiebreaker for tied timestamps. Without it,
		// two events with the same ts can swap positions between pages
		// of a Stream walk, causing both duplicate emission and missed
		// emission across page boundaries. Tied ts is common under any
		// sub-millisecond write rate.
		" ORDER BY ts DESC, id ASC" +
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
