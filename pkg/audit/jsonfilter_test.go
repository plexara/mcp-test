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
		{"int64", 3, int64(3), true},
		{"int32", 3, int32(3), true},
		{"int", 3, 3, true},
		{"uint", 3, uint(3), true},
		{"uint32", 3, uint32(3), true},
		{"uint64", 3, uint64(3), true},
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
