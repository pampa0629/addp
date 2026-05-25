package datatype

const (
	GraphModelGeneric       = "generic"
	GraphModelPropertyGraph = "property_graph"
	GraphModelRDF           = "rdf"

	GraphNodeShapeKindLabel    = "label"
	GraphNodeShapeKindLabelSet = "label_set"
	GraphNodeShapeKindClass    = "class"
	GraphNodeShapeKindInferred = "inferred"
)

// GraphInfo is the common type info for graph data items.
type GraphInfo struct {
	Model              string                       `json:"model,omitempty"`
	Directed           *bool                        `json:"directed,omitempty"`
	NodeCount          *int64                       `json:"node_count,omitempty"`
	RelationshipCount  *int64                       `json:"relationship_count,omitempty"`
	NodeShapes         []GraphNodeShapeInfo         `json:"node_shapes,omitempty"`
	RelationshipShapes []GraphRelationshipShapeInfo `json:"relationship_shapes,omitempty"`
}

// GraphNodeShapeInfo describes a node structure shape, such as a label,
// label set, class, or inferred node group.
type GraphNodeShapeInfo struct {
	Name       string      `json:"name,omitempty"`
	Kind       string      `json:"kind,omitempty"`
	Labels     []string    `json:"labels,omitempty"`
	Properties []FieldInfo `json:"properties,omitempty"`
	Count      *int64      `json:"count,omitempty"`
}

// GraphRelationshipShapeInfo describes a relationship structure shape.
type GraphRelationshipShapeInfo struct {
	Type       string                         `json:"type,omitempty"`
	Properties []FieldInfo                    `json:"properties,omitempty"`
	Patterns   []GraphRelationshipPatternInfo `json:"patterns,omitempty"`
	Count      *int64                         `json:"count,omitempty"`
}

// GraphRelationshipPatternInfo describes an observed endpoint pattern for a
// relationship shape.
type GraphRelationshipPatternInfo struct {
	From  GraphEndpointInfo `json:"from,omitempty"`
	To    GraphEndpointInfo `json:"to,omitempty"`
	Count *int64            `json:"count,omitempty"`
}

// GraphEndpointInfo describes one side of a relationship pattern.
type GraphEndpointInfo struct {
	ShapeName string   `json:"shape_name,omitempty"`
	Labels    []string `json:"labels,omitempty"`
}

// Clone returns a deep copy of GraphInfo.
func (g *GraphInfo) Clone() *GraphInfo {
	if g == nil {
		return nil
	}
	cloned := *g
	if g.Directed != nil {
		directed := *g.Directed
		cloned.Directed = &directed
	}
	if g.NodeCount != nil {
		nodeCount := *g.NodeCount
		cloned.NodeCount = &nodeCount
	}
	if g.RelationshipCount != nil {
		relationshipCount := *g.RelationshipCount
		cloned.RelationshipCount = &relationshipCount
	}
	cloned.NodeShapes = cloneGraphNodeShapes(g.NodeShapes)
	cloned.RelationshipShapes = cloneGraphRelationshipShapes(g.RelationshipShapes)
	return &cloned
}

func cloneGraphNodeShapes(input []GraphNodeShapeInfo) []GraphNodeShapeInfo {
	if len(input) == 0 {
		return nil
	}
	output := make([]GraphNodeShapeInfo, len(input))
	for i, shape := range input {
		output[i] = shape
		output[i].Labels = append([]string(nil), shape.Labels...)
		output[i].Properties = append([]FieldInfo(nil), shape.Properties...)
		if shape.Count != nil {
			count := *shape.Count
			output[i].Count = &count
		}
	}
	return output
}

func cloneGraphRelationshipShapes(input []GraphRelationshipShapeInfo) []GraphRelationshipShapeInfo {
	if len(input) == 0 {
		return nil
	}
	output := make([]GraphRelationshipShapeInfo, len(input))
	for i, shape := range input {
		output[i] = shape
		output[i].Properties = append([]FieldInfo(nil), shape.Properties...)
		output[i].Patterns = cloneGraphRelationshipPatterns(shape.Patterns)
		if shape.Count != nil {
			count := *shape.Count
			output[i].Count = &count
		}
	}
	return output
}

func cloneGraphRelationshipPatterns(input []GraphRelationshipPatternInfo) []GraphRelationshipPatternInfo {
	if len(input) == 0 {
		return nil
	}
	output := make([]GraphRelationshipPatternInfo, len(input))
	for i, pattern := range input {
		output[i] = pattern
		output[i].From.Labels = append([]string(nil), pattern.From.Labels...)
		output[i].To.Labels = append([]string(nil), pattern.To.Labels...)
		if pattern.Count != nil {
			count := *pattern.Count
			output[i].Count = &count
		}
	}
	return output
}
