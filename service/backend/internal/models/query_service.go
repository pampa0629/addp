package models

import (
	"time"
)

// QueryService 查询服务模型
type QueryService struct {
	ID uint `gorm:"primarykey" json:"id"`

	// 基本信息
	TenantID    uint        `gorm:"not null;index:idx_query_services_tenant" json:"tenant_id"`
	ServiceName string      `gorm:"not null;size:255;uniqueIndex:unique_query_service_name" json:"service_name"`
	Title       string      `gorm:"not null;size:255" json:"title"`
	Description string      `gorm:"type:text" json:"description"`
	Keywords    StringArray `gorm:"type:text[]" json:"keywords"`

	// 配置方式（互斥）
	ConfigType string `gorm:"size:50;not null;check:config_type IN ('table', 'sql');index:idx_query_services_config_type" json:"config_type"`

	// 存储引擎（DuckDB SQL 模式时为 nil）
	EngineID *uint `gorm:"index:idx_query_services_engine" json:"engine_id"`

	// 表配置字段（config_type='table'时使用）
	SchemaName  string `gorm:"size:255;column:schema_name" json:"schema_name"`
	TargetTable string `gorm:"size:255;column:table_name" json:"table_name"`

	// SQL配置字段（config_type='sql'时使用）
	SqlQuery string `gorm:"type:text" json:"sql_query"`

	// 数据配置（JSONB）
	DataConfig JSONB `gorm:"type:jsonb;not null;default:'{}';index:idx_query_services_data_config_gin,type:gin" json:"data_config"`
	/* data_config 结构示例：
	{
	  "geometry": {
	    "has_geometry": true,
	    "column": "geom",
	    "srid": 4326,
	    "types": ["Point"],
	    "extent": {"minX": 116.0, "minY": 39.0, "maxX": 117.0, "maxY": 40.0}
	  },
	  "default_fields": ["id", "name", "geom"],
	  "filterable_fields": ["name", "category"]
	}
	*/

	// 协议配置（JSONB）
	Protocols JSONB `gorm:"type:jsonb;not null;default:'{\"rest_api\":{\"enabled\":true,\"formats\":[\"json\",\"csv\",\"geojson\"]},\"ogc_features\":{\"enabled\":false,\"version\":\"1.0\"}}';index:idx_query_services_protocols_gin,type:gin" json:"protocols"`

	// 访问控制
	PublicAccess bool `gorm:"default:false" json:"public_access"`
	MaxFeatures  int  `gorm:"default:1000" json:"max_features"`

	// 状态
	Status       string `gorm:"size:50;default:'active';check:status IN ('active', 'inactive', 'error');index:idx_query_services_status" json:"status"`
	ErrorMessage string `gorm:"type:text" json:"error_message"`

	// 审计字段
	CreatedBy uint      `gorm:"not null" json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (QueryService) TableName() string {
	return "service.query_services"
}

// IsDuckDBSQL 是否为 DuckDB 联邦 SQL 模式（engine_id 为 nil）
func (q *QueryService) IsDuckDBSQL() bool {
	return q.ConfigType == "sql" && (q.EngineID == nil || *q.EngineID == 0)
}

// GetEngineID 安全获取 EngineID（0 表示未设置）
func (q *QueryService) GetEngineID() uint {
	if q.EngineID == nil {
		return 0
	}
	return *q.EngineID
}

// IsTableMode 是否为表配置模式
func (q *QueryService) IsTableMode() bool {
	return q.ConfigType == "table"
}

// IsObjectTable 是否为对象存储中的文件/目录型表格资源。
func (q *QueryService) IsObjectTable() bool {
	if q.DataConfig == nil {
		return false
	}
	_, ok := q.DataConfig["object_table"].(map[string]interface{})
	return ok
}

// GetObjectTablePhysicalPath 获取对象表的物理路径。
func (q *QueryService) GetObjectTablePhysicalPath() string {
	if q.DataConfig == nil {
		return ""
	}
	objectTable, ok := q.DataConfig["object_table"].(map[string]interface{})
	if !ok {
		return ""
	}
	physicalPath, _ := objectTable["physical_path"].(string)
	return physicalPath
}

// IsSQLMode 是否为SQL配置模式
func (q *QueryService) IsSQLMode() bool {
	return q.ConfigType == "sql"
}

// HasGeometry 是否包含空间字段
func (q *QueryService) HasGeometry() bool {
	if q.DataConfig == nil {
		return false
	}
	geometry, ok := q.DataConfig["geometry"].(map[string]interface{})
	if !ok {
		return false
	}
	hasGeometry, ok := geometry["has_geometry"].(bool)
	return ok && hasGeometry
}

// GetGeometryColumn 获取几何列名
func (q *QueryService) GetGeometryColumn() string {
	if q.DataConfig == nil {
		return ""
	}
	geometry, ok := q.DataConfig["geometry"].(map[string]interface{})
	if !ok {
		return ""
	}
	column, ok := geometry["column"].(string)
	if !ok {
		return ""
	}
	return column
}

// GetSRID 获取坐标系
func (q *QueryService) GetSRID() int {
	if q.DataConfig == nil {
		return 0
	}
	geometry, ok := q.DataConfig["geometry"].(map[string]interface{})
	if !ok {
		return 0
	}
	srid, ok := geometry["srid"].(float64)
	if !ok {
		return 0
	}
	return int(srid)
}

// GetDefaultFields 获取默认返回字段
func (q *QueryService) GetDefaultFields() []string {
	if q.DataConfig == nil {
		return nil
	}
	fields, ok := q.DataConfig["default_fields"].([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(fields))
	for _, f := range fields {
		if str, ok := f.(string); ok {
			result = append(result, str)
		}
	}
	return result
}

// GetFilterableFields 获取可过滤字段
func (q *QueryService) GetFilterableFields() []string {
	if q.DataConfig == nil {
		return nil
	}
	fields, ok := q.DataConfig["filterable_fields"].([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(fields))
	for _, f := range fields {
		if str, ok := f.(string); ok {
			result = append(result, str)
		}
	}
	return result
}

// GetProtocolConfig 获取指定协议的配置
func (q *QueryService) GetProtocolConfig(protocol string) map[string]interface{} {
	if q.Protocols == nil {
		return nil
	}
	config, ok := q.Protocols[protocol].(map[string]interface{})
	if !ok {
		return nil
	}
	return config
}

// IsProtocolEnabled 检查指定协议是否启用
func (q *QueryService) IsProtocolEnabled(protocol string) bool {
	config := q.GetProtocolConfig(protocol)
	if config == nil {
		return false
	}
	enabled, ok := config["enabled"].(bool)
	if !ok {
		return false
	}
	return enabled
}

// IsRESTAPIEnabled 检查 REST API 是否启用
func (q *QueryService) IsRESTAPIEnabled() bool {
	return q.IsProtocolEnabled("rest_api")
}

// IsOGCFeaturesEnabled 检查 OGC Features 是否启用
func (q *QueryService) IsOGCFeaturesEnabled() bool {
	return q.IsProtocolEnabled("ogc_features")
}

// CreateQueryServiceRequest 创建查询服务请求
type CreateQueryServiceRequest struct {
	ServiceName string   `json:"service_name" binding:"required"`
	Title       string   `json:"title" binding:"required"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`

	// 配置方式（table 或 sql）
	ConfigType string `json:"config_type" binding:"required,oneof=table sql"`

	// 存储引擎（table 模式必填；sql 模式 + DuckDB 时为 nil）
	EngineID *uint `json:"engine_id"`

	// 表配置字段由 data_config.locator 服务端派生，仅作为执行快照返回。
	SchemaName string `json:"schema_name"`
	TableName  string `json:"table_name"`

	// SQL配置字段（config_type='sql'时需要）
	SqlQuery string `json:"sql_query"`

	// 数据配置。table 模式必须提供 locator，格式为带 item_id 的 ResourceLocator。
	DataConfig map[string]interface{} `json:"data_config"`

	// 协议配置（可选，使用默认值）
	Protocols map[string]interface{} `json:"protocols"`

	// 访问控制
	PublicAccess bool `json:"public_access"`
	MaxFeatures  int  `json:"max_features" binding:"omitempty,gte=1,lte=10000"`
}

// UpdateQueryServiceRequest 更新查询服务请求
type UpdateQueryServiceRequest struct {
	Title       *string  `json:"title,omitempty"`
	Description *string  `json:"description,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`

	// 数据配置更新
	DataConfig map[string]interface{} `json:"data_config,omitempty"`

	// 协议配置更新
	Protocols map[string]interface{} `json:"protocols,omitempty"`

	// 访问控制更新
	PublicAccess *bool `json:"public_access,omitempty"`
	MaxFeatures  *int  `json:"max_features,omitempty" binding:"omitempty,gte=1,lte=10000"`

	// 状态更新
	Status *string `json:"status,omitempty" binding:"omitempty,oneof=active inactive error"`
}

// QueryServiceDTO 查询服务 DTO
type QueryServiceDTO struct {
	ID uint `json:"id"`

	TenantID    uint     `json:"tenant_id"`
	ServiceName string   `json:"service_name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`

	ConfigType string `json:"config_type"`
	EngineID   *uint  `json:"engine_id"`

	// 表配置
	SchemaName string `json:"schema_name,omitempty"`
	TableName  string `json:"table_name,omitempty"`

	// SQL配置
	SqlQuery string `json:"sql_query,omitempty"`

	// 配置
	DataConfig map[string]interface{} `json:"data_config"`
	Protocols  map[string]interface{} `json:"protocols"`

	// 访问控制
	PublicAccess bool `json:"public_access"`
	MaxFeatures  int  `json:"max_features"`

	// 状态
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`

	// 服务端点
	Endpoints map[string]string `json:"endpoints"`

	// 审计
	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
