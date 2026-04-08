package models

// SpatialLayerInfo 空间图层信息（名称 + 配置）
type SpatialLayerInfo struct {
	Name   string              `json:"name"`   // Neo4j layer name
	Config *SpatialLayerConfig `json:"config"` // 含 geometry_type / lon_field / lat_field / geom_field
}

// AlgorithmCapabilities 算法能力探测结果
type AlgorithmCapabilities struct {
	GDSAvailable     bool               `json:"gds_available"`
	GDSVersion       string             `json:"gds_version,omitempty"`
	SpatialAvailable bool               `json:"spatial_available"`
	SpatialLayers    []SpatialLayerInfo `json:"spatial_layers"`  // 已在 Neo4j 中创建的空间图层
	PendingLayers    []string           `json:"pending_layers"`  // 本体中已定义（含继承）但尚未同步到 Neo4j 的空间图层
	CypherAlgos      []string           `json:"cypher_algos"`    // 始终返回
	GDSAlgos         []string           `json:"gds_algos"`       // 不可用时返回空列表
	SpatialAlgos     []string           `json:"spatial_algos"`   // 不可用时返回空列表
}

// AlgorithmRunRequest 执行算法的请求体
type AlgorithmRunRequest struct {
	Algorithm  string                 `json:"algorithm"    binding:"required"`
	Params     map[string]interface{} `json:"params"`
	NodeLabels []string               `json:"node_labels"` // GDS 投影过滤（空=全部）
	RelTypes   []string               `json:"rel_types"`   // GDS 投影过滤（空=全部）
	Limit      int                    `json:"limit"`       // Top-N，默认50，最大200
}

// NodeScore 节点评分（中心性/社区算法结果）
type NodeScore struct {
	NodeID      string  `json:"node_id"`
	DisplayName string  `json:"display_name"`
	EntityType  string  `json:"entity_type"`
	Score       float64 `json:"score"`
	Rank        int     `json:"rank"`         // 1-based
	CommunityID int64   `json:"community_id"` // 仅社区算法填充
}

// AlgorithmResult 算法执行结果
type AlgorithmResult struct {
	Algorithm     string                 `json:"algorithm"`
	AlgorithmName string                 `json:"algorithm_name"` // 中文展示名
	NodeScores    []NodeScore            `json:"node_scores"`    // 中心性/排名算法
	Subgraph      *SubgraphResult        `json:"subgraph,omitempty"` // 路径/邻居算法
	Metadata      map[string]interface{} `json:"metadata"`       // 耗时/社区数等
	Warning       string                 `json:"warning,omitempty"`
}
