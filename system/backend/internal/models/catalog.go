package models

// CatalogListChildrenRequest 实时 catalog 子节点浏览请求。
type CatalogListChildrenRequest struct {
	Path    CatalogPath            `json:"path"`
	Options CatalogListOptions     `json:"options,omitempty"`
	Filter  map[string]interface{} `json:"filter,omitempty"`
}

// CatalogListChildrenResponse 实时 catalog 子节点浏览响应。
type CatalogListChildrenResponse struct {
	Nodes []CatalogNode `json:"nodes"`
}

// CatalogPath 表达跨引擎的结构化 catalog 路径。
type CatalogPath struct {
	Version  string           `json:"version,omitempty" example:"catalog.path/v1"`
	EngineID uint             `json:"engine_id,omitempty" example:"1"`
	Segments []CatalogSegment `json:"segments"`
}

// CatalogSegment 表达 catalog 路径中的一层。
type CatalogSegment struct {
	Term string `json:"term" example:"schema"`
	Kind string `json:"kind" example:"namespace"`
	Name string `json:"name" example:"public"`
}

// CatalogNode 表达实时 catalog 浏览返回的中性节点。
type CatalogNode struct {
	Name        string                 `json:"name" example:"public"`
	Path        CatalogPath            `json:"path"`
	Term        string                 `json:"term" example:"schema"`
	Kind        string                 `json:"kind" example:"namespace"`
	IsContainer bool                   `json:"is_container" example:"true"`
	IsItem      bool                   `json:"is_item" example:"false"`
	Stats       map[string]interface{} `json:"stats,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	Actions     []string               `json:"actions,omitempty"`
}

// CatalogListOptions 实时 catalog 列表选项。
type CatalogListOptions struct {
	Recursive bool `json:"recursive,omitempty" example:"false"`
	Limit     int  `json:"limit,omitempty" example:"100"`
	Offset    int  `json:"offset,omitempty" example:"0"`
}
