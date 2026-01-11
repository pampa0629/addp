package models

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
}

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

// MetaCleanupExecuteResult - Meta清理执行结果
type MetaCleanupExecuteResult struct {
	DeletedNodes        int      `json:"deleted_nodes"`
	DeletedItems        int      `json:"deleted_items"`
	DeletedFingerprints int      `json:"deleted_fingerprints"`
	Errors              []string `json:"errors"`
}
