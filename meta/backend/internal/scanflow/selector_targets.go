package scanflow

import (
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/models"
)

func TargetPathsFromNode(node models.MetaNode) []string {
	if node.ParentNodeID == nil && strings.TrimSpace(node.FullName) == "" {
		return nil
	}
	if target := strings.Trim(strings.TrimSpace(node.FullName), "/"); target != "" {
		return []string{target}
	}
	if target := strings.Trim(strings.TrimSpace(node.Name), "/"); target != "" {
		return []string{target}
	}
	return nil
}

func TargetPathsFromItem(item models.MetaItem) []string {
	if targets := TargetPathsFromAttributes(item.Attributes); len(targets) > 0 {
		return targets
	}
	fullName := strings.Trim(strings.TrimSpace(item.FullName), "/")
	if fullName != "" {
		return []string{fullName}
	}
	return nil
}

func TargetPathsFromAttributes(attrs map[string]interface{}) []string {
	if targets := dataitem.ScanTargetsFromAttributes(attrs); len(targets) > 0 {
		result := make([]string, 0, len(targets))
		for _, target := range targets {
			if path := strings.Trim(strings.TrimSpace(target.Path), "/"); path != "" {
				result = append(result, path)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	if physicalPath := dataitem.DescriptorFromAttributes(attrs).PhysicalPath; physicalPath != "" {
		return []string{strings.Trim(physicalPath, "/")}
	}
	return nil
}

func EngineIDFromLocator(locator string) (uint, bool) {
	loc, err := resourcetree.ParseURI(strings.TrimSpace(locator))
	if err != nil || loc.EngineID == 0 {
		return 0, false
	}
	return loc.EngineID, true
}

func UniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
