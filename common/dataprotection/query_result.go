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
	for i, row := range result.Rows {
		if row != nil {
			restoreQueryBytes(row, clone.Rows[i])
		}
	}
	return &clone, nil
}

// JSON detaches ordinary values and preserves exact numbers, but erases byte
// types. Restore independently owned bytes from the original value tree;
// masked strings are never decoded or replaced with originals.
func restoreQueryBytes(original, cloned interface{}) interface{} {
	switch value := original.(type) {
	case []byte:
		if value == nil {
			return []byte(nil)
		}
		return append([]byte{}, value...)
	case map[string]interface{}:
		copy, ok := cloned.(map[string]interface{})
		if ok {
			for key, item := range value {
				copy[key] = restoreQueryBytes(item, copy[key])
			}
		}
	case []interface{}:
		copy, ok := cloned.([]interface{})
		if ok {
			for i, item := range value {
				copy[i] = restoreQueryBytes(item, copy[i])
			}
		}
	}
	return cloned
}
