package models

import (
	"bytes"
	"encoding/json"
	"github.com/addp/common/dataprotection"
	"io"
)

type QueryProtectionOptionsResponse struct {
	Fields []dataprotection.FieldRoleOptions `json:"fields"`
}

func QueryProtectionChoices(content map[string]interface{}) ([]dataprotection.FieldRoleChoice, error) {
	raw, exists := content["protection_choices"]
	if !exists {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	if len(encoded) > 128<<10 {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	var choices []dataprotection.FieldRoleChoice
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&choices) != nil || choices == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	if err := dataprotection.ValidateFieldRoleChoices(choices); err != nil {
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
