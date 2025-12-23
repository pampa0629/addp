package models

// ExecutionConfig 执行配置接口
type ExecutionConfig interface {
	GetType() string
}

// SQLExecutionConfig SQL 执行配置
type SQLExecutionConfig struct {
	Type         string `json:"type"`           // "sql"
	DataSourceID uint   `json:"data_source_id"` // PostgreSQL/MySQL/Spark SQL 资源 ID
}

// GetType 实现 ExecutionConfig 接口
func (c SQLExecutionConfig) GetType() string {
	return "sql"
}

// WorkflowExecutionConfig 工作流执行配置（基础）
type WorkflowExecutionConfig struct {
	Type       string `json:"type"`        // "workflow"
	EngineID   uint   `json:"engine_id"`   // 工作流引擎 ID
	EngineType string `json:"engine_type"` // "api.geopandas" | "api.spark_sedona"

	// 引擎特定配置（可选）
	EngineSpecific map[string]interface{} `json:"engine_specific,omitempty"`
}

// GetType 实现 ExecutionConfig 接口
func (c WorkflowExecutionConfig) GetType() string {
	return "workflow"
}

// SparkSedonaEngineConfig Spark Sedona 引擎特定配置
type SparkSedonaEngineConfig struct {
	SparkClusterID uint `json:"spark_cluster_id"` // Spark 运行时 ID
}
