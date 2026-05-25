package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/duckdb"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/config"
	_ "github.com/marcboeker/go-duckdb"
)

// DuckDBService DuckDB 联邦查询引擎服务
// 支持对象存储表（Parquet/ORC/Avro on MinIO/S3）和关系型表（PG/MySQL）跨源 JOIN
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
	EngineName string     `json:"engine_name"`
	EngineID   uint       `json:"engine_id"`
	EngineType string     `json:"engine_type"`
	Tables     []TableRef `json:"tables"`
}

// TableRef 表引用（用于自动补全）
type TableRef struct {
	EngineName string   `json:"engine_name"`
	Schema     string   `json:"schema,omitempty"`
	Table      string   `json:"table"`
	ItemType   string   `json:"item_type"` // table
	Fields     []string `json:"fields,omitempty"`

	PhysicalPath string `json:"physical_path,omitempty"`
	Format       string `json:"format,omitempty"`
	Layout       string `json:"layout,omitempty"`
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

	db, err := duckdb.OpenDB()
	if err != nil {
		return 0, err
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

	session, err := duckdb.PrepareFederatedQuery(ctx, tenantID, sqlStr, s.systemClient, s.metaClient)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	s.logger.Info("executing federated query",
		"original_sql", sqlStr,
		"rewritten_sql", session.RewrittenSQL,
		"tenant_id", tenantID)

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	result, err := duckdb.ExecuteQuery(execCtx, session.Conn, session.RewrittenSQL)
	if err != nil {
		return nil, err
	}

	return &FederatedQueryResult{
		Columns:         result.Columns,
		Rows:            result.Rows,
		RowCount:        result.RowCount,
		ExecutionTimeMs: time.Since(start).Milliseconds(),
	}, nil
}

// GenerateSampleQuery 生成可直接执行的样例查询
func (s *DuckDBService) GenerateSampleQuery(ctx context.Context, tenantID uint) string {
	sources, err := s.GetSources(ctx, tenantID)
	if err != nil || len(sources) == 0 {
		return "SELECT version() AS duckdb_version, current_date AS today"
	}

	// 优先对象存储表
	for _, src := range sources {
		for _, t := range src.Tables {
			if t.PhysicalPath != "" {
				name := duckdb.SanitizeName(src.EngineName)
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
			name := duckdb.SanitizeName(src.EngineName)
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

		switch {
		case duckdb.IsObjectTableEngine(engine.EngineType):
			source.Tables = s.getObjectTables(ctx, tenantID, engine)
		case duckdb.IsRelationalMountEngine(engine.EngineType):
			source.Tables = s.getRelationalTables(ctx, tenantID, engine)
		default:
			continue
		}

		if len(source.Tables) > 0 {
			sources = append(sources, source)
		}
	}

	return sources, nil
}

// getObjectTables 从 Meta 获取对象存储引擎下可被 DuckDB 查询的表格对象。
func (s *DuckDBService) getObjectTables(ctx context.Context, tenantID uint, engine commonModels.Engine) []TableRef {
	if s.metaClient == nil {
		return nil
	}

	tree, err := s.metaClient.WithTenantID(tenantID).GetMetadataTree(engine.ID)
	if err != nil {
		s.logger.Warn("获取元数据树失败", "engine_id", engine.ID, "error", err)
		return nil
	}

	var tables []TableRef
	for _, item := range tree.Items {
		if !duckdb.IsObjectTableItem(item) {
			continue
		}
		physicalPath := ""
		formatName := ""
		layout := ""
		if item.Attributes != nil {
			physicalPath = commonJSON.String(item.Attributes, "storage", "physical_path")
			formatName = commonJSON.String(item.Attributes, "item", "format")
			layout = commonJSON.String(item.Attributes, "item", "layout")
		}
		tables = append(tables, TableRef{
			EngineName:   engine.Name,
			Table:        item.Name,
			ItemType:     "table",
			PhysicalPath: physicalPath,
			Format:       formatName,
			Layout:       layout,
		})
	}

	return tables
}

// getRelationalTables 获取关系型数据库的表列表
func (s *DuckDBService) getRelationalTables(ctx context.Context, tenantID uint, engine commonModels.Engine) []TableRef {
	if s.metaClient == nil {
		return nil
	}

	tree, err := s.metaClient.WithTenantID(tenantID).GetMetadataTree(engine.ID)
	if err != nil {
		s.logger.Warn("获取元数据树失败", "engine_id", engine.ID, "error", err)
		return nil
	}

	var tables []TableRef
	for _, item := range tree.Items {
		if item.ItemType != "table" {
			continue
		}
		parts := splitN(item.FullName, ".", 2)
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

func splitN(s, sep string, n int) []string {
	result := make([]string, 0, n)
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, sep)
		if idx < 0 {
			break
		}
		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}
	result = append(result, s)
	return result
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
