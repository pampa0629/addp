package models

// GraphNodeDTO 图节点（供前端 G6 使用）
type GraphNodeDTO struct {
	ID          string                 `json:"id"`           // Neo4j elementId
	Labels      []string               `json:"labels"`       // Neo4j 标签列表
	EntityType  string                 `json:"entity_type"`  // 匹配到的本体实体类型 name
	Color       string                 `json:"color"`        // 本体中定义的颜色
	DisplayName string                 `json:"display_name"` // 用于图标签的展示名称
	Properties  map[string]interface{} `json:"properties"`
}

// GraphEdgeDTO 图关系（供前端 G6 使用）
type GraphEdgeDTO struct {
	ID           string                 `json:"id"`            // Neo4j elementId
	Type         string                 `json:"type"`          // 关系类型名称（大写）
	RelationType string                 `json:"relation_type"` // 本体关系类型 name
	Color        string                 `json:"color"`         // 本体中定义的颜色
	Source       string                 `json:"source"`        // 源节点 elementId
	Target       string                 `json:"target"`        // 目标节点 elementId
	Properties   map[string]interface{} `json:"properties"`
}

// NodeShapeDTO 节点结构形状，labels 是具体图引擎的执行映射。
type NodeShapeDTO struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind,omitempty"`
	Labels []string `json:"labels,omitempty"`
	Count  *int64   `json:"count,omitempty"`
}

// RelationshipShapeDTO 关系结构形状。
type RelationshipShapeDTO struct {
	Type     string                   `json:"type"`
	Patterns []RelationshipPatternDTO `json:"patterns,omitempty"`
	Count    *int64                   `json:"count,omitempty"`
}

// RelationshipPatternDTO 关系端点模式。
type RelationshipPatternDTO struct {
	From  GraphEndpointDTO `json:"from"`
	To    GraphEndpointDTO `json:"to"`
	Count *int64           `json:"count,omitempty"`
}

// GraphEndpointDTO 图关系端点。
type GraphEndpointDTO struct {
	ShapeName string   `json:"shape_name,omitempty"`
	Labels    []string `json:"labels,omitempty"`
}

// SubgraphResult 子图查询结果
type SubgraphResult struct {
	Nodes []GraphNodeDTO `json:"nodes"`
	Edges []GraphEdgeDTO `json:"edges"`
}

// GraphSchema 图谱 Schema（节点形状 + 关系形状）
type BrowseSchema struct {
	NodeShapes         []NodeShapeDTO         `json:"node_shapes"`         // 所有节点形状
	RelationshipShapes []RelationshipShapeDTO `json:"relationship_shapes"` // 所有关系形状
}

// GraphStats 图谱统计信息
type BrowseStats struct {
	NodeCount int64            `json:"node_count"` // 总节点数
	EdgeCount int64            `json:"edge_count"` // 总关系数
	ByLabel   map[string]int64 `json:"by_label"`   // 按标签分组的节点数
}

// SearchRequest 全文搜索请求
type SearchRequest struct {
	Query string `json:"query" binding:"required"`
	Limit int    `json:"limit"`
}

// ExpandRequest 节点展开请求
type ExpandRequest struct {
	NodeID string `json:"node_id" binding:"required"`
	Limit  int    `json:"limit"`
}

// PathRequest 路径查询请求
type PathRequest struct {
	SourceID string `json:"source_id" binding:"required"`
	TargetID string `json:"target_id" binding:"required"`
}
