package models

import (
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

// EngineCatalogListChildrenRequest 实时 catalog 子节点浏览请求。
type EngineCatalogListChildrenRequest struct {
	Path    EngineCatalogPath        `json:"path"`
	Options EngineCatalogListOptions `json:"options,omitempty"`
}

// EngineCatalogListChildrenResponse 实时 catalog 子节点浏览响应。
type EngineCatalogListChildrenResponse struct {
	Nodes []EngineCatalogEntry `json:"nodes"`
}

// EngineCatalogDescribeFactsRequest 实时 catalog 叶子事实请求。
type EngineCatalogDescribeFactsRequest struct {
	Path EngineCatalogPath `json:"path"`
}

// EngineCatalogPath 表达跨引擎的结构化 catalog 路径。
type EngineCatalogPath struct {
	Version  string                 `json:"version,omitempty" example:"catalog.path/v1"`
	EngineID uint                   `json:"engine_id,omitempty" example:"1"`
	Segments []EngineCatalogSegment `json:"segments"`
}

// EngineCatalogSegment 表达 catalog 路径中的一层。
type EngineCatalogSegment struct {
	Term string `json:"term" example:"schema"`
	Kind string `json:"kind" example:"namespace"`
	Name string `json:"name" example:"public"`
}

// EngineCatalogEntry 表达实时 catalog 浏览返回的中性条目。
type EngineCatalogEntry struct {
	Name      string                            `json:"name" example:"public"`
	Path      EngineCatalogPath                 `json:"path"`
	Term      string                            `json:"term" example:"schema"`
	Kind      string                            `json:"kind" example:"namespace"`
	Role      string                            `json:"role" example:"branch"`
	Table     *datatype.TableInfo               `json:"table,omitempty"`
	Storage   *plugin.EngineCatalogStorageFacts `json:"storage,omitempty"`
	LeafCount *int                              `json:"leaf_count,omitempty"`
	UpdatedAt *time.Time                        `json:"updated_at,omitempty"`
}

// EngineCatalogListOptions 实时 catalog 列表选项。
type EngineCatalogListOptions struct {
	Recursive bool `json:"recursive,omitempty" example:"false"`
	Limit     int  `json:"limit,omitempty" example:"100"`
	Offset    int  `json:"offset,omitempty" example:"0"`
}
