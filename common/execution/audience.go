package execution

// Execution Authorization audience 使用逻辑模块或 Runtime 标识，
// 不使用 addp- 机器身份前缀。
const (
	AudienceModel    = "model"
	AudienceQuality  = "quality"
	AudienceDevelop  = "develop"
	AudienceTransfer = "transfer"
	AudienceService  = "service"
	AudienceDuckDB   = "duckdb"
)
