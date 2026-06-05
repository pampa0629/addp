package scanflow

import (
	"encoding/json"

	"github.com/addp/meta/internal/models"
)

type TargetSet struct {
	ScopeType    string
	CatalogPaths []string
	RefGroups    []models.ScanRefGroup
}

func TargetsFromScope(scope models.JSONMap) TargetSet {
	scopeType, _ := scope["type"].(string)
	if scopeType == "" {
		scopeType = "engine"
	}
	return TargetSet{
		ScopeType:    scopeType,
		CatalogPaths: jsonMapStringSlice(scope, "catalog_paths"),
		RefGroups:    RefGroupsFromMap(scope),
	}
}

func InheritedTargets(parent models.JSONMap, independent []models.JSONMap) TargetSet {
	allTargets := TargetsFromScope(parent)
	if allTargets.ScopeType != "catalog_path" {
		return allTargets
	}

	scheduledPaths := make(map[string]bool)
	for _, scope := range independent {
		targets := TargetsFromScope(scope)
		if targets.ScopeType != "catalog_path" {
			continue
		}
		for _, catalogPath := range targets.CatalogPaths {
			scheduledPaths[catalogPath] = true
		}
	}

	return TargetSet{
		ScopeType:    allTargets.ScopeType,
		CatalogPaths: filterUnscheduled(allTargets.CatalogPaths, scheduledPaths),
	}
}

func RefGroupsFromMap(config models.JSONMap) []models.ScanRefGroup {
	raw := config["ref_groups"]
	if raw == nil {
		return nil
	}
	if groups, ok := raw.([]models.ScanRefGroup); ok {
		result := make([]models.ScanRefGroup, len(groups))
		copy(result, groups)
		return result
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var groups []models.ScanRefGroup
	if err := json.Unmarshal(bytes, &groups); err != nil {
		return nil
	}
	return groups
}

func StringSliceFromInterface(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		if values, ok := raw.([]string); ok {
			result := make([]string, len(values))
			copy(result, values)
			return result
		}
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func jsonMapStringSlice(m models.JSONMap, key string) []string {
	if m == nil {
		return nil
	}
	raw, ok := m[key]
	if !ok {
		return nil
	}
	return StringSliceFromInterface(raw)
}

func filterUnscheduled(values []string, scheduled map[string]bool) []string {
	if len(values) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !scheduled[value] {
			result = append(result, value)
		}
	}
	return result
}
