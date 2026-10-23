package models

import (
	"github.com/addp/common/dataprotection"
)

// SourceProtectionChoices preserves invalid or unsupported saved choices as an
// error instead of dropping them when a source or runtime mode is changed.
func SourceProtectionChoices(config map[string]interface{}) ([]dataprotection.FieldRoleChoice, error) {
	if _, exists := config["protection_choices"]; exists {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	if target, ok := config["target"].(map[string]interface{}); ok {
		if _, exists := target["protection_choices"]; exists {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
	}
	source, ok := config["source"].(map[string]interface{})
	if !ok {
		return nil, nil
	}
	raw, exists := source["protection_choices"]
	if !exists {
		return nil, nil
	}
	choices, err := dataprotection.DecodeFieldRoleChoices(raw)
	if err != nil {
		return nil, err
	}
	if len(choices) == 0 {
		return choices, nil
	}
	runtime, _ := config["runtime"].(map[string]interface{})
	load, _ := config["load"].(map[string]interface{})
	if runtime["boundary"] != "bounded" || load["mode"] != "snapshot" || source["data_type"] != "table" || source["representation"] != "native" {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	return choices, nil
}
