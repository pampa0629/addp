package postgresql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PostgreSQLPlugin PostgreSQL 数据库插件
type PostgreSQLPlugin struct{}

// init 函数在包被导入时自动注册插件
func init() {
	plugin.Register(&PostgreSQLPlugin{})
}

// Type 返回数据库类型标识
func (p *PostgreSQLPlugin) Type() string {
	return "postgresql"
}

// DisplayName 返回显示名称
func (p *PostgreSQLPlugin) DisplayName() string {
	return "PostgreSQL"
}

// EngineOrigin 返回引擎分类
func (p *PostgreSQLPlugin) EngineOrigin() string {
	return "general"
}

// DefaultPort 返回默认端口
func (p *PostgreSQLPlugin) DefaultPort() int {
	return 5432
}

// RequiredFields 返回必填字段列表
func (p *PostgreSQLPlugin) RequiredFields() []string {
	return []string{"host", "user", "database"}
}

// SensitiveFields 返回敏感字段列表
func (p *PostgreSQLPlugin) SensitiveFields() []string {
	return []string{"password"}
}

func (p *PostgreSQLPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), "schema", plugin.TabularCapabilityOptions{
		Write:             true,
		BulkWrite:         true,
		TableReadSession:  true,
		BatchWrite:        true,
		TableWriteSession: true,
		TableWritePrepare: true,
		Delete:            true,
		SpatialFacts:      true,
		SupportsExplain:   true,
		SupportsCancel:    true,
		WriterConnector:   "postgres_copy",
	})
}

func (p *PostgreSQLPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.TabularCatalogModel("schema")
}

func (p *PostgreSQLPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *PostgreSQLPlugin) tabularCatalogCallbacks() plugin.TabularCatalogCallbacks {
	return plugin.TabularCatalogCallbacks{
		NamespaceTerm:         "schema",
		ListNamespaces:        p.listNamespaces,
		ListTables:            p.listTables,
		ListColumns:           p.listColumns,
		RowCount:              p.getTableRowCount,
		IsSystemNamespaceFunc: p.isSystemSchema,
	}
}

func (p *PostgreSQLPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	return plugin.ListTabularCatalogChildren(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, parent, opts)
}

func (p *PostgreSQLPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	return plugin.ResolveTabularCatalogPath(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path)
}

func (p *PostgreSQLPlugin) DescribeCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	return plugin.DescribeTabularCatalogFacts(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path, opts)
}

func (p *PostgreSQLPlugin) QueryLanguages() []string {
	return []string{"sql"}
}

func (p *PostgreSQLPlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return "SELECT *\nFROM your_schema.your_table\nLIMIT 10", "sql"
}

func (p *PostgreSQLPlugin) ExecuteRuntimeQuery(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	return p.ExecuteSQL(ctx, connInfo, req.Query, req.Options)
}

func (p *PostgreSQLPlugin) SQLDialect() string {
	return p.GetDialect()
}

func (p *PostgreSQLPlugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return plugin.ExecuteSQLWithConnectionPool(ctx, p, connInfo, sql, opts)
}

func (p *PostgreSQLPlugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return plugin.ReadSQLBatch(ctx, p, connInfo, path, opts)
}

// ValidateConnectionInfo 验证连接信息
func (p *PostgreSQLPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

// BuildDSN 构建连接字符串
func (p *PostgreSQLPlugin) BuildDSN(connInfo plugin.ConnectionInfo) (string, error) {
	return plugin.BuildPostgreSQLDSN(connInfo, p.DefaultPort())
}

// TestConnection 测试数据库连接
func (p *PostgreSQLPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}
	return plugin.TestSQLConnection(ctx, "postgres", connStr, "SELECT version()")
}

// === ConnectionPoolPlugin 接口实现 ===

// CreateConnectionPool 创建GORM连接池
func (p *PostgreSQLPlugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	return plugin.OpenGORMPool(postgres.Open(connStr), poolConfig)
}

// GetDialect 获取数据库方言
func (p *PostgreSQLPlugin) GetDialect() string {
	return "postgres"
}

// === CatalogProvider / CatalogFactsProvider 回调实现 ===

// listNamespaces 列出所有 Schema。
func (p *PostgreSQLPlugin) listNamespaces(ctx context.Context, db *gorm.DB, root plugin.CatalogPath) ([]plugin.CatalogEntry, error) {
	var rows []postgresNamespaceRow

	query := `
		SELECT
			schema_name as name,
			(SELECT COUNT(*)
			 FROM information_schema.tables
			 WHERE table_schema = s.schema_name
			   AND table_type = 'BASE TABLE') as leaf_count
		FROM information_schema.schemata s
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY schema_name
	`

	err := db.WithContext(ctx).Raw(query).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	namespaces := make([]plugin.CatalogEntry, 0, len(rows))
	for _, row := range rows {
		namespaces = append(namespaces, plugin.TabularNamespaceCatalogEntry(root, "schema", row.Name, row.LeafCount))
	}
	return namespaces, nil
}

type postgresNamespaceRow struct {
	Name      string
	LeafCount int
}

// ListTables 列出指定Schema下的所有表
func (p *PostgreSQLPlugin) listTables(ctx context.Context, db *gorm.DB, schema string) ([]datatype.TableInfo, error) {
	var rows []postgresTableRow

	query := `
		SELECT
			t.table_name as name,
			t.table_type,
			COALESCE(c.relkind::text, '') as relkind,
			CASE
				WHEN t.table_type = 'VIEW' THEN 'view'
				WHEN t.table_type = 'BASE TABLE' THEN 'table'
				ELSE lower(replace(t.table_type, ' ', '_'))
			END AS kind,
			COALESCE(pg_total_relation_size(quote_ident(t.table_schema)||'.'||quote_ident(t.table_name)), 0) as size_bytes,
			GREATEST(c.reltuples::bigint, 0) as row_count,
			GREATEST(
				s.last_autoanalyze,
				s.last_autovacuum,
				s.last_analyze,
				s.last_vacuum
			) as updated_at
		FROM information_schema.tables t
		LEFT JOIN pg_stat_user_tables s
			ON t.table_schema = s.schemaname AND t.table_name = s.relname
		LEFT JOIN pg_class c
			ON c.oid = to_regclass(quote_ident(t.table_schema)||'.'||quote_ident(t.table_name))
		WHERE t.table_schema = $1
		  AND t.table_type IN ('BASE TABLE', 'VIEW')
		ORDER BY t.table_name
	`

	err := db.WithContext(ctx).Raw(query, schema).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	tables := make([]datatype.TableInfo, 0, len(rows))
	for _, row := range rows {
		tables = append(tables, datatype.TableInfo{
			Name:      row.Name,
			Kind:      row.Kind,
			RowCount:  row.RowCount,
			SizeBytes: row.SizeBytes,
			UpdatedAt: row.UpdatedAt,
			Native:    postgresTableNative(row.TableType, row.Relkind),
		})
	}

	return tables, nil
}

type postgresTableRow struct {
	Name      string
	TableType string
	Relkind   string
	Kind      string
	RowCount  *int64
	SizeBytes *int64
	UpdatedAt *time.Time
}

var postgresTableNativeKeys = datatype.NewNativeAllowedKeys("table_type", "relkind")

func postgresTableNative(tableType, relkind string) map[string]interface{} {
	native := map[string]interface{}{}
	if tableType = strings.TrimSpace(tableType); tableType != "" {
		native["table_type"] = tableType
	}
	if relkind = strings.TrimSpace(relkind); relkind != "" {
		native["relkind"] = relkind
	}
	return datatype.FilterTableNative(native, postgresTableNativeKeys)
}

// ListColumns 列出指定表的所有列
func (p *PostgreSQLPlugin) listColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]datatype.FieldInfo, error) {
	var fields []datatype.FieldInfo

	query := `
		SELECT
			c.column_name as name,
			CASE
				WHEN c.data_type = 'USER-DEFINED' THEN c.udt_name
				ELSE c.data_type
			END as native_type,
			CASE WHEN c.is_nullable = 'YES' THEN true ELSE false END as nullable,
			CASE WHEN pk.column_name IS NOT NULL THEN true ELSE false END as primary_key,
			COALESCE(
				col_description(
					(quote_ident(c.table_schema)||'.'||quote_ident(c.table_name))::regclass,
					c.ordinal_position
				),
				''
			) as comment
		FROM information_schema.columns c
		LEFT JOIN (
			SELECT kcu.column_name
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
				ON tc.constraint_name = kcu.constraint_name
				AND tc.table_schema = kcu.table_schema
			WHERE tc.table_schema = $1
			  AND tc.table_name = $2
			  AND tc.constraint_type = 'PRIMARY KEY'
		) pk ON c.column_name = pk.column_name
		WHERE c.table_schema = $1
		  AND c.table_name = $2
		ORDER BY c.ordinal_position
	`

	err := db.WithContext(ctx).Raw(query, schema, table).Scan(&fields).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list columns: %w", err)
	}

	return plugin.NormalizeFieldInfos(fields), nil
}

// GetTableRowCount 获取表的行数
func (p *PostgreSQLPlugin) getTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	var count int64

	// 使用统计估算（快速）
	query := `
		SELECT GREATEST(reltuples::bigint, 0)
		FROM pg_class
		WHERE oid = to_regclass(quote_ident($1)||'.'||quote_ident($2))
	`

	err := db.WithContext(ctx).Raw(query, schema, table).Scan(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}

	return count, nil
}

// isSystemSchema 判断是否为系统 Schema
func (p *PostgreSQLPlugin) isSystemSchema(schemaName string) bool {
	normalized := strings.ToLower(schemaName)
	systemSchemas := map[string]bool{
		"pg_catalog":         true,
		"information_schema": true,
		"pg_toast":           true,
		"pg_temp_1":          true,
		"pg_toast_temp_1":    true,
	}

	// 检查是否在系统 schema 列表中
	if systemSchemas[normalized] {
		return true
	}

	// 检查是否以 pg_toast_ 或 pg_temp_ 开头
	return strings.HasPrefix(normalized, "pg_toast_") || strings.HasPrefix(normalized, "pg_temp_")
}
