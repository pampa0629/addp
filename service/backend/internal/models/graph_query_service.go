package models

import "time"

// GraphQueryService 图查询服务模型（对应 service.graph_query_services 表）
type GraphQueryService struct {
	ID uint `gorm:"primarykey" json:"id"`

	TenantID    uint        `gorm:"not null;index:idx_graph_query_services_tenant" json:"tenant_id"`
	ServiceName string      `gorm:"not null;size:255;uniqueIndex:unique_graph_service_name" json:"service_name"`
	Title       string      `gorm:"not null;size:255" json:"title"`
	Description string      `gorm:"type:text" json:"description"`
	Keywords    StringArray `gorm:"type:text[]" json:"keywords"`

	EngineID     uint   `gorm:"not null;index:idx_graph_query_services_engine" json:"engine_id"`
	DatabaseName string `gorm:"size:255;not null;default:'neo4j'" json:"database_name"`

	// 配置类型: 'shape' | 'cypher'
	ConfigType  string      `gorm:"size:50;not null;check:config_type IN ('shape', 'cypher')" json:"config_type"`
	NodeShape   string      `gorm:"size:255" json:"node_shape,omitempty"`
	NodeLabels  StringArray `gorm:"type:text[]" json:"node_labels,omitempty"`
	CypherQuery string      `gorm:"type:text" json:"cypher_query,omitempty"`

	// 数据配置（JSONB）— 同时存储模式特定配置和参数定义
	// shape 模式: {"properties":["id","name"],"filterable_properties":["name"]}
	// cypher 模式: {"result_type":"table|graph|both","parameters":[{"name":"city","type":"string","required":true}]}
	DataConfig JSONB `gorm:"type:jsonb;not null;default:'{}'" json:"data_config"`

	PublicAccess bool `gorm:"default:false" json:"public_access"`
	MaxRecords   int  `gorm:"default:500" json:"max_records"`

	Status       string `gorm:"size:50;not null;default:'active';check:status IN ('active', 'inactive', 'error')" json:"status"`
	ErrorMessage string `gorm:"type:text" json:"error_message,omitempty"`

	CreatedBy uint      `gorm:"not null" json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (GraphQueryService) TableName() string {
	return "service.graph_query_services"
}

// IsShapeMode 是否为节点形状模式
func (g *GraphQueryService) IsShapeMode() bool {
	return g.ConfigType == "shape"
}

// IsCypherMode 是否为 Cypher 模式
func (g *GraphQueryService) IsCypherMode() bool {
	return g.ConfigType == "cypher"
}

// GetResultType 获取结果类型（cypher 模式）
func (g *GraphQueryService) GetResultType() string {
	if g.DataConfig == nil {
		return "table"
	}
	if rt, ok := g.DataConfig["result_type"].(string); ok && rt != "" {
		return rt
	}
	return "table"
}

// IsGraphResult 是否需要返回图结构
func (g *GraphQueryService) IsGraphResult() bool {
	rt := g.GetResultType()
	return rt == "graph" || rt == "both"
}

// IsTableResult 是否需要返回表格数据
func (g *GraphQueryService) IsTableResult() bool {
	rt := g.GetResultType()
	return rt == "table" || rt == "both"
}

// GetProperties shape 模式下可返回的属性列表
func (g *GraphQueryService) GetProperties() []string {
	return getStringSlice(g.DataConfig, "properties")
}

// GetFilterableProperties shape 模式下可过滤的属性列表
func (g *GraphQueryService) GetFilterableProperties() []string {
	return getStringSlice(g.DataConfig, "filterable_properties")
}

func getStringSlice(data JSONB, key string) []string {
	if data == nil {
		return nil
	}
	raw, ok := data[key].([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// ---- 请求/响应 DTO ----

// ParameterDef 参数定义（服务发布时声明的 Cypher 参数）
type ParameterDef struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"` // string | integer | float | boolean
	Required    bool        `json:"required"`
	Description string      `json:"description"`
	Default     interface{} `json:"default,omitempty"`
}

// CreateGraphQueryServiceRequest 创建图查询服务请求
type CreateGraphQueryServiceRequest struct {
	ServiceName  string   `json:"service_name" binding:"required"`
	Title        string   `json:"title" binding:"required"`
	Description  string   `json:"description"`
	Keywords     []string `json:"keywords"`
	EngineID     uint     `json:"engine_id" binding:"required"`
	DatabaseName string   `json:"database_name"`

	ConfigType  string   `json:"config_type" binding:"required,oneof=shape cypher"`
	NodeShape   string   `json:"node_shape"`
	NodeLabels  []string `json:"node_labels"`
	CypherQuery string   `json:"cypher_query"`

	// cypher 模式下的参数定义（可选，不传则自动从 Cypher 中提取）
	Parameters []ParameterDef         `json:"parameters"`
	DataConfig map[string]interface{} `json:"data_config"`

	PublicAccess bool `json:"public_access"`
	MaxRecords   int  `json:"max_records" binding:"omitempty,gte=1,lte=5000"`
}

// UpdateGraphQueryServiceRequest 更新图查询服务请求（所有字段均可选）
type UpdateGraphQueryServiceRequest struct {
	Title        *string                `json:"title,omitempty"`
	Description  *string                `json:"description,omitempty"`
	Keywords     []string               `json:"keywords,omitempty"`
	CypherQuery  *string                `json:"cypher_query,omitempty"`
	DataConfig   map[string]interface{} `json:"data_config,omitempty"`
	PublicAccess *bool                  `json:"public_access,omitempty"`
	MaxRecords   *int                   `json:"max_records,omitempty" binding:"omitempty,gte=1,lte=5000"`
	Status       *string                `json:"status,omitempty" binding:"omitempty,oneof=active inactive error"`
}

// GraphQueryServiceDTO 图查询服务 DTO（对外响应）
type GraphQueryServiceDTO struct {
	ID uint `json:"id"`

	TenantID    uint     `json:"tenant_id"`
	ServiceName string   `json:"service_name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`

	EngineID     uint   `json:"engine_id"`
	DatabaseName string `json:"database_name"`

	ConfigType  string   `json:"config_type"`
	NodeShape   string   `json:"node_shape,omitempty"`
	NodeLabels  []string `json:"node_labels,omitempty"`
	CypherQuery string   `json:"cypher_query,omitempty"`

	Parameters []ParameterDef         `json:"parameters"`
	DataConfig map[string]interface{} `json:"data_config"`

	PublicAccess bool `json:"public_access"`
	MaxRecords   int  `json:"max_records"`

	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`

	// 服务访问端点
	Endpoints map[string]string `json:"endpoints"`

	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GraphQueryExecuteRequest 图查询执行请求
type GraphQueryExecuteRequest struct {
	// Cypher 模式：用户提供的参数值（对应 $paramName 占位符）
	// Shape 模式：用于过滤的属性条件（属性名 → 值）
	Parameters map[string]interface{} `json:"parameters"`

	// 分页（两种模式均支持）
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

// GraphQueryResponse 图查询执行响应
type GraphQueryResponse struct {
	// 表格格式（result_type = table | both）
	Columns []string                 `json:"columns,omitempty"`
	Rows    []map[string]interface{} `json:"rows,omitempty"`

	// 分页（shape 模式提供）
	TotalCount *int64 `json:"total_count,omitempty"`
	Page       *int   `json:"page,omitempty"`
	PageSize   *int   `json:"page_size,omitempty"`
	HasMore    *bool  `json:"has_more,omitempty"`

	// 图格式（result_type = graph | both）
	GraphData interface{} `json:"graph_data,omitempty"`

	// 通用
	RowsCount int `json:"rows_count"`
}
