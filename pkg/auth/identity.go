// Package auth defines identity, authenticators, and request-context helpers.
package auth

// Identity describes the authenticated caller for a single MCP or HTTP request.
type Identity struct {
	Subject  string         `json:"subject"`
	Email    string         `json:"email,omitempty"`
	Name     string         `json:"name,omitempty"`
	AuthType string         `json:"auth_type"` // "oidc" | "apikey" | "anonymous"
	Claims   map[string]any `json:"claims,omitempty"`
	APIKeyID string         `json:"api_key_id,omitempty"`
}

// Anonymous returns the identity used when allow_anonymous is true and no
// credentials are presented.
func Anonymous() *Identity {
	return &Identity{Subject: "anonymous", AuthType: "anonymous"}
}
