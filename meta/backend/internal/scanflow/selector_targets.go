package scanflow

import (
	"fmt"
	"strings"

	"github.com/addp/common/dataitem"
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

func TargetPathsFromLocator(locator string) []string {
	locator = strings.TrimSpace(locator)
	if locator == "" {
		return nil
	}
	typeIdx := strings.Index(locator, "?type=")
	pathPart := locator
	targetType := ""
	if typeIdx >= 0 {
		pathPart = locator[:typeIdx]
		targetType = locator[typeIdx+6:]
		if amp := strings.Index(targetType, "&"); amp >= 0 {
			targetType = targetType[:amp]
		}
	}
	pathMarker := "/path/"
	pathIdx := strings.Index(pathPart, pathMarker)
	if pathIdx < 0 {
		return nil
	}
	path := strings.Trim(pathPart[pathIdx+len(pathMarker):], "/")
	if path == "" {
		return nil
	}
	path = strings.ReplaceAll(path, "%2F", "/")
	path = strings.ReplaceAll(path, "%2f", "/")
	switch targetType {
	case "table", "collection", "graph":
		parts := strings.Split(path, "/")
		if len(parts) > 1 {
			return []string{parts[0]}
		}
		return []string{path}
	case "schema", "database":
		return []string{strings.Split(path, "/")[0]}
	}
	return []string{path}
}

func EngineIDFromLocator(locator string) (uint, bool) {
	locator = strings.TrimSpace(locator)
	const prefix = "addp://engine/"
	if !strings.HasPrefix(locator, prefix) {
		return 0, false
	}
	rest := strings.TrimPrefix(locator, prefix)
	idx := strings.Index(rest, "/")
	if idx < 0 {
		return 0, false
	}
	var id uint
	if _, err := fmt.Sscanf(rest[:idx], "%d", &id); err != nil {
		return 0, false
	}
	return id, id > 0
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

func TopCatalogTargets(paths []string) []string {
	targets := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.Trim(strings.TrimSpace(path), "/")
		if path == "" {
			continue
		}
		parts := strings.FieldsFunc(path, func(r rune) bool {
			return r == '/' || r == '.'
		})
		if len(parts) == 0 {
			continue
		}
		targets = append(targets, parts[0])
	}
	return UniqueNonEmpty(targets)
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
