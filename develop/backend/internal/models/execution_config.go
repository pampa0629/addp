package models

// WorkflowExecutionConfig 工作流执行配置（基础）
type WorkflowExecutionConfig struct {
	EngineID uint `json:"engine_id"` // 工作流引擎实例 ID

	// 引擎特定配置（可选）
	EngineSpecific map[string]interface{} `json:"engine_specific,omitempty"`
}

// SparkWorkflowEngineConfig Spark 工作流引擎特定配置
type SparkWorkflowEngineConfig struct {
	SparkClusterID uint `json:"spark_cluster_id"` // 真实 spark 通用引擎资源 ID
}
