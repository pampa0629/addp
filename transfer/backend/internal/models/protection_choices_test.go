package models

import (
	"github.com/addp/common/dataprotection"
	"testing"
)

func TestSourceProtectionChoiceStrictShape(t *testing.T) {
	choice := map[string]any{"target": map[string]any{"owner_module": "meta", "resource_type": "data_item", "resource_identity": "item"}, "component_key": "phone", "selection": map[string]any{"policy_id": "policy", "policy_revision": 1, "assessment_revision": 1, "option_id": "sm3"}}
	for _, mode := range []string{"valid", "unknown", "null", "continuous", "incremental", "encoded", "record", "target", "root"} {
		t.Run(mode, func(t *testing.T) {
			field := map[string]any{}
			for k, v := range choice {
				field[k] = v
			}
			source := map[string]any{"locator": "source", "data_type": "table", "representation": "native", "protection_choices": []any{field}}
			config := map[string]any{"runtime": map[string]any{"boundary": "bounded"}, "load": map[string]any{"mode": "snapshot"}, "source": source, "target": map[string]any{}}
			switch mode {
			case "unknown":
				field["role_keys"] = []string{"administrator"}
			case "null":
				source["protection_choices"] = nil
			case "continuous":
				config["runtime"].(map[string]any)["boundary"] = "continuous"
			case "incremental":
				config["load"].(map[string]any)["mode"] = "incremental"
			case "encoded":
				source["representation"] = "encoded"
			case "record":
				source["data_type"] = "record"
			case "target":
				config["target"].(map[string]any)["protection_choices"] = []any{field}
			case "root":
				config["protection_choices"] = []any{field}
			}
			choices, err := SourceProtectionChoices(config)
			if (err == nil) != (mode == "valid") || mode == "valid" && len(choices) != 1 {
				t.Fatal("invalid role selection was discarded", err)
			}
		})
	}
	if err := dataprotection.ValidateFieldRoleChoices(nil); err != nil {
		t.Fatal(err)
	}
}
