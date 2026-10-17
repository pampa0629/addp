package propertygraph

import (
	"encoding/json"
	"strings"

	"github.com/addp/common/engine/plugin"
)

func cypherStrings(values []string) string { raw, _ := json.Marshal(values); return string(raw) }

// Engine-owned exclusions must never become global business vocabulary.
func NodePredicate(d plugin.PropertyGraphDialect, variable string) string {
	predicate := "size(labels(" + variable + ")) > 0"
	if len(d.ExcludedNodeLabels) > 0 {
		predicate += " AND NOT (any(label IN labels(" + variable + ") WHERE label IN " + cypherStrings(d.ExcludedNodeLabels) + "))"
	}
	return predicate
}
func RelationshipPredicate(d plugin.PropertyGraphDialect, relationship, source, target string) string {
	var parts []string
	if len(d.ExcludedRelationshipTypes) > 0 {
		parts = append(parts, "NOT (type("+relationship+") IN "+cypherStrings(d.ExcludedRelationshipTypes)+")")
	}
	if source != "" {
		parts = append(parts, NodePredicate(d, source))
	}
	if target != "" {
		parts = append(parts, NodePredicate(d, target))
	}
	if len(parts) == 0 {
		return "true"
	}
	return strings.Join(parts, " AND ")
}
func IsExcludedRelationship(d plugin.PropertyGraphDialect, name string) bool {
	for _, value := range d.ExcludedRelationshipTypes {
		if strings.EqualFold(value, name) {
			return true
		}
	}
	return false
}
