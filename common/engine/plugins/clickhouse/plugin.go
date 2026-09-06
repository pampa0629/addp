package clickhouse

import (
	"context"
	"fmt"
	"strings"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"gorm.io/driver/clickhouse"
	"gorm.io/gorm"
)

// ClickHousePlugin ClickHouse 数据库插件
type ClickHousePlugin struct{}

// init 函数在包被导入时自动注册插件
func init() {
	plugin.Register(&ClickHousePlugin{})
}

// Type 返回数据库类型标识
func (p *ClickHousePlugin) Type() string {
	return "clickhouse"
}

// DisplayName 返回显示名称
func (p *ClickHousePlugin) DisplayName() string {
	return "ClickHouse"
}

// EngineOrigin 返回引擎分类
func (p *ClickHousePlugin) EngineOrigin() string {
	return "general"
}

func (p *ClickHousePlugin) ConnectionSpec() plugin.ConnectionSpec {
	return plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "host", LabelKey: "storageEngine.host", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "localhost", Placeholder: "localhost"},
		plugin.ConnectionFieldSpec{Key: "port", LabelKey: "storageEngine.port", Input: plugin.ConnectionFieldNumber, Identity: true, Default: 9000, Min: plugin.Int(1), Max: plugin.Int(65535)},
		plugin.ConnectionFieldSpec{Key: "database", LabelKey: "storageEngine.database", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "default", Placeholder: "default"},
		plugin.ConnectionFieldSpec{Key: "user", LabelKey: "storageEngine.username", Input: plugin.ConnectionFieldText, Required: true, Default: "default", Placeholder: "default"},
		plugin.ConnectionFieldSpec{Key: "password", LabelKey: "storageEngine.passwordOptional", Input: plugin.ConnectionFieldPassword, Sensitive: true, HintKey: "storageEngine.hints.clickhousePassword"},
	)
}

// DefaultPort 返回默认端口
func (p *ClickHousePlugin) DefaultPort() int {
	return p.ConnectionSpec().DefaultPortValue()
}

// RequiredFields 返回必填字段列表
func (p *ClickHousePlugin) RequiredFields() []string {
	return p.ConnectionSpec().RequiredFields()
}

// SensitiveFields 返回敏感字段列表
func (p *ClickHousePlugin) SensitiveFields() []string {
	return p.ConnectionSpec().SensitiveFields()
}

func (p *ClickHousePlugin) ConnectionIdentityFields() []string {
	return p.ConnectionSpec().IdentityFields()
}

func (p *ClickHousePlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), "database", plugin.TabularCapabilityOptions{
		Write:              true,
		BulkWrite:          true,
		BatchWrite:         true,
		TableReadSession:   true,
		TableWriteSession:  true,
		TableWritePrepare:  true,
		Delete:             true,
		SupportsExplain:    true,
		SupportsParameters: true,
		IdentifierQuote:    "`",
		WriterConnector:    "clickhouse_insert",
	})
}

func (p *ClickHousePlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.TabularCatalogModel("database")
}

func (p *ClickHousePlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *ClickHousePlugin) tabularCatalogCallbacks() plugin.TabularCatalogCallbacks {
	return plugin.TabularCatalogCallbacks{
		NamespaceTerm:         "database",
		ListNamespaces:        p.listNamespaces,
		ListTables:            p.listTables,
		ListColumns:           p.listColumns,
		RowCount:              p.getTableRowCount,
		IsSystemNamespaceFunc: p.isSystemSchema,
	}
}

func (p *ClickHousePlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return plugin.ListTabularCatalogChildren(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, parent, opts)
}

func (p *ClickHousePlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return plugin.ResolveTabularCatalogPath(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path)
}

func (p *ClickHousePlugin) DescribeEngineCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return plugin.DescribeTabularCatalogFacts(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path, opts)
}

func (p *ClickHousePlugin) QueryLanguages() []string {
	return []string{"sql"}
}

func (p *ClickHousePlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return plugin.SampleSQLForDialectCatalogPath(p.SQLDialect(), opts.Path, 10), "sql"
}

func (p *ClickHousePlugin) PrepareQuery(_ context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	return plugin.PrepareSQLRuntimeQuery(p, connInfo, req, nil, nil)
}

func (p *ClickHousePlugin) SQLDialect() string {
	return commonquery.DialectClickHouse
}

func (p *ClickHousePlugin) SupportsParameterizedQueries() bool {
	return true
}

func (p *ClickHousePlugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return plugin.ExecuteSQLWithConnectionPool(ctx, p, connInfo, sql, opts)
}

func (p *ClickHousePlugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return plugin.ReadSQLBatch(ctx, p, connInfo, path, opts)
}

// ValidateConnectionInfo 验证连接信息
func (p *ClickHousePlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

// BuildDSN 构建连接字符串
func (p *ClickHousePlugin) BuildDSN(connInfo plugin.ConnectionInfo) (string, error) {
	return plugin.BuildClickHouseDSN(connInfo, p.DefaultPort(), map[string]string{
		"dial_timeout":       "10s",
		"max_execution_time": "60",
	})
}

// TestConnection 测试数据库连接
func (p *ClickHousePlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}
	return plugin.TestSQLConnection(ctx, "clickhouse", connStr, "SELECT version()")
}

// === ConnectionPoolPlugin 接口实现 ===

// CreateConnectionPool 创建GORM连接池
func (p *ClickHousePlugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	return plugin.OpenGORMPool(clickhouse.Open(connStr), poolConfig)
}

// GetDialect 获取数据库方言
func (p *ClickHousePlugin) GORMDialect() string {
	return "clickhouse"
}

// === EngineCatalogProvider / EngineCatalogFactsProvider 回调实现 ===

// listNamespaces 列出所有 Database。
func (p *ClickHousePlugin) listNamespaces(ctx context.Context, db *gorm.DB, root plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
	var rows []clickhouseNamespaceRow

	// ClickHouse 使用 system.databases 获取 database 列表和表数量统计。
	query := `
		SELECT
			name,
			(SELECT COUNT(*)
			 FROM system.tables
			 WHERE database = d.name) as leaf_count
		FROM system.databases d
		WHERE name NOT IN ('system', 'information_schema', 'INFORMATION_SCHEMA')
		ORDER BY name
	`

	err := db.WithContext(ctx).Raw(query).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	namespaces := make([]plugin.EngineCatalogEntry, 0, len(rows))
	for _, row := range rows {
		namespaces = append(namespaces, plugin.TabularNamespaceCatalogEntry(root, "database", row.Name, row.LeafCount))
	}
	return namespaces, nil
}

type clickhouseNamespaceRow struct {
	Name      string
	LeafCount int
}

// ListTables 列出指定Database下的所有表
func (p *ClickHousePlugin) listTables(ctx context.Context, db *gorm.DB, schema string) ([]datatype.TableInfo, error) {
	var rows []clickhouseTableRow

	query := `
		SELECT
			name,
			engine,
			CASE
				WHEN engine = 'MaterializedView' THEN 'materialized_view'
				WHEN engine = 'View' THEN 'view'
				WHEN engine LIKE '%View%' THEN 'view'
				ELSE 'table'
			END AS kind,
			comment,
			total_rows as row_count,
			total_bytes as size_bytes
		FROM system.tables
		WHERE database = ?
		ORDER BY name
	`

	err := db.WithContext(ctx).Raw(query, schema).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	tables := make([]datatype.TableInfo, 0, len(rows))
	for _, row := range rows {
		tables = append(tables, datatype.TableInfo{
			Name:              row.Name,
			Kind:              row.Kind,
			Comment:           row.Comment,
			EstimatedRowCount: row.RowCount,
			SizeBytes:         row.SizeBytes,
			Native:            clickhouseTableNative(nil, row.Engine),
		})
	}

	return tables, nil
}

type clickhouseTableRow struct {
	Name      string
	Engine    string
	Kind      string
	Comment   string
	RowCount  *int64
	SizeBytes *int64
}

var clickhouseTableNativeKeys = datatype.NewNativeAllowedKeys("engine")

func clickhouseTableNative(native map[string]interface{}, engine string) map[string]interface{} {
	engine = strings.TrimSpace(engine)
	if engine == "" {
		return native
	}
	if native == nil {
		native = map[string]interface{}{}
	}
	native["engine"] = engine
	return datatype.FilterTableNative(native, clickhouseTableNativeKeys)
}

// ListColumns 列出指定表的所有列
func (p *ClickHousePlugin) listColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]datatype.FieldInfo, error) {
	var rows []clickhouseColumnRow

	query := `
		SELECT
			name,
			type as native_type,
			position(type, 'Nullable(') > 0 as nullable,
			comment,
			default_kind,
			default_expression
		FROM system.columns
		WHERE database = ?
		  AND table = ?
		ORDER BY position
	`

	err := db.WithContext(ctx).Raw(query, schema, table).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list columns: %w", err)
	}

	fields := make([]datatype.FieldInfo, 0, len(rows))
	for _, row := range rows {
		fields = append(fields, clickhouseFieldInfo(row))
	}
	return plugin.NormalizeFieldInfos(fields), nil
}

type clickhouseColumnRow struct {
	Name              string
	NativeType        string
	Nullable          bool
	Comment           string
	DefaultKind       string
	DefaultExpression string
}

func clickhouseFieldInfo(row clickhouseColumnRow) datatype.FieldInfo {
	field := datatype.FieldInfo{
		Name:       row.Name,
		Type:       clickhouseCommonFieldType(row.NativeType),
		NativeType: row.NativeType,
		Nullable:   row.Nullable,
		Comment:    row.Comment,
	}
	expression := strings.TrimSpace(row.DefaultExpression)
	if expression == "" {
		return field
	}
	switch strings.ToUpper(strings.TrimSpace(row.DefaultKind)) {
	case "MATERIALIZED", "ALIAS":
		field.Generated = true
		field.GenerationExpression = expression
	default:
		field.DefaultExpression = expression
	}
	return field
}

// GetTableRowCount 获取表的行数
func (p *ClickHousePlugin) getTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	var count int64
	query := commonquery.ForDialect(p.SQLDialect()).CountTableSQL(schema, table, "")
	err := db.WithContext(ctx).Raw(query).Scan(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}

	return count, nil
}

// isSystemSchema 判断是否为系统 Database
func (p *ClickHousePlugin) isSystemSchema(schemaName string) bool {
	systemDatabases := map[string]bool{
		"system":             true,
		"information_schema": true,
	}
	return systemDatabases[strings.ToLower(schemaName)]
}
