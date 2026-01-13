package models

import "time"

// InvalidEngineDetail - 无效引擎详情
type InvalidEngineDetail struct {
	EngineID      uint   `json:"engine_id"`
	EngineName    string `json:"engine_name"`
	AffectedNodes int    `json:"affected_nodes"`
	AffectedItems int    `json:"affected_items"`
	Reason        string `json:"reason"` // "engine已删除"/"engine已禁用"
}

// OrphanItemDetail - 孤儿数据详情
type OrphanItemDetail struct {
	ItemID   uint   `json:"item_id"`
	ItemName string `json:"item_name"`
	NodeID   uint   `json:"node_id"`
	Reason   string `json:"reason"`
}

// MeilisearchRecordInfo - Meilisearch 记录信息
type MeilisearchRecordInfo struct {
	AssetID   string `json:"asset_id"`   // 资产ID
	AssetType string `json:"asset_type"` // table / object
	EngineID  uint   `json:"engine_id"`  // 引擎ID
	TenantID  uint   `json:"tenant_id"`  // 租户ID
	Name      string `json:"name"`       // 名称
	Reason    string `json:"reason"`     // 原因(引擎已删除/软删除等)
}

// MinIOFileInfo - MinIO 文件信息
type MinIOFileInfo struct {
	Bucket   string    `json:"bucket"`   // bucket 名称
	Key      string    `json:"key"`      // 对象键(路径)
	Size     int64     `json:"size"`     // 文件大小(字节)
	Modified time.Time `json:"modified"` // 最后修改时间
	Reason   string    `json:"reason"`   // 原因(引擎已删除/过期等)
}

// MetaCleanupStatistics - Meta模块垃圾数据统计
type MetaCleanupStatistics struct {
	// 无效引擎的数据
	InvalidEngines struct {
		Count   int                   `json:"count"`
		Details []InvalidEngineDetail `json:"details"`
	} `json:"invalid_engines"`

	// 孤儿数据项（node_id不存在）
	OrphanItems struct {
		Count  int                `json:"count"`
		Sample []OrphanItemDetail `json:"sample"` // 最多返回10条样本
	} `json:"orphan_items"`

	// 过期数据（长期未扫描）
	ExpiredData struct {
		Count         int `json:"count"`
		ThresholdDays int `json:"threshold_days"` // 过期阈值（天）
	} `json:"expired_data"`

	// 软删除数据
	SoftDeleted struct {
		Nodes      int  `json:"nodes"`
		Items      int  `json:"items"`
		CanRecover bool `json:"can_recover"`
	} `json:"soft_deleted"`

	// 重复fingerprint
	DuplicateFingerprints struct {
		Count int `json:"count"`
	} `json:"duplicate_fingerprints"`

	// Meilisearch 索引统计
	MeilisearchIndexes struct {
		Count  int                      `json:"count"`   // assets 索引中的垃圾记录总数
		ByType map[string]int           `json:"by_type"` // 按资产类型分组 (table/object)
		Sample []MeilisearchRecordInfo  `json:"sample"`  // 样本记录（最多10条）
	} `json:"meilisearch_indexes"`

	// MinIO 文件统计
	MinIOFiles struct {
		Count          int             `json:"count"`             // 垃圾文件总数
		TotalSizeBytes int64           `json:"total_size_bytes"`  // 总大小(字节)
		TotalSizeMB    float64         `json:"total_size_mb"`     // 总大小(MB)
		ByBucket       map[string]int  `json:"by_bucket"`         // 按 bucket 分组统计
		Sample         []MinIOFileInfo `json:"sample"`            // 样本文件(最多10条)
	} `json:"minio_files"`
}

// MetaCleanupExecuteResult - Meta清理执行结果
type MetaCleanupExecuteResult struct {
	DeletedNodes              int      `json:"deleted_nodes"`
	DeletedItems              int      `json:"deleted_items"`
	DeletedFingerprints       int      `json:"deleted_fingerprints"`
	DeletedMeilisearchIndexes int      `json:"deleted_meilisearch_indexes"` // 删除的索引记录数
	DeletedMinIOFiles         int      `json:"deleted_minio_files"`         // 删除的文件数
	FreedSpaceMB              float64  `json:"freed_space_mb"`              // 释放的空间(MB)
	Errors                    []string `json:"errors"`
}
