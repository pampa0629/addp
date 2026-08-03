package models

import (
	"time"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
)

const QueryServiceSourceSnapshotKey = "source_snapshot"

// QueryServiceSourceRef 记录表模式查询服务发布时的 Meta item 身份事实。
type QueryServiceSourceRef struct {
	ItemID          uint       `json:"item_id"`
	ItemFingerprint string     `json:"item_fingerprint"`
	ScannedAt       *time.Time `json:"scanned_at,omitempty"`
	DataUpdatedAt   *time.Time `json:"data_updated_at,omitempty"`
}

// QueryServiceOutputContract 是 SQL 检测 API 与发布请求共享的输出契约。
// captured_at、query_hash 和 dependency_hash 由 Service 在发布时生成。
type QueryServiceOutputContract struct {
	Table   *datatype.TableInfo   `json:"table,omitempty"`
	Spatial *datatype.SpatialInfo `json:"spatial,omitempty"`
}

// SQLQueryOutputContractRequest 请求检测一个 SQL 查询的输出契约。
type SQLQueryOutputContractRequest struct {
	EngineID uint   `json:"engine_id" binding:"required"`
	SQL      string `json:"sql" binding:"required"`
}

// QueryServiceDependencySnapshot 是查询服务运行和对外契约依赖的冻结事实。
type QueryServiceDependencySnapshot struct {
	Source                   *QueryServiceSourceRef       `json:"source,omitempty"`
	CapturedAt               time.Time                    `json:"captured_at"`
	DependencyHash           string                       `json:"dependency_hash"`
	VerificationStatus       string                       `json:"verification_status,omitempty"`
	QueryHash                string                       `json:"query_hash,omitempty"`
	Table                    *datatype.TableInfo          `json:"table,omitempty"`
	Spatial                  *datatype.SpatialInfo        `json:"spatial,omitempty"`
	ObjectTable              *dataitem.ItemDescriptor     `json:"object_table,omitempty"`
	FederatedSourceEngineIDs []uint                       `json:"federated_source_engine_ids,omitempty"`
	FederatedObjectTables    map[string]map[string]string `json:"federated_object_tables,omitempty"`
}

// QueryServiceSnapshotDiff 是显式检查上游事实后的快照差异。
type QueryServiceSnapshotDiff struct {
	ServiceID               uint                            `json:"service_id"`
	Status                  string                          `json:"status"`
	PublishedDependencyHash string                          `json:"published_dependency_hash"`
	CurrentDependencyHash   string                          `json:"current_dependency_hash,omitempty"`
	SourceChanged           bool                            `json:"source_changed"`
	TableChanged            bool                            `json:"table_changed"`
	SpatialChanged          bool                            `json:"spatial_changed"`
	ObjectTableChanged      bool                            `json:"object_table_changed"`
	PublishedSnapshot       *QueryServiceDependencySnapshot `json:"published_snapshot,omitempty"`
	CurrentSnapshot         *QueryServiceDependencySnapshot `json:"current_snapshot,omitempty"`
}

// SourceSnapshot 从 data_config 读取唯一的查询服务依赖快照。
func (q *QueryService) SourceSnapshot() *QueryServiceDependencySnapshot {
	if q == nil || q.DataConfig == nil {
		return nil
	}
	payload := commonJSON.InterfaceMap(q.DataConfig[QueryServiceSourceSnapshotKey])
	if len(payload) == 0 {
		return nil
	}
	var snapshot QueryServiceDependencySnapshot
	if err := commonJSON.DecodeStruct(payload, &snapshot); err != nil {
		return nil
	}
	return &snapshot
}
