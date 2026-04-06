package models

// KSEntity 知识服务 API：单个实体（本体感知）
type KSEntity struct {
	ID         string                 `json:"id"`          // Neo4j elementId
	Type       string                 `json:"type"`        // 本体实体类型 name
	TypeLabel  string                 `json:"type_label"`  // 本体实体类型显示名
	Properties map[string]interface{} `json:"properties"`  // 所有属性
}

// KSNeighborItem 邻居节点项
type KSNeighborItem struct {
	Node          KSEntity `json:"node"`
	RelationType  string   `json:"relation_type"`  // 本体关系类型 name
	RelationLabel string   `json:"relation_label"` // 本体关系类型显示名
	Direction     string   `json:"direction"`      // "out" | "in"
}

// KSNeighborsResponse 邻居响应（直接返回，非分页）
type KSNeighborsResponse struct {
	Node      KSEntity         `json:"node"`
	Neighbors []KSNeighborItem `json:"neighbors"`
}

// KSPathRequest 路径查找请求
type KSPathRequest struct {
	SourceNodeID string `json:"source_node_id" binding:"required"`
	TargetNodeID string `json:"target_node_id" binding:"required"`
	MaxHops      int    `json:"max_hops"` // 默认 6
}

// KSSubgraphRequest 子图请求
type KSSubgraphRequest struct {
	NodeID string `json:"node_id" binding:"required"`
	Depth  int    `json:"depth"` // 默认 2，最大 3
	Limit  int    `json:"limit"` // 默认 50
}

// KSOntologyResponse 本体描述
type KSOntologyResponse struct {
	GraphName     string           `json:"graph_name"`
	OntologyName  string           `json:"ontology_name"`
	EntityTypes   []KSEntityType   `json:"entity_types"`
	RelationTypes []KSRelationType `json:"relation_types"`
}

// KSEntityType 本体实体类型描述
type KSEntityType struct {
	Name       string           `json:"name"`
	Label      string           `json:"label"`
	Properties []KSPropertyInfo `json:"properties"`
	Count      int64            `json:"count"` // Neo4j 节点数量
}

// KSPropertyInfo 属性定义
type KSPropertyInfo struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	Unique   bool   `json:"unique"`
	Required bool   `json:"required"`
}

// KSRelationType 本体关系类型描述
type KSRelationType struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	SourceType string `json:"source_type"`
	TargetType string `json:"target_type"`
	Count      int64  `json:"count"` // Neo4j 关系数量
}
