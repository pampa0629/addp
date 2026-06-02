package models

import (
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

// CatalogListChildrenRequest 实时 catalog 子节点浏览请求。
type CatalogListChildrenRequest struct {
	Path    CatalogPath        `json:"path"`
	Options CatalogListOptions `json:"options,omitempty"`
}

// CatalogListChildrenResponse 实时 catalog 子节点浏览响应。
type CatalogListChildrenResponse struct {
	Nodes []CatalogEntry `json:"nodes"`
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

// CatalogEntry 表达实时 catalog 浏览返回的中性条目。
type CatalogEntry struct {
	Name      string                      `json:"name" example:"public"`
	Path      CatalogPath                 `json:"path"`
	Term      string                      `json:"term" example:"schema"`
	Kind      string                      `json:"kind" example:"namespace"`
	Role      string                      `json:"role" example:"branch"`
	Table     *datatype.TableInfo         `json:"table,omitempty"`
	Storage   *plugin.CatalogStorageFacts `json:"storage,omitempty"`
	LeafCount *int                        `json:"leaf_count,omitempty"`
	UpdatedAt *time.Time                  `json:"updated_at,omitempty"`
}

// CatalogListOptions 实时 catalog 列表选项。
type CatalogListOptions struct {
	Recursive bool `json:"recursive,omitempty" example:"false"`
	Limit     int  `json:"limit,omitempty" example:"100"`
	Offset    int  `json:"offset,omitempty" example:"0"`
}
