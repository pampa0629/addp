package service

import "testing"

func TestWorkflowSuperMapS3MAdapterKeepsNFSSourceAndAllowsObjectTarget(t *testing.T) {
	spec := workflowSuperMapS3MAdapterSpec()
	var sourceFamilies, sourceTypes, targetFamilies []string
	for _, parameter := range spec.PublicParameters {
		if parameter.UIType != "resource_tree_picker" {
			continue
		}
		binding, _ := parameter.UIConfig["resource_binding"].(map[string]interface{})
		families, _ := parameter.UIConfig["engine_families"].([]string)
		types, _ := parameter.UIConfig["engine_types"].([]string)
		if binding["mode"] == "existing" {
			sourceFamilies, sourceTypes = families, types
		} else if binding["mode"] == "target" {
			targetFamilies = families
			if len(types) != 0 {
				t.Fatalf("target engine_types = %#v, want unrestricted file/object engines", types)
			}
		}
	}
	if len(sourceFamilies) != 1 || sourceFamilies[0] != "file" || len(sourceTypes) != 1 || sourceTypes[0] != "nfs" {
		t.Fatalf("source families/types = %#v/%#v", sourceFamilies, sourceTypes)
	}
	if len(targetFamilies) != 2 || targetFamilies[0] != "file" || targetFamilies[1] != "object" {
		t.Fatalf("target families = %#v", targetFamilies)
	}
}
