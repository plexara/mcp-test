package audit

import (
	"encoding/json"
	"testing"
)

func TestEvent_WithPayload_Roundtrip(t *testing.T) {
	ev := NewEvent("echo")
	if ev.Payload != nil {
		t.Fatal("fresh Event should have nil Payload")
	}
	p := &Payload{
		JSONRPCMethod:    "tools/call",
		RequestParams:    map[string]any{"message": "hi"},
		RequestSizeBytes: 17,
	}
	ev.WithPayload(p)
	if ev.Payload != p {
		t.Errorf("WithPayload didn't set the field")
	}
	ev.WithPayload(nil)
	if ev.Payload != nil {
		t.Errorf("WithPayload(nil) didn't clear")
	}
}

func TestPayload_JSONShape(t *testing.T) {
	p := &Payload{
		JSONRPCMethod:    "tools/call",
		RequestParams:    map[string]any{"k": "v"},
		RequestSizeBytes: 12,
		RequestTruncated: false,
		ResponseResult: map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "hi"},
			},
			"isError": false,
		},
		ResponseSizeBytes: 50,
		Notifications: []Notification{
			{Method: "notifications/progress", Params: map[string]any{"step": 1}},
		},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{
		"jsonrpc_method",
		"request_params", "request_size_bytes",
		"response_result", "response_size_bytes",
		"notifications",
	} {
		if _, ok := round[k]; !ok {
			t.Errorf("payload JSON missing %q key", k)
		}
	}
	// Truncated flags omitted when false.
	if _, ok := round["request_truncated"]; ok {
		t.Errorf("request_truncated:false should be omitempty")
	}
}

func TestEvent_PayloadOmittedWhenNil(t *testing.T) {
	ev := NewEvent("echo")
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	_ = json.Unmarshal(b, &round)
	if _, ok := round["payload"]; ok {
		t.Errorf("payload key should be omitted when nil; got %s", b)
	}
}
