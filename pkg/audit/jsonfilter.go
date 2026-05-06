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

// JSONFilterToBytes marshals JSONFilterToContainment(path, value) to
// JSON bytes, suitable to pass as a $N::jsonb argument.
func JSONFilterToBytes(path []string, value any) ([]byte, error) {
	return json.Marshal(JSONFilterToContainment(path, value))
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
	case int64:
		return a == float64(bv)
	case int:
		return a == float64(bv)
	case json.Number:
		bf, _ := bv.Float64()
		return a == bf
	}
	return false
}

// IsAllowedHasKey reports whether key is one of AllowedHasKeys.
func IsAllowedHasKey(key string) bool {
	for _, k := range AllowedHasKeys {
		if k == key {
			return true
		}
	}
	return false
}

// IsAllowedJSONSource reports whether src is one of AllowedJSONSources.
func IsAllowedJSONSource(src string) bool {
	for _, s := range AllowedJSONSources {
		if s == src {
			return true
		}
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
