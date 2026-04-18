package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/config"
	_ "github.com/marcboeker/go-duckdb"
)

// DuckDBService DuckDB 联邦查询引擎服务
// 支持湖表（Parquet on MinIO/S3）和关系型表（PG/MySQL）跨源 JOIN
type DuckDBService struct {
	cfg          *config.Config
	systemClient *commonClient.SystemClient
	metaClient   *commonClient.MetaClient
	logger       *slog.Logger
}

// FederatedQueryResult 联邦查询结果
type FederatedQueryResult struct {
	Columns         []string                 `json:"columns"`
	Rows            []map[string]interface{} `json:"rows"`
	RowCount        int                      `json:"row_count"`
	ExecutionTimeMs int64                    `json:"execution_time_ms"`
}

// DataSource 可查询的数据源
type DataSource struct {
	EngineName string      `json:"engine_name"`
	EngineID   uint        `json:"engine_id"`
	EngineType string      `json:"engine_type"`
	Tables     []TableRef  `json:"tables"`
}

// TableRef 表引用（用于自动补全）
type TableRef struct {
	EngineName string   `json:"engine_name"`
	Schema     string   `json:"schema,omitempty"`
	Table      string   `json:"table"`
	ItemType   string   `json:"item_type"` // "table" 或 "lake_table"
	Fields     []string `json:"fields,omitempty"`
	// 湖表专用
	PhysicalPath string `json:"physical_path,omitempty"`
}

// NewDuckDBService 创建 DuckDB 联邦查询服务
func NewDuckDBService(
	cfg *config.Config,
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
) *DuckDBService {
	return &DuckDBService{
		cfg:          cfg,
		systemClient: systemClient,
		metaClient:   metaClient,
		logger:       slog.Default().With("component", "duckdb_service"),
	}
}

// Ping 验证 DuckDB 引擎本身可用（不挂载任何外部引擎）
func (s *DuckDBService) Ping(ctx context.Context) (int64, error) {
	start := time.Now()

	db, err := sql.Open("duckdb", "")
	if err != nil {
		return 0, fmt.Errorf("初始化 DuckDB 失败: %w", err)
	}
	defer db.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var result int
	if err := db.QueryRowContext(pingCtx, "SELECT 1").Scan(&result); err != nil {
		return 0, fmt.Errorf("DuckDB ping 失败: %w", err)
	}

	return time.Since(start).Milliseconds(), nil
}

// ExecuteQuery 执行联邦查询
func (s *DuckDBService) ExecuteQuery(ctx context.Context, tenantID uint, sqlStr string, timeout int) (*FederatedQueryResult, error) {
	if timeout <= 0 {
		timeout = 30
	}

	start := time.Now()

	// 解析 SQL 中引用的引擎名（三段式 engine.schema.table）
	referencedEngines := extractReferencedEngineNames(sqlStr)

	// 只有 SQL 引用了外部引擎时才去拉取引擎列表并挂载
	var engines []commonModels.Engine
	if len(referencedEngines) > 0 {
		var err error
		engines, err = s.systemClient.ListEngines("", tenantID)
		if err != nil {
			return nil, fmt.Errorf("获取引擎列表失败: %w", err)
		}
		// 过滤：只保留 SQL 中实际引用的引擎
		engines = filterEnginesByName(engines, referencedEngines)
	}

	// 初始化 DuckDB 内存连接
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("初始化 DuckDB 失败: %w", err)
	}
	defer db.Close()

	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取 DuckDB 连接失败: %w", err)
	}
	defer conn.Close()

	// 按需挂载引擎 + 构建湖表映射
	var engineLakeTables map[string]map[string]string
	if len(engines) > 0 {
		engineLakeTables = s.buildLakeTableMap(ctx, tenantID, engines)
		if err := s.mountEngines(ctx, conn, engines); err != nil {
			s.logger.Warn("部分引擎挂载失败", "error", err)
		}
	}

	// 改写 SQL（湖表引用 → read_parquet）
	rewriter := NewSQLRewriter(s.metaClient, tenantID)
	rewrittenSQL, err := rewriter.RewriteWithEngines(ctx, sqlStr, engineLakeTables)
	if err != nil {
		s.logger.Warn("SQL 改写失败，使用原始 SQL", "error", err)
		rewrittenSQL = sqlStr
	}

	s.logger.Info("executing federated query",
		"original_sql", sqlStr,
		"rewritten_sql", rewrittenSQL,
		"tenant_id", tenantID,
		"mounted_engines", len(engines))

	// 执行查询
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	rows, err := conn.QueryContext(execCtx, rewrittenSQL)
	if err != nil {
		return nil, fmt.Errorf("DuckDB 查询执行失败: %w", err)
	}
	defer rows.Close()

	// 读取结果
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("获取列信息失败: %w", err)
	}

	var resultRows []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("读取行数据失败: %w", err)
		}
		row := make(map[string]interface{}, len(cols))
		for i, col := range cols {
			row[col] = values[i]
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历结果集失败: %w", err)
	}

	if resultRows == nil {
		resultRows = []map[string]interface{}{}
	}

	return &FederatedQueryResult{
		Columns:         cols,
		Rows:            resultRows,
		RowCount:        len(resultRows),
		ExecutionTimeMs: time.Since(start).Milliseconds(),
	}, nil
}

// GenerateSampleQuery 生成可直接执行的样例查询
// 逻辑：优先取第一个湖表，其次取第一个关系型表，都没有则返回 SELECT version()
func (s *DuckDBService) GenerateSampleQuery(ctx context.Context, tenantID uint) string {
	sources, err := s.GetSources(ctx, tenantID)
	if err != nil || len(sources) == 0 {
		return "SELECT version() AS duckdb_version, current_date AS today"
	}

	// 优先湖表
	for _, src := range sources {
		for _, t := range src.Tables {
			if t.ItemType == "lake_table" {
				name := sanitizeName(src.EngineName)
				if t.Schema != "" {
					return fmt.Sprintf("SELECT *\nFROM %s.%s.%s\nLIMIT 10", name, t.Schema, t.Table)
				}
				return fmt.Sprintf("SELECT *\nFROM %s.%s\nLIMIT 10", name, t.Table)
			}
		}
	}

	// 其次关系型表
	for _, src := range sources {
		for _, t := range src.Tables {
			name := sanitizeName(src.EngineName)
			if t.Schema != "" {
				return fmt.Sprintf("SELECT *\nFROM %s.%s.%s\nLIMIT 10", name, t.Schema, t.Table)
			}
			return fmt.Sprintf("SELECT *\nFROM %s.%s\nLIMIT 10", name, t.Table)
		}
	}

	return "SELECT version() AS duckdb_version, current_date AS today"
}

// GetSources 获取当前租户下所有可查询的数据源
func (s *DuckDBService) GetSources(ctx context.Context, tenantID uint) ([]DataSource, error) {
	engines, err := s.systemClient.ListEngines("", tenantID)
	if err != nil {
		return nil, fmt.Errorf("获取引擎列表失败: %w", err)
	}

	var sources []DataSource
	for _, engine := range engines {
		source := DataSource{
			EngineName: engine.Name,
			EngineID:   engine.ID,
			EngineType: engine.EngineType,
		}

		switch engine.EngineType {
		case "minio", "s3":
			// 对象存储：从 Meta 获取湖表列表
			tables := s.getLakeTables(ctx, tenantID, engine)
			source.Tables = tables
		case "postgresql", "mysql":
			// 关系型数据库：列出表
			tables := s.getRelationalTables(ctx, tenantID, engine)
			source.Tables = tables
		default:
			continue
		}

		if len(source.Tables) > 0 {
			sources = append(sources, source)
		}
	}

	return sources, nil
}

// mountEngines 将各引擎挂载到 DuckDB
func (s *DuckDBService) mountEngines(ctx context.Context, conn *sql.Conn, engines []commonModels.Engine) error {
	var errs []string

	for _, engine := range engines {
		switch engine.EngineType {
		case "minio", "s3":
			if err := s.mountObjectStorage(ctx, conn, engine); err != nil {
				errs = append(errs, fmt.Sprintf("挂载 %s(%s) 失败: %v", engine.Name, engine.EngineType, err))
			}
		case "postgresql":
			if err := s.mountPostgres(ctx, conn, engine); err != nil {
				errs = append(errs, fmt.Sprintf("挂载 %s(postgresql) 失败: %v", engine.Name, err))
			}
		case "mysql":
			if err := s.mountMySQL(ctx, conn, engine); err != nil {
				errs = append(errs, fmt.Sprintf("挂载 %s(mysql) 失败: %v", engine.Name, err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("部分引擎挂载失败: %s", strings.Join(errs, "; "))
	}
	return nil
}

// mountObjectStorage 挂载 MinIO/S3 到 DuckDB httpfs
func (s *DuckDBService) mountObjectStorage(ctx context.Context, conn *sql.Conn, engine commonModels.Engine) error {
	connInfo := engine.ConnectionInfo
	endpoint := getString(connInfo, "endpoint")
	accessKey := getString(connInfo, "access_key")
	secretKey := getString(connInfo, "secret_key")
	region := getString(connInfo, "region")
	if region == "" {
		region = "us-east-1"
	}

	// 安装并加载 httpfs 扩展
	stmts := []string{
		"INSTALL httpfs",
		"LOAD httpfs",
		fmt.Sprintf("SET s3_endpoint='%s'", endpoint),
		fmt.Sprintf("SET s3_access_key_id='%s'", accessKey),
		fmt.Sprintf("SET s3_secret_access_key='%s'", secretKey),
		fmt.Sprintf("SET s3_region='%s'", region),
		"SET s3_use_ssl=false",
		"SET s3_url_style='path'",
	}

	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			// INSTALL 可能已安装，忽略该错误
			if !strings.Contains(err.Error(), "already installed") &&
				!strings.Contains(err.Error(), "already loaded") {
				s.logger.Warn("DuckDB httpfs setup stmt failed", "stmt", stmt, "error", err)
			}
		}
	}

	return nil
}

// mountPostgres 挂载 PostgreSQL 到 DuckDB
func (s *DuckDBService) mountPostgres(ctx context.Context, conn *sql.Conn, engine commonModels.Engine) error {
	connInfo := engine.ConnectionInfo
	host := getString(connInfo, "host")
	port := getInt(connInfo, "port", 5432)
	user := getString(connInfo, "username")
	if user == "" {
		user = getString(connInfo, "user")
	}
	password := getString(connInfo, "password")
	database := getString(connInfo, "database")

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
		host, port, user, password, database)

	stmts := []string{
		"INSTALL postgres",
		"LOAD postgres",
		fmt.Sprintf("ATTACH '%s' AS %s (TYPE postgres, READ_ONLY)", dsn, sanitizeName(engine.Name)),
	}

	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			if !strings.Contains(err.Error(), "already installed") &&
				!strings.Contains(err.Error(), "already loaded") &&
				!strings.Contains(err.Error(), "already exists") {
				return fmt.Errorf("postgres attach failed: %w", err)
			}
		}
	}

	return nil
}

// mountMySQL 挂载 MySQL 到 DuckDB
func (s *DuckDBService) mountMySQL(ctx context.Context, conn *sql.Conn, engine commonModels.Engine) error {
	connInfo := engine.ConnectionInfo
	host := getString(connInfo, "host")
	port := getInt(connInfo, "port", 3306)
	user := getString(connInfo, "username")
	if user == "" {
		user = getString(connInfo, "user")
	}
	password := getString(connInfo, "password")
	database := getString(connInfo, "database")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", user, password, host, port, database)

	stmts := []string{
		"INSTALL mysql",
		"LOAD mysql",
		fmt.Sprintf("ATTACH '%s' AS %s (TYPE mysql, READ_ONLY)", dsn, sanitizeName(engine.Name)),
	}

	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			if !strings.Contains(err.Error(), "already installed") &&
				!strings.Contains(err.Error(), "already loaded") &&
				!strings.Contains(err.Error(), "already exists") {
				return fmt.Errorf("mysql attach failed: %w", err)
			}
		}
	}

	return nil
}

// buildLakeTableMap 构建湖表映射：engineName -> (schema.table 或 table) -> physicalPath
func (s *DuckDBService) buildLakeTableMap(ctx context.Context, tenantID uint, engines []commonModels.Engine) map[string]map[string]string {
	result := make(map[string]map[string]string)
	if s.metaClient == nil {
		return result
	}
	s.metaClient.SetTenantID(&tenantID)
	for _, engine := range engines {
		if engine.EngineType != "minio" && engine.EngineType != "s3" {
			continue
		}
		tree, err := s.metaClient.GetMetadataTree(engine.ID)
		if err != nil {
			s.logger.Warn("获取元数据树失败", "engine_id", engine.ID, "error", err)
			continue
		}
		tables := make(map[string]string)
		for _, item := range tree.Items {
			if item.ItemType != "lake_table" {
				continue
			}
			physicalPath := ""
			if item.Attributes != nil {
				if p, ok := item.Attributes["physical_path"].(string); ok {
					physicalPath = p
				}
			}
			if physicalPath == "" {
				continue
			}
			tables[item.Name] = physicalPath
			if item.FullName != "" && item.FullName != item.Name {
				tables[item.FullName] = physicalPath
			}
		}
		if len(tables) > 0 {
			result[engine.Name] = tables
			// 同时用 sanitized 名作为 key，与 SQL 中的引用保持一致
			if sn := sanitizeName(engine.Name); sn != engine.Name {
				result[sn] = tables
			}
		}
	}
	return result
}

// getLakeTables 从 Meta 获取对象存储引擎下的湖表列表
func (s *DuckDBService) getLakeTables(ctx context.Context, tenantID uint, engine commonModels.Engine) []TableRef {
	if s.metaClient == nil {
		return nil
	}

	// 服务间调用需要携带租户 ID
	s.metaClient.SetTenantID(&tenantID)

	tree, err := s.metaClient.GetMetadataTree(engine.ID)
	if err != nil {
		s.logger.Warn("获取元数据树失败", "engine_id", engine.ID, "error", err)
		return nil
	}

	var tables []TableRef
	for _, item := range tree.Items {
		if item.ItemType != "lake_table" {
			continue
		}
		physicalPath := ""
		if item.Attributes != nil {
			if p, ok := item.Attributes["physical_path"].(string); ok {
				physicalPath = p
			}
		}
		tables = append(tables, TableRef{
			EngineName:   engine.Name,
			Table:        item.Name,
			ItemType:     "lake_table",
			PhysicalPath: physicalPath,
		})
	}

	return tables
}

// getRelationalTables 获取关系型数据库的表列表
func (s *DuckDBService) getRelationalTables(ctx context.Context, tenantID uint, engine commonModels.Engine) []TableRef {
	if s.metaClient == nil {
		return nil
	}

	s.metaClient.SetTenantID(&tenantID)
	tree, err := s.metaClient.GetMetadataTree(engine.ID)
	if err != nil {
		s.logger.Warn("获取元数据树失败", "engine_id", engine.ID, "error", err)
		return nil
	}

	var tables []TableRef
	for _, item := range tree.Items {
		if item.ItemType == "lake_table" {
			continue
		}
		// 从 full_name 解析 schema.table
		parts := strings.SplitN(item.FullName, ".", 2)
		schema := ""
		if len(parts) == 2 {
			schema = parts[0]
		}
		tables = append(tables, TableRef{
			EngineName: engine.Name,
			Schema:     schema,
			Table:      item.Name,
			ItemType:   "table",
		})
	}

	return tables
}

// ===== 辅助函数 =====

// extractReferencedEngineNames 从 SQL 中提取可能的引擎名（三段式和两段式引用的第一段）
func extractReferencedEngineNames(sql string) map[string]bool {
	names := make(map[string]bool)
	// 三段式 engine.schema.table
	for _, ref := range extractTableRefs(sql) {
		parts := strings.SplitN(ref, ".", 3)
		if len(parts) == 3 {
			names[parts[0]] = true
		}
	}
	// 两段式 engine.table（湖表场景）
	for _, ref := range extractTwoPartTableRefs(sql) {
		parts := strings.SplitN(ref, ".", 2)
		if len(parts) == 2 {
			names[parts[0]] = true // 可能是引擎名或 schema 名，filterEnginesByName 会过滤
		}
	}
	return names
}

// filterEnginesByName 只保留名称在 referenced 集合中的引擎
func filterEnginesByName(engines []commonModels.Engine, referenced map[string]bool) []commonModels.Engine {
	if len(referenced) == 0 {
		return nil
	}
	var result []commonModels.Engine
	for _, e := range engines {
		if referenced[e.Name] || referenced[sanitizeName(e.Name)] {
			result = append(result, e)
		}
	}
	return result
}

func getString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]interface{}, key string, defaultVal int) int {
	if m == nil {
		return defaultVal
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return defaultVal
}

// sanitizeName 将引擎名称转换为合法的 DuckDB 标识符
func sanitizeName(name string) string {
	// 替换非字母数字字符为下划线
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	result := b.String()
	// 确保不以数字开头
	if len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		result = "_" + result
	}
	if result == "" {
		result = "engine"
	}
	return result
}
