package audit

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseJSONFilterValue(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{"true", true},
		{"false", false},
		{"42", int64(42)},
		{"-7", int64(-7)},
		{"3.14", 3.14},
		{`"42"`, "42"},         // forced string
		{`"true"`, "true"},     // forced string
		{`"hello"`, "hello"},   // forced string
		{"hello", "hello"},     // plain string
		{"", ""},               // empty stays empty (HTTP layer should reject)
		{"alice@x", "alice@x"}, // @ is not numeric, stays string
	}
	for _, c := range cases {
		got := ParseJSONFilterValue(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseJSONFilterValue(%q) = %v (%T), want %v (%T)",
				c.in, got, got, c.want, c.want)
		}
	}
}

func TestJSONFilterToContainment(t *testing.T) {
	cases := []struct {
		path  []string
		value any
		want  any
	}{
		{nil, "v", "v"},
		{[]string{}, 42, 42},
		{[]string{"a"}, "v", map[string]any{"a": "v"}},
		{[]string{"a", "b"}, true, map[string]any{"a": map[string]any{"b": true}}},
		{[]string{"x", "y", "z"}, int64(1),
			map[string]any{"x": map[string]any{"y": map[string]any{"z": int64(1)}}}},
	}
	for _, c := range cases {
		got := JSONFilterToContainment(c.path, c.value)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("JSONFilterToContainment(%v, %v) = %v, want %v",
				c.path, c.value, got, c.want)
		}
	}
}

func TestMatchJSONPath(t *testing.T) {
	doc := map[string]any{
		"top":     "leaf",
		"flag":    true,
		"count":   float64(3),
		"countI":  int64(3),
		"nested":  map[string]any{"deep": map[string]any{"x": "y"}},
		"isError": false,
	}
	cases := []struct {
		path []string
		want any
		ok   bool
	}{
		{[]string{"top"}, "leaf", true},
		{[]string{"top"}, "nope", false},
		{[]string{"flag"}, true, true},
		{[]string{"flag"}, false, false},
		{[]string{"count"}, int64(3), true}, // float<->int widen
		{[]string{"countI"}, float64(3), true},
		{[]string{"nested", "deep", "x"}, "y", true},
		{[]string{"nested", "deep", "x"}, "z", false},
		{[]string{"nested", "missing"}, "y", false},
		{[]string{"isError"}, false, true},
		{[]string{"isError"}, true, false},
	}
	for _, c := range cases {
		got := MatchJSONPath(doc, c.path, c.want)
		if got != c.ok {
			t.Errorf("MatchJSONPath(%v, %v) = %v, want %v",
				c.path, c.want, got, c.ok)
		}
	}
}

func TestMatchJSONPath_NonMapTraversal(t *testing.T) {
	// A path that descends past a non-map value should fail rather than
	// panic. Operators have no way to send such a path today (the HTTP
	// layer rejects before we get here), but the helper is conservative.
	doc := map[string]any{"a": "scalar"}
	if MatchJSONPath(doc, []string{"a", "b"}, "scalar") {
		t.Error("expected false: cannot descend past scalar")
	}
}

func TestNumericEq_TypeSurface(t *testing.T) {
	// numericEq compares the float64 left side against arbitrary right
	// types. Cover each case so a future signature change can't drop a
	// type silently.
	cases := []struct {
		name string
		a    float64
		b    any
		want bool
	}{
		{"float64 eq", 3, float64(3), true},
		{"float64 ne", 3, float64(4), false},
		{"float32", 3, float32(3), true},
		{"int8", 3, int8(3), true},
		{"int16", 3, int16(3), true},
		{"int32", 3, int32(3), true},
		{"int64", 3, int64(3), true},
		{"int", 3, 3, true},
		{"uint8", 3, uint8(3), true},
		{"uint16", 3, uint16(3), true},
		{"uint32", 3, uint32(3), true},
		{"uint64", 3, uint64(3), true},
		{"uint", 3, uint(3), true},
		{"json.Number", 3.5, json.Number("3.5"), true},
		{"unsupported type", 3, "3", false},
	}
	for _, c := range cases {
		got := numericEq(c.a, c.b)
		if got != c.want {
			t.Errorf("%s: numericEq(%v, %v) = %v, want %v", c.name, c.a, c.b, got, c.want)
		}
	}
}

func TestSplitJSONPath(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a.b.c", []string{"a", "b", "c"}},
		{"User-Agent", []string{"User-Agent"}},
	}
	for _, c := range cases {
		got := SplitJSONPath(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitJSONPath(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIsAllowedHasKey(t *testing.T) {
	for _, k := range AllowedHasKeys {
		if !IsAllowedHasKey(k) {
			t.Errorf("IsAllowedHasKey(%q) = false, want true (in allow list)", k)
		}
	}
	for _, k := range []string{"", "events", "id", "DROP TABLE"} {
		if IsAllowedHasKey(k) {
			t.Errorf("IsAllowedHasKey(%q) = true, want false", k)
		}
	}
}

func TestAllowList_FunctionAndSliceAgree(t *testing.T) {
	// IsAllowedHasKey and IsAllowedJSONSource are implemented as closed
	// switches; AllowedHasKeys and AllowedJSONSources are exported
	// slices for documentation. Both must agree in BOTH directions:
	// every slice entry must be allowed by the function, and the
	// function must not allow anything outside the slice (within a
	// candidate set wide enough to catch typos / additions).
	//
	// The candidate set lists every column that could plausibly be
	// added to has= and every source that could plausibly be added to
	// the JSON-path syntax; if a future change widens the function but
	// forgets the slice (or vice versa), this test surfaces the drift.

	// Has keys.
	for _, k := range AllowedHasKeys {
		if !IsAllowedHasKey(k) {
			t.Errorf("AllowedHasKeys contains %q but IsAllowedHasKey rejects it", k)
		}
	}
	candidateColumns := append([]string{}, AllowedHasKeys...)
	candidateColumns = append(candidateColumns,
		"event_id", "ts", "captured_at", "request_size_bytes",
		"response_size_bytes", "request_truncated", "response_truncated",
		"jsonrpc_method", "jsonrpc_id", "request_method", "request_path",
		"request_remote_addr", "notifications_truncated",
	)
	for _, k := range candidateColumns {
		want := false
		for _, allowed := range AllowedHasKeys {
			if k == allowed {
				want = true
				break
			}
		}
		if got := IsAllowedHasKey(k); got != want {
			t.Errorf("IsAllowedHasKey(%q) = %v, want %v (slice/function disagree)", k, got, want)
		}
	}

	// JSON sources.
	for _, s := range AllowedJSONSources {
		if !IsAllowedJSONSource(s) {
			t.Errorf("AllowedJSONSources contains %q but IsAllowedJSONSource rejects it", s)
		}
	}
	for _, s := range append([]string{}, AllowedJSONSources...) {
		want := false
		for _, a := range AllowedJSONSources {
			if s == a {
				want = true
				break
			}
		}
		if got := IsAllowedJSONSource(s); got != want {
			t.Errorf("IsAllowedJSONSource(%q) = %v, want %v", s, got, want)
		}
	}
	for _, s := range []string{"", "params", "headers", "result", "PARAM", "Param"} {
		if IsAllowedJSONSource(s) {
			t.Errorf("IsAllowedJSONSource(%q) = true, want false", s)
		}
	}

	// AllowedHasKeysList / AllowedJSONSourcesList must mirror the
	// underlying slices exactly and must return a fresh clone each call
	// (a downstream caller mutating the returned slice must not affect
	// the next caller).
	if got := AllowedHasKeysList(); len(got) != len(AllowedHasKeys) {
		t.Fatalf("AllowedHasKeysList len = %d, want %d", len(got), len(AllowedHasKeys))
	}
	for i, k := range AllowedHasKeysList() {
		if k != AllowedHasKeys[i] {
			t.Errorf("AllowedHasKeysList[%d] = %q, want %q (drift vs AllowedHasKeys)", i, k, AllowedHasKeys[i])
		}
	}
	first := AllowedHasKeysList()
	first[0] = "MUTATED"
	if AllowedHasKeysList()[0] == "MUTATED" {
		t.Error("AllowedHasKeysList() returned a shared slice; mutation leaked across callers")
	}
	if AllowedHasKeys[0] == "MUTATED" {
		t.Error("AllowedHasKeysList() returned the package var directly; mutation leaked into AllowedHasKeys")
	}

	if got := AllowedJSONSourcesList(); len(got) != len(AllowedJSONSources) {
		t.Fatalf("AllowedJSONSourcesList len = %d, want %d", len(got), len(AllowedJSONSources))
	}
	for i, s := range AllowedJSONSourcesList() {
		if s != AllowedJSONSources[i] {
			t.Errorf("AllowedJSONSourcesList[%d] = %q, want %q", i, s, AllowedJSONSources[i])
		}
	}
	firstSrc := AllowedJSONSourcesList()
	firstSrc[0] = "MUTATED"
	if AllowedJSONSourcesList()[0] == "MUTATED" {
		t.Error("AllowedJSONSourcesList() returned a shared slice; mutation leaked across callers")
	}
	if AllowedJSONSources[0] == "MUTATED" {
		t.Error("AllowedJSONSourcesList() returned the package var directly; mutation leaked into AllowedJSONSources")
	}
}
