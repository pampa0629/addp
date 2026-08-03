package models

// QueryExecutionRequest 是已发布查询服务唯一的结构化查询输入。
type QueryExecutionRequest struct {
	Select  []string         `json:"select,omitempty"`
	Filter  *QueryFilter     `json:"filter,omitempty"`
	OrderBy []QueryOrder     `json:"order_by,omitempty"`
	Page    QueryPageRequest `json:"page"`
	Format  string           `json:"format,omitempty" enums:"json,csv,geojson"`
}

// QueryFilter 使用叶子谓词或 and/or/not 中的一种表达过滤条件。
type QueryFilter struct {
	Field string        `json:"field,omitempty"`
	Op    string        `json:"op,omitempty" enums:"eq,ne,lt,lte,gt,gte,in,is_null,is_not_null,bbox_intersects"`
	Value interface{}   `json:"value,omitempty" swaggertype:"object"`
	And   []QueryFilter `json:"and,omitempty"`
	Or    []QueryFilter `json:"or,omitempty"`
	Not   *QueryFilter  `json:"not,omitempty"`
}

// QueryOrder 是结构化排序项。
type QueryOrder struct {
	Field     string `json:"field"`
	Direction string `json:"direction" enums:"asc,desc"`
}

// QueryPageRequest 是 cursor/keyset 分页输入。
type QueryPageRequest struct {
	Limit  int    `json:"limit,omitempty" minimum:"1" maximum:"10000"`
	Cursor string `json:"cursor,omitempty"`
}

// QueryPageResult 是 cursor/keyset 分页结果。
type QueryPageResult struct {
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// QueryExecutionResult 是 REST 和协议适配层共享的查询结果。
type QueryExecutionResult struct {
	Data           []map[string]interface{} `json:"data"`
	Page           QueryPageResult          `json:"page"`
	ServiceVersion string                   `json:"service_version"`
	Fields         []string                 `json:"-"`
	FeatureIDs     []string                 `json:"-"`
}
