package models

import (
	"strings"

	"github.com/addp/common/dataprotection"
)

const QueryProtectionChoicesKey = "protection_choices"

// ProtectionChoices is strict: a malformed saved reference is never ignored.
func (q *QueryService) ProtectionChoices() ([]dataprotection.FieldRoleChoice, error) {
	if q == nil || q.DataConfig == nil {
		return nil, nil
	}
	raw, exists := q.DataConfig[QueryProtectionChoicesKey]
	if !exists {
		return nil, nil
	}
	return dataprotection.DecodeFieldRoleChoices(raw)
}

type QueryProtectionOptionsResponse struct {
	Fields []dataprotection.FieldRoleOptions `json:"fields"`
}

// QueryProtectionOptionsRequest identifies one explicit source; SQL stays out of URLs.
type QueryProtectionOptionsRequest struct {
	ConfigType    string         `json:"config_type"`
	TargetLocator string         `json:"target_locator,omitempty"`
	EngineID      uint           `json:"engine_id,omitempty"`
	SQL           string         `json:"sql,omitempty"`
	Parameters    map[string]any `json:"parameters,omitempty" swaggertype:"object"`
}

func (r *QueryProtectionOptionsRequest) Valid() bool {
	if r == nil {
		return false
	}
	switch r.ConfigType {
	case "table":
		return strings.TrimSpace(r.TargetLocator) != "" && r.EngineID == 0 && r.SQL == "" && r.Parameters == nil
	case "sql":
		return r.TargetLocator == "" && r.EngineID > 0 && strings.TrimSpace(r.SQL) != ""
	default:
		return false
	}
}
