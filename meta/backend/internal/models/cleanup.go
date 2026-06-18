package models

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

// MetaCleanupStatistics - Meta 模块资源回收候选统计
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

	// 逻辑清理候选：已离开活跃路径、可进入物理清理评估的数据
	LogicalCleanupCandidates struct {
		Nodes      int  `json:"nodes"`
		Items      int  `json:"items"`
		CanRecover bool `json:"can_recover"`
	} `json:"logical_cleanup_candidates"`

	// 重复fingerprint
	DuplicateFingerprints struct {
		Count int `json:"count"`
	} `json:"duplicate_fingerprints"`

	// Meilisearch 索引统计
	MeilisearchIndexes struct {
		Count  int                     `json:"count"`   // assets 索引中的待回收记录总数
		ByType map[string]int          `json:"by_type"` // 按资产类型分组 (table/object)
		Sample []MeilisearchRecordInfo `json:"sample"`  // 样本记录（最多10条）
	} `json:"meilisearch_indexes"`

	// 扫描任务定义残留
	ScanTaskDefinitions struct {
		Count int `json:"count"`
	} `json:"scan_task_definitions"`
}

// MetaCleanupExecuteResult - Meta 资源回收执行结果
type MetaCleanupExecuteResult struct {
	DeletedNodes                int      `json:"deleted_nodes"`
	DeletedItems                int      `json:"deleted_items"`
	DeletedFingerprints         int      `json:"deleted_fingerprints"`
	DeletedMeilisearchIndexes   int      `json:"deleted_meilisearch_indexes"` // 删除的索引记录数
	DisabledScanTaskDefinitions int      `json:"disabled_scan_task_definitions"`
	DeletedScanTaskDefinitions  int      `json:"deleted_scan_task_definitions"`
	Errors                      []string `json:"errors"`
}
