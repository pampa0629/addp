package models

import (
	"github.com/addp/common/dataprotection"
)

type QueryProtectionOptionsResponse struct {
	Fields []dataprotection.FieldRoleOptions `json:"fields"`
}

func QueryProtectionChoices(content map[string]interface{}) ([]dataprotection.FieldRoleChoice, error) {
	raw, exists := content["protection_choices"]
	if !exists {
		return nil, nil
	}
	choices, err := dataprotection.DecodeFieldRoleChoices(raw)
	if err != nil {
		return nil, err
	}
	if len(choices) > 0 {
		kind, _ := content["query_type"].(string)
		if kind != "" && kind != "sql" && kind != "mql" {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
	}
	return choices, nil
}

func QueryExportProtectionChoices(content map[string]interface{}) ([]dataprotection.FieldRoleChoice, error) {
	raw, exists := content["export_protection_choices"]
	if !exists {
		return nil, nil
	}
	return dataprotection.DecodeFieldRoleChoices(raw)
}
