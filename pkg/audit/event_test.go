package audit

import "testing"

func TestSanitizeParameters_TopLevel(t *testing.T) {
	in := map[string]any{
		"username": "alice",
		"password": "p@ss",
		"api_key":  "AKIA...",
	}
	out := SanitizeParameters(in, []string{"password", "api_key"})
	if out["username"] != "alice" {
		t.Errorf("username got %v", out["username"])
	}
	if out["password"] != "[redacted]" {
		t.Errorf("password not redacted: %v", out["password"])
	}
	if out["api_key"] != "[redacted]" {
		t.Errorf("api_key not redacted: %v", out["api_key"])
	}
}

func TestSanitizeParameters_Nested(t *testing.T) {
	in := map[string]any{
		"outer": map[string]any{
			"Token":  "xyz",
			"data":   42,
			"nested": []any{map[string]any{"Secret": "shh", "ok": "v"}},
		},
	}
	out := SanitizeParameters(in, []string{"token", "secret"})
	outer := out["outer"].(map[string]any)
	if outer["Token"] != "[redacted]" {
		t.Errorf("nested Token not redacted: %v", outer["Token"])
	}
	if outer["data"] != 42 {
		t.Errorf("data should pass through, got %v", outer["data"])
	}
	nested := outer["nested"].([]any)
	first := nested[0].(map[string]any)
	if first["Secret"] != "[redacted]" {
		t.Errorf("array-elem Secret not redacted: %v", first["Secret"])
	}
	if first["ok"] != "v" {
		t.Errorf("array-elem ok lost: %v", first["ok"])
	}
}
