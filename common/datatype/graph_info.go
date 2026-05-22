package datatype

// GraphInfo is the common type info for graph data items.
type GraphInfo struct {
	NodeCount         *int64                  `json:"node_count,omitempty"`
	EdgeCount         *int64                  `json:"edge_count,omitempty"`
	NodeLabels        []GraphLabelInfo        `json:"node_labels,omitempty"`
	RelationshipTypes []GraphRelationshipInfo `json:"relationship_types,omitempty"`
}

// GraphLabelInfo describes a graph node label and its properties.
type GraphLabelInfo struct {
	Name       string      `json:"name,omitempty"`
	Properties []FieldInfo `json:"properties,omitempty"`
	Count      *int64      `json:"count,omitempty"`
}

// GraphRelationshipInfo describes a graph relationship type and its properties.
type GraphRelationshipInfo struct {
	Name       string      `json:"name,omitempty"`
	FromLabels []string    `json:"from_labels,omitempty"`
	ToLabels   []string    `json:"to_labels,omitempty"`
	Properties []FieldInfo `json:"properties,omitempty"`
	Count      *int64      `json:"count,omitempty"`
}
