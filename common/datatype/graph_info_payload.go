package datatype

import (
	"sort"
	"strings"

	commonJSON "github.com/addp/common/jsonmap"
)

// GraphInfoFromPayload restores common graph facts from a graph JSON payload.
func GraphInfoFromPayload(payload map[string]interface{}) *GraphInfo {
	if len(payload) == 0 {
		return nil
	}
	var info GraphInfo
	if err := commonJSON.DecodeStruct(payload, &info); err != nil {
		return nil
	}
	info.Model = strings.TrimSpace(info.Model)
	info.NodeShapes = normalizeGraphNodeShapes(info.NodeShapes)
	info.RelationshipShapes = normalizeGraphRelationshipShapes(info.RelationshipShapes)
	if info.NodeCount != nil && *info.NodeCount < 0 {
		info.NodeCount = nil
	}
	if info.RelationshipCount != nil && *info.RelationshipCount < 0 {
		info.RelationshipCount = nil
	}
	if info.Model == "" && info.Directed == nil && info.NodeCount == nil && info.RelationshipCount == nil &&
		len(info.NodeShapes) == 0 && len(info.RelationshipShapes) == 0 {
		return nil
	}
	return &info
}

// GraphInfoPayload converts common graph facts to a JSON payload.
func GraphInfoPayload(info *GraphInfo) map[string]interface{} {
	normalized := NormalizeGraphInfo(info)
	return commonJSON.MapFromStruct(normalized)
}

// NormalizeGraphInfo returns a normalized copy of graph facts.
func NormalizeGraphInfo(info *GraphInfo) *GraphInfo {
	if info == nil {
		return nil
	}
	payload := commonJSON.MapFromStruct(info)
	return GraphInfoFromPayload(payload)
}

func normalizeGraphNodeShapes(input []GraphNodeShapeInfo) []GraphNodeShapeInfo {
	if len(input) == 0 {
		return nil
	}
	output := make([]GraphNodeShapeInfo, 0, len(input))
	for _, shape := range input {
		shape.Name = strings.TrimSpace(shape.Name)
		shape.Kind = strings.TrimSpace(shape.Kind)
		shape.Labels = normalizeGraphLabelSet(shape.Labels)
		if shape.Name == "" {
			shape.Name = graphLabelSetName(shape.Labels)
		}
		shape.Properties = normalizeGraphProperties(shape.Properties)
		if shape.Count != nil && *shape.Count < 0 {
			shape.Count = nil
		}
		if shape.Name == "" && len(shape.Labels) == 0 && len(shape.Properties) == 0 && shape.Count == nil {
			continue
		}
		output = append(output, shape)
	}
	return output
}

func normalizeGraphRelationshipShapes(input []GraphRelationshipShapeInfo) []GraphRelationshipShapeInfo {
	if len(input) == 0 {
		return nil
	}
	output := make([]GraphRelationshipShapeInfo, 0, len(input))
	for _, shape := range input {
		shape.Type = strings.TrimSpace(shape.Type)
		shape.Properties = normalizeGraphProperties(shape.Properties)
		shape.Patterns = normalizeGraphRelationshipPatterns(shape.Patterns)
		if shape.Count != nil && *shape.Count < 0 {
			shape.Count = nil
		}
		if shape.Type == "" && len(shape.Properties) == 0 && len(shape.Patterns) == 0 && shape.Count == nil {
			continue
		}
		output = append(output, shape)
	}
	return output
}

func normalizeGraphRelationshipPatterns(input []GraphRelationshipPatternInfo) []GraphRelationshipPatternInfo {
	if len(input) == 0 {
		return nil
	}
	output := make([]GraphRelationshipPatternInfo, 0, len(input))
	for _, pattern := range input {
		pattern.From = normalizeGraphEndpoint(pattern.From)
		pattern.To = normalizeGraphEndpoint(pattern.To)
		if pattern.Count != nil && *pattern.Count < 0 {
			pattern.Count = nil
		}
		if emptyGraphEndpoint(pattern.From) && emptyGraphEndpoint(pattern.To) && pattern.Count == nil {
			continue
		}
		output = append(output, pattern)
	}
	return output
}

func normalizeGraphEndpoint(endpoint GraphEndpointInfo) GraphEndpointInfo {
	endpoint.ShapeName = strings.TrimSpace(endpoint.ShapeName)
	endpoint.Labels = normalizeGraphLabelSet(endpoint.Labels)
	if endpoint.ShapeName == "" {
		endpoint.ShapeName = graphLabelSetName(endpoint.Labels)
	}
	return endpoint
}

func emptyGraphEndpoint(endpoint GraphEndpointInfo) bool {
	return endpoint.ShapeName == "" && len(endpoint.Labels) == 0
}

func normalizeGraphProperties(input []FieldInfo) []FieldInfo {
	if len(input) == 0 {
		return nil
	}
	output := make([]FieldInfo, 0, len(input))
	for _, property := range input {
		property = normalizeFieldInfo(property)
		if property.Name == "" {
			continue
		}
		output = append(output, property)
	}
	return output
}

func normalizeGraphLabelSet(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(input))
	output := make([]string, 0, len(input))
	for _, value := range input {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		output = append(output, value)
	}
	sort.Strings(output)
	return output
}

func graphLabelSetName(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	return strings.Join(labels, "+")
}
