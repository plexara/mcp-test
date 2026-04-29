package mcpmw

import "encoding/json"

// jsonImpl is the actual JSON unmarshal; wrapped so jsonUnmarshal in audit.go
// can stay simple while keeping encoding/json out of the test surface.
func jsonImpl(data []byte, v any) error { return json.Unmarshal(data, v) }
