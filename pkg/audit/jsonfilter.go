package audit

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ParseJSONFilterValue type-detects a URL-supplied string into a Go
// value suitable for JSON containment matching.
//
// Detection order: bool ("true"/"false"), int64, float64, plain string.
// Quoted forms (`"42"`) force string. This keeps `?response.isError=true`
// matching JSON true rather than the string "true", while letting an
// operator who actually wants the string write `?param.code="200"`.
func ParseJSONFilterValue(s string) any {
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	// Quoted force-string: "abc" -> abc as string.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// JSONFilterToContainment turns Path=[a,b,c], Value=v into the JSON
// document {"a": {"b": {"c": v}}} suitable for Postgres @> containment
// and for the MemoryLogger's nested map walk. Empty Path returns just v.
func JSONFilterToContainment(path []string, value any) any {
	if len(path) == 0 {
		return value
	}
	cur := any(value)
	for i := len(path) - 1; i >= 0; i-- {
		cur = map[string]any{path[i]: cur}
	}
	return cur
}

// MatchJSONPath reports whether m contains a value at the given dotted
// path equal to want, using the same type detection as
// ParseJSONFilterValue. Used by MemoryLogger's filter loop. Comparison
// is JSON-semantic (number widens int<->float; bool exact; string exact).
func MatchJSONPath(m map[string]any, path []string, want any) bool {
	if len(path) == 0 {
		return jsonEqual(m, want)
	}
	cur := any(m)
	for _, seg := range path {
		next, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		v, present := next[seg]
		if !present {
			return false
		}
		cur = v
	}
	return jsonEqual(cur, want)
}

// jsonEqual returns true when a and b are equal under JSON semantics.
// Numbers widen across int / float; strings, bools, and exact equality
// otherwise. Maps and slices are compared via deep marshal-then-equal,
// which is rare for filter values (almost always scalars).
func jsonEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	switch av := a.(type) {
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case float64:
		return numericEq(av, b)
	case int64:
		return numericEq(float64(av), b)
	case int:
		return numericEq(float64(av), b)
	}
	// Fall back to JSON round-trip for compound types.
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func numericEq(a float64, b any) bool {
	switch bv := b.(type) {
	case float64:
		return a == bv
	case float32:
		return a == float64(bv)
	case int8:
		return a == float64(bv)
	case int16:
		return a == float64(bv)
	case int32:
		return a == float64(bv)
	case int64:
		return a == float64(bv)
	case int:
		return a == float64(bv)
	case uint8:
		return a == float64(bv)
	case uint16:
		return a == float64(bv)
	case uint32:
		return a == float64(bv)
	case uint64:
		// Lossy at very large values; for filter-equality this is the
		// best we can do without bigint, and audit numbers are nowhere
		// near 2^53.
		return a == float64(bv)
	case uint:
		return a == float64(bv)
	case json.Number:
		bf, _ := bv.Float64()
		return a == bf
	}
	return false
}

// IsAllowedHasKey reports whether key is an allowlisted has= column.
// Implemented as a closed switch (not a slice iteration) so the
// AllowedHasKeys exported var cannot be mutated by an importing package
// to widen what gets spliced into the verbatim SQL column reference in
// buildSelect. The slice stays exported for documentation generators
// and reflection callers; the gate is the function.
func IsAllowedHasKey(key string) bool {
	switch key {
	case "request_params",
		"request_headers",
		"response_result",
		"response_error",
		"notifications",
		"replayed_from":
		return true
	}
	return false
}

// IsAllowedJSONSource reports whether src is an allowlisted source.
// Closed switch for the same reason as IsAllowedHasKey: defense against
// mutation of the exported AllowedJSONSources slice.
func IsAllowedJSONSource(src string) bool {
	switch src {
	case "param", "response", "header":
		return true
	}
	return false
}

// SplitJSONPath splits a dotted path "a.b.c" into ["a","b","c"]. Empty
// input returns a nil slice. A trailing/leading dot is treated as an
// empty segment, which won't match anything; that's fine since the
// HTTP layer rejects empty segments before this is called.
func SplitJSONPath(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ".")
}
