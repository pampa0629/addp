package scantask

import "github.com/addp/meta/internal/models"

type TargetSet struct {
	Namespaces  []string
	ObjectPaths []string
}

func TargetsFromParameters(params models.JSONMap) TargetSet {
	return TargetSet{
		Namespaces:  JSONMapStringSlice(params, "namespaces"),
		ObjectPaths: JSONMapStringSlice(params, "object_paths"),
	}
}

func InheritedTargets(parent models.JSONMap, independent []models.JSONMap) TargetSet {
	allTargets := TargetsFromParameters(parent)

	scheduledNamespaces := make(map[string]bool)
	scheduledPaths := make(map[string]bool)
	for _, params := range independent {
		targets := TargetsFromParameters(params)
		for _, namespace := range targets.Namespaces {
			scheduledNamespaces[namespace] = true
		}
		for _, objectPath := range targets.ObjectPaths {
			scheduledPaths[objectPath] = true
		}
	}

	return TargetSet{
		Namespaces:  filterUnscheduled(allTargets.Namespaces, scheduledNamespaces),
		ObjectPaths: filterUnscheduled(allTargets.ObjectPaths, scheduledPaths),
	}
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
