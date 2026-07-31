package models

// GraphNodeDTO 图视图节点。kind 区分 Neo4j 实体与仅用于概览的聚合桶。
type GraphNodeDTO struct {
	ID          string                 `json:"id"`           // Neo4j elementId
	Kind        string                 `json:"kind"`         // entity | aggregate
	Labels      []string               `json:"labels"`       // Neo4j 标签列表
	EntityType  string                 `json:"entity_type"`  // 匹配到的本体实体类型 name
	Color       string                 `json:"color"`        // 本体中定义的颜色
	DisplayName string                 `json:"display_name"` // 用于图标签的展示名称
	MemberCount int64                  `json:"member_count,omitempty"`
	Properties  map[string]interface{} `json:"properties"`
}

// GraphEdgeDTO 图视图关系。聚合关系的 count 表示桶之间的真实关系数。
type GraphEdgeDTO struct {
	ID           string                 `json:"id"`            // Neo4j elementId
	Kind         string                 `json:"kind"`          // entity | aggregate
	Type         string                 `json:"type"`          // 关系类型名称（大写）
	RelationType string                 `json:"relation_type"` // 本体关系类型 name
	Color        string                 `json:"color"`         // 本体中定义的颜色
	Directed     bool                   `json:"directed"`      // 是否按本体定义显示方向
	Source       string                 `json:"source"`        // 源节点 elementId
	Target       string                 `json:"target"`        // 目标节点 elementId
	Count        int64                  `json:"count,omitempty"`
	Properties   map[string]interface{} `json:"properties"`
}

// NodeShapeDTO 节点结构形状，labels 是具体图引擎的执行映射。
type NodeShapeDTO struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind,omitempty"`
	Labels []string `json:"labels,omitempty"`
	Color  string   `json:"color"`
	Count  *int64   `json:"count,omitempty"`
}

// RelationshipShapeDTO 关系结构形状。
type RelationshipShapeDTO struct {
	Type     string                   `json:"type"`
	Color    string                   `json:"color"`
	Directed bool                     `json:"directed"`
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
	NodeCount         int64            `json:"node_count"`         // 总节点数
	RelationshipCount int64            `json:"relationship_count"` // 总关系数
	ByLabel           map[string]int64 `json:"by_label"`           // 按标签分组的节点数
}

// BrowseSnapshot 是同一次图事实读取派生的浏览初始化数据。
type BrowseSnapshot struct {
	Schema   BrowseSchema   `json:"schema"`
	Stats    BrowseStats    `json:"stats"`
	Overview SubgraphResult `json:"overview"`
}

// SearchRequest 全文搜索请求
type SearchRequest struct {
	Query string `json:"query" binding:"required"`
	Limit int    `json:"limit"`
}

// ExpandTarget 展开目标。聚合目标通过完整 label set 标识，实体目标通过 elementId 标识。
type ExpandTarget struct {
	Kind   string   `json:"kind" binding:"required"`
	ID     string   `json:"id"`
	Labels []string `json:"labels"`
}

// ExpandRequest 统一的聚合桶/实体子图展开请求。
type ExpandRequest struct {
	Target            ExpandTarget `json:"target" binding:"required"`
	Depth             int          `json:"depth"`
	NodeLimit         int          `json:"node_limit"`
	RelationshipLimit int          `json:"relationship_limit"`
}

// PathRequest 路径查询请求
type PathRequest struct {
	SourceID string `json:"source_id" binding:"required"`
	TargetID string `json:"target_id" binding:"required"`
}

// ApplyInferredSchemaRequest 应用推导结果到本体（从 graph_id）
type ApplyInferredSchemaRequest struct {
	OntologyID       uint     `json:"ontology_id" binding:"required"`
	EntityTypeNames  []string `json:"entity_type_names"`
	RelationTypeKeys []string `json:"relation_type_keys"`
	Conflict         string   `json:"conflict"` // "skip" | "overwrite"
}

// ApplyInferredSchemaFromEngineRequest 应用推导结果到本体（从 engine_id）
type ApplyInferredSchemaFromEngineRequest struct {
	EngineID         uint     `json:"engine_id" binding:"required"`
	EntityTypeNames  []string `json:"entity_type_names"`
	RelationTypeKeys []string `json:"relation_type_keys"`
	Conflict         string   `json:"conflict"` // "skip" | "overwrite"
}
