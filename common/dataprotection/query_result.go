package dataprotection

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/addp/common/engine/plugin"
)

type AuthorizedQueryReadResult struct {
	plugin.QueryResult
	// Retained contains only still-authorized originals; expired bindings are irreversibly removed.
	Retained           *AuthorizedQueryResult `json:"-"`
	PlaintextExpiresAt *time.Time
}

// AuthorizedQueryResult is ephemeral and owner-private. Result contains plaintext only for
// fields authorized at execution; all other fields already have default or explicitly selected masking.
// Sources bind those plaintext outputs to provider-owned, frozen field lineage.
// This value must never be persisted or returned in an execution observation or generic DTO.
type AuthorizedQueryResult struct {
	RoleChoices []FieldRoleChoice       `json:"role_choices,omitempty"`
	TenantID    int64                   `json:"tenant_id"`
	Subject     SubjectReference        `json:"subject"`
	Result      plugin.QueryResult      `json:"result"`
	Sources     []AuthorizedQuerySource `json:"sources"`
	Truncated   bool                    `json:"truncated"`
}

type AuthorizedQuerySource struct {
	Target       ResourceReference        `json:"target"`
	Source       plugin.QueryOutputSource `json:"source"`
	DefaultRules []Rule                   `json:"default_rules"`
}

// CloneQueryResult also detaches nested documents and arrays. UseNumber avoids
// rounding integer/decimal JSON values while detaching query result values.
func CloneQueryResult(result *plugin.QueryResult) (*plugin.QueryResult, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var clone plugin.QueryResult
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&clone); err != nil {
		return nil, err
	}
	return &clone, nil
}
