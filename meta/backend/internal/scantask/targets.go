package scantask

import "github.com/addp/meta/internal/models"

type TargetSet struct {
	CatalogPaths []string
}

func TargetsFromParameters(params models.JSONMap) TargetSet {
	return TargetSet{
		CatalogPaths: catalogPathsFromParams(params),
	}
}

func InheritedTargets(parent models.JSONMap, independent []models.JSONMap) TargetSet {
	allTargets := TargetsFromParameters(parent)

	scheduledPaths := make(map[string]bool)
	for _, params := range independent {
		targets := TargetsFromParameters(params)
		for _, catalogPath := range targets.CatalogPaths {
			scheduledPaths[catalogPath] = true
		}
	}

	return TargetSet{
		CatalogPaths: filterUnscheduled(allTargets.CatalogPaths, scheduledPaths),
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
