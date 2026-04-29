package mcpmw

import "testing"

func TestJSONImpl(t *testing.T) {
	var out map[string]int
	if err := jsonImpl([]byte(`{"a":1}`), &out); err != nil {
		t.Fatal(err)
	}
	if out["a"] != 1 {
		t.Errorf("a = %d, want 1", out["a"])
	}
	if err := jsonImpl([]byte(`bad json`), &out); err == nil {
		t.Error("expected unmarshal error")
	}
}
