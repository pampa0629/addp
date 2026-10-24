package models

import "github.com/addp/common/dataprotection"

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
