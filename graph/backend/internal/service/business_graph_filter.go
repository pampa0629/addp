package service

import (
	"strings"
)

const internalRelationshipTypeList = "['RTREE_METADATA', 'RTREE_REFERENCE', 'RTREE_ROOT']"
const internalRelationshipTypePredicate = "type(r) IN " + internalRelationshipTypeList
const internalNodeLabelList = "['SpatialLayer']"

func isInternalRelationshipType(relType string) bool {
	switch strings.ToUpper(strings.TrimSpace(relType)) {
	case "RTREE_METADATA", "RTREE_REFERENCE", "RTREE_ROOT":
		return true
	default:
		return false
	}
}

func isInternalNodeLabel(label string) bool {
	return strings.EqualFold(strings.TrimSpace(label), "SpatialLayer")
}

func isInternalNodeLabelSet(labels []string) bool {
	for _, label := range labels {
		if isInternalNodeLabel(label) {
			return true
		}
	}
	return false
}

func internalNodePredicate(nodeVar string) string {
	nodeVar = strings.TrimSpace(nodeVar)
	if nodeVar == "" {
		nodeVar = "n"
	}
	return "any(label IN labels(" + nodeVar + ") WHERE label IN " + internalNodeLabelList + ")"
}

func businessNodePredicate(nodeVar string) string {
	return "NOT (" + internalNodePredicate(nodeVar) + ")"
}

func businessRelationshipPredicate(relVar, sourceVar, targetVar string) string {
	relVar = strings.TrimSpace(relVar)
	if relVar == "" {
		relVar = "r"
	}
	sourceVar = strings.TrimSpace(sourceVar)
	targetVar = strings.TrimSpace(targetVar)
	parts := []string{"NOT (type(" + relVar + ") IN " + internalRelationshipTypeList + ")"}
	if sourceVar != "" {
		parts = append(parts, businessNodePredicate(sourceVar))
	}
	if targetVar != "" {
		parts = append(parts, businessNodePredicate(targetVar))
	}
	return strings.Join(parts, " AND ")
}
