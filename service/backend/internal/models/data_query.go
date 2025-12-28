package models

// DataQueryRequest 数据查询请求
type DataQueryRequest struct {
	EngineID uint     `json:"engine_id" binding:"required"`
	Schema       string   `json:"schema" binding:"required"`
	Table        string   `json:"table" binding:"required"`
	Columns      []string `json:"columns,omitempty"`           // 指定列，为空表示全部列
	Filter       string   `json:"filter,omitempty"`            // WHERE 条件（需要防 SQL 注入）
	OrderBy      string   `json:"order_by,omitempty"`          // 排序字段
	Page         int      `json:"page,omitempty"`              // 页码（从 1 开始）
	PageSize     int      `json:"page_size,omitempty"`         // 每页大小
	Geometry     bool     `json:"geometry,omitempty"`          // 是否包含几何字段
	GeometryType string   `json:"geometry_type,omitempty"`     // 几何输出格式: 'geojson', 'wkt', 'wkb'
}

// DataQueryResponse 数据查询响应
type DataQueryResponse struct {
	Columns    []ColumnInfo    `json:"columns"`
	Rows       [][]interface{} `json:"rows"`
	TotalCount int64           `json:"total_count"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	HasMore    bool            `json:"has_more"`
}

// ColumnInfo 列信息
type ColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

// AggregationRequest 聚合查询请求
type AggregationRequest struct {
	EngineID uint              `json:"engine_id" binding:"required"`
	Schema     string            `json:"schema" binding:"required"`
	Table      string            `json:"table" binding:"required"`
	GroupBy    []string          `json:"group_by,omitempty"`    // 分组字段
	Aggregates []AggregateColumn `json:"aggregates"`            // 聚合列
	Filter     string            `json:"filter,omitempty"`      // WHERE 条件
	Having     string            `json:"having,omitempty"`      // HAVING 条件
	OrderBy    string            `json:"order_by,omitempty"`    // 排序
	Limit      int               `json:"limit,omitempty"`       // 限制结果数
}

// AggregateColumn 聚合列定义
type AggregateColumn struct {
	Column   string `json:"column" binding:"required"`              // 列名
	Function string `json:"function" binding:"required,oneof=count sum avg min max"` // 聚合函数
	Alias    string `json:"alias,omitempty"`                        // 别名
}

// AggregationResponse 聚合查询响应
type AggregationResponse struct {
	Columns []ColumnInfo    `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
	Count   int             `json:"count"`
}
