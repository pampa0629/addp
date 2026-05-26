package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/addp/graph/internal/models"
)

func effectiveNodeLabels(et *models.EntityType, byID map[uint]*models.EntityType) []string {
	if et == nil {
		return nil
	}
	if labels := et.ParsedNodeLabels(); len(labels) > 0 {
		return labels
	}
	return inheritedEntityTypeNames(et, byID, 0)
}

func inheritedEntityTypeNames(et *models.EntityType, byID map[uint]*models.EntityType, depth int) []string {
	if et == nil || depth > 16 {
		return nil
	}
	labels := []string{et.Name}
	if et.ParentID == nil {
		return labels
	}
	parent, ok := byID[*et.ParentID]
	if !ok {
		return labels
	}
	return append(labels, inheritedEntityTypeNames(parent, byID, depth+1)...)
}

func entityTypeLabels(entityTypeName string) []string {
	parts := strings.Split(entityTypeName, "+")
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		label := strings.TrimSpace(part)
		if label != "" {
			labels = append(labels, label)
		}
	}
	if len(labels) == 0 && strings.TrimSpace(entityTypeName) != "" {
		labels = append(labels, strings.TrimSpace(entityTypeName))
	}
	return labels
}

func entityTypeByID(entityTypes []models.EntityType) map[uint]*models.EntityType {
	byID := make(map[uint]*models.EntityType, len(entityTypes))
	for i := range entityTypes {
		byID[entityTypes[i].ID] = &entityTypes[i]
	}
	return byID
}

func sameStringSet(a, b []string) bool {
	a = normalizedStringSet(a)
	b = normalizedStringSet(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func normalizedStringSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cypherStringList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("'%s'", escapeCypher(value)))
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

func entityTypeNodeLabels(ontology *models.Ontology, entityTypeName string) []string {
	if ontology == nil {
		return entityTypeLabels(entityTypeName)
	}
	byID := entityTypeByID(ontology.EntityTypes)
	for i := range ontology.EntityTypes {
		if ontology.EntityTypes[i].Name == entityTypeName {
			return effectiveNodeLabels(&ontology.EntityTypes[i], byID)
		}
	}
	return entityTypeLabels(entityTypeName)
}
