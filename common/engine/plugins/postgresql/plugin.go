package postgresql

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	_ "github.com/lib/pq"
	pgquery "github.com/pganalyze/pg_query_go/v6"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PostgreSQLPlugin implements the PostgreSQL wire-protocol and SQL behavior.
// The optional identity is used by independently registered engines that have
// certified this protocol implementation while retaining their own engine type.
type PostgreSQLPlugin struct {
	identity *ProtocolIdentity
}

// ProtocolIdentity identifies an engine that reuses PostgreSQL protocol
// behavior. It does not copy PostgreSQL capabilities into that engine.
type ProtocolIdentity struct {
	EngineType  string
	DisplayName string
	// AdditionalSystemSchemas extends PostgreSQL's built-in catalog filter for
	// a protocol-compatible engine's own reserved schemas.
	AdditionalSystemSchemas []string
	// AdditionalSystemTables extends the catalog filter for protocol-compatible
	// engines that expose reserved objects inside an otherwise business schema.
	AdditionalSystemTables []string
}

// NewProtocolCompatiblePlugin creates a PostgreSQL protocol implementation
// whose prepared queries and catalog paths retain the owning engine identity.
func NewProtocolCompatiblePlugin(identity ProtocolIdentity) *PostgreSQLPlugin {
	return &PostgreSQLPlugin{identity: &identity}
}

var (
	_ plugin.BoundedWatermarkReadProvider        = (*PostgreSQLPlugin)(nil)
	_ plugin.SpatialFeatureReadProvider          = (*PostgreSQLPlugin)(nil)
	_ plugin.TableUpsertProvider                 = (*PostgreSQLPlugin)(nil)
	_ plugin.PartitionedTableChangeApplyProvider = (*PostgreSQLPlugin)(nil)
)

var superMapSDXSystemTableNames = []string{
	"smadditionalinfo",
	"smbandregister",
	"smcodedomains",
	"smdatasetlock",
	"smdatasourceinfo",
	"smdomainfield",
	"smdomains",
	"smdynamicindex",
	"smentityrelation",
	"smfieldinfo",
	"smgroupitems",
	"smhistoricalmoments",
	"smimgregister",
	"smpyramidcolumns",
	"smrangedomains",
	"smregister",
	"smreplicas",
	"smsequencemanage",
	"smtileindex",
	"smtoporelation",
	"smtoporules",
	"smuserinfo",
	"smversionconflicts",
	"smversiondtitems",
	"smversions",
}

// init 函数在包被导入时自动注册插件
func init() {
	plugin.Register(&PostgreSQLPlugin{})
}

// Type 返回数据库类型标识
func (p *PostgreSQLPlugin) Type() string {
	if p != nil && p.identity != nil && strings.TrimSpace(p.identity.EngineType) != "" {
		return strings.ToLower(strings.TrimSpace(p.identity.EngineType))
	}
	return "postgresql"
}

// DisplayName 返回显示名称
func (p *PostgreSQLPlugin) DisplayName() string {
	if p != nil && p.identity != nil && strings.TrimSpace(p.identity.DisplayName) != "" {
		return strings.TrimSpace(p.identity.DisplayName)
	}
	return "PostgreSQL"
}

// EngineOrigin 返回引擎分类
func (p *PostgreSQLPlugin) EngineOrigin() string {
	return "general"
}

func (p *PostgreSQLPlugin) ConnectionSpec() plugin.ConnectionSpec {
	return plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "host", LabelKey: "storageEngine.host", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "localhost", Placeholder: "localhost"},
		plugin.ConnectionFieldSpec{Key: "port", LabelKey: "storageEngine.port", Input: plugin.ConnectionFieldNumber, Identity: true, Default: 5432, Min: plugin.Int(1), Max: plugin.Int(65535)},
		plugin.ConnectionFieldSpec{Key: "database", LabelKey: "storageEngine.database", Input: plugin.ConnectionFieldText, Required: true, Identity: true, PlaceholderKey: "storageEngine.databasePlaceholder"},
		plugin.ConnectionFieldSpec{Key: "user", LabelKey: "storageEngine.username", Input: plugin.ConnectionFieldText, Required: true, PlaceholderKey: "storageEngine.usernamePlaceholder"},
		plugin.ConnectionFieldSpec{Key: "password", LabelKey: "storageEngine.passwordOptional", Input: plugin.ConnectionFieldPassword, Sensitive: true, PlaceholderKey: "storageEngine.passwordPlaceholder"},
		plugin.ConnectionFieldSpec{Key: "sslmode", LabelKey: "storageEngine.sslMode", Input: plugin.ConnectionFieldSelect, Default: "disable", Options: []plugin.ConnectionFieldOption{
			{Value: "disable", LabelKey: "storageEngine.sslDisable"}, {Value: "require", LabelKey: "storageEngine.sslRequire"},
			{Value: "verify-ca", LabelKey: "storageEngine.sslVerifyCa"}, {Value: "verify-full", LabelKey: "storageEngine.sslVerifyFull"},
		}},
	)
}

// DefaultPort 返回默认端口
func (p *PostgreSQLPlugin) DefaultPort() int {
	return p.ConnectionSpec().DefaultPortValue()
}

// RequiredFields 返回必填字段列表
func (p *PostgreSQLPlugin) RequiredFields() []string {
	return p.ConnectionSpec().RequiredFields()
}

// SensitiveFields 返回敏感字段列表
func (p *PostgreSQLPlugin) SensitiveFields() []string {
	return p.ConnectionSpec().SensitiveFields()
}

func (p *PostgreSQLPlugin) ConnectionIdentityFields() []string {
	return p.ConnectionSpec().IdentityFields()
}

func (p *PostgreSQLPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), "schema", plugin.TabularCapabilityOptions{
		Constraints:               true,
		Write:                     true,
		BulkWrite:                 true,
		TableReadSession:          true,
		QueryReadSession:          true,
		TableReadSpatialTransform: true,
		TableSpatialEncoding: &plugin.NativeTableSpatialEncodingCapability{
			GeometryReadEncodings:  []string{"ewkb", "geojson"},
			GeometryWriteEncodings: []string{"ewkb"},
			ReadTransform:          true,
			NativeSpatialFunctions: true,
		},
		BatchWrite:           true,
		TableWriteSession:    true,
		TableWritePrepare:    true,
		BoundedWatermarkRead: true,
		TableUpsert:          true,
		PartitionedTableChangeApplyOperations: []string{
			plugin.TableChangeOperationUpsert,
			plugin.TableChangeOperationDelete,
			plugin.TableChangeOperationSkip,
		},
		Delete:             true,
		SpatialFacts:       true,
		SupportsExplain:    true,
		SupportsCancel:     true,
		SupportsParameters: true,
		AdditionalParameterTypes: []string{
			"relation",
		},
		IdentifierQuote: `"`,
		WriterConnector: "postgres_copy",
	})
}

func (p *PostgreSQLPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
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
		DescribeSpatial:       p.describeSpatialFacts,
		IsSystemNamespaceFunc: p.isSystemSchema,
	}
}

func (p *PostgreSQLPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	if err := p.rejectHiddenCatalogPath(parent); err != nil {
		return nil, err
	}
	return plugin.ListTabularCatalogChildren(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, parent, opts)
}

func (p *PostgreSQLPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	if err := p.rejectHiddenCatalogPath(path); err != nil {
		return nil, err
	}
	return plugin.ResolveTabularCatalogPath(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path)
}

func (p *PostgreSQLPlugin) DescribeEngineCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	if err := p.rejectHiddenCatalogPath(path); err != nil {
		return nil, err
	}
	return plugin.DescribeTabularCatalogFacts(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path, opts)
}

func (p *PostgreSQLPlugin) QueryLanguages() []string {
	return []string{"sql"}
}

func (p *PostgreSQLPlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return plugin.SampleSQLForDialectCatalogPath(p.SQLDialect(), opts.Path, 10), "sql"
}

func (p *PostgreSQLPlugin) PrepareQuery(_ context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	boundQuery, _, err := plugin.BindSQLRuntimeParameters(p.SQLDialect(), req.Query, req.Options)
	if err != nil {
		return nil, err
	}
	if _, err := pgquery.Parse(strings.TrimSpace(boundQuery)); err != nil {
		return nil, fmt.Errorf("PostgreSQL 查询语法无效: %w", err)
	}
	return plugin.PrepareSQLRuntimeQuery(p, connInfo, req, p.resolvePreparedQueryReadSet, p.resolvePreparedQueryOutputLineage)
}

func (p *PostgreSQLPlugin) SQLDialect() string {
	return commonquery.DialectPostgreSQL
}

func (p *PostgreSQLPlugin) SupportsParameterizedQueries() bool {
	return true
}

func (p *PostgreSQLPlugin) ControlledReadOnlySQLBoundary() plugin.ControlledReadOnlySQLBoundary {
	return plugin.ControlledReadOnlySQLBoundaryDatabaseTransaction
}

func (p *PostgreSQLPlugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return plugin.ExecuteSQLWithConnectionPool(ctx, p, connInfo, sql, opts)
}

func (p *PostgreSQLPlugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return p.readBatch(ctx, connInfo, path, opts)
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
func (p *PostgreSQLPlugin) GORMDialect() string {
	return "postgres"
}

// === EngineCatalogProvider / EngineCatalogFactsProvider 回调实现 ===

// listNamespaces 列出所有 Schema。
func (p *PostgreSQLPlugin) listNamespaces(ctx context.Context, db *gorm.DB, root plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
	var rows []postgresNamespaceRow

	superMapSDXDetected, err := p.hasSuperMapSDXSystemTables(ctx, db)
	if err != nil {
		return nil, err
	}

	query, args := p.listNamespacesQuery(superMapSDXDetected)

	err = db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	namespaces := make([]plugin.EngineCatalogEntry, 0, len(rows))
	for _, row := range rows {
		namespaces = append(namespaces, plugin.TabularNamespaceCatalogEntry(root, "schema", row.Name, row.LeafCount))
	}
	return namespaces, nil
}

func (p *PostgreSQLPlugin) listNamespacesQuery(superMapSDXDetected bool) (string, []interface{}) {
	systemTables := p.catalogSystemTableNames(superMapSDXDetected)
	tablePlaceholders := make([]string, len(systemTables))
	args := make([]interface{}, 0, len(systemTables)+len(p.systemSchemaNames()))
	for index, table := range systemTables {
		tablePlaceholders[index] = "?"
		args = append(args, table)
	}
	tableFilter := ""
	if len(tablePlaceholders) > 0 {
		tableFilter = "AND lower(table_name) NOT IN (" + strings.Join(tablePlaceholders, ", ") + ")"
	}

	systemSchemas := p.systemSchemaNames()
	placeholders := make([]string, len(systemSchemas))
	for index, schema := range systemSchemas {
		placeholders[index] = "?"
		args = append(args, schema)
	}

	query := `
		SELECT
			schema_name as name,
			(SELECT COUNT(*)
			 FROM information_schema.tables
			 WHERE table_schema = s.schema_name
			   AND table_type = 'BASE TABLE'
			   ` + tableFilter + `) as leaf_count
		FROM information_schema.schemata s
		WHERE lower(schema_name) NOT IN (` + strings.Join(placeholders, ", ") + `)
		  AND has_schema_privilege(s.schema_name, 'USAGE')
		ORDER BY schema_name
	`
	return query, args
}

type postgresNamespaceRow struct {
	Name      string
	LeafCount int
}

const postgresListTablesQuery = `
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
			CASE WHEN c.reltuples >= 0 THEN c.reltuples::bigint END as row_count,
			GREATEST(
				s.last_autoanalyze,
				s.last_autovacuum,
				s.last_analyze,
				s.last_vacuum
			) as updated_at
		FROM information_schema.tables t
		LEFT JOIN pg_catalog.pg_stat_user_tables s
			ON t.table_schema = s.schemaname AND t.table_name = s.relname
		LEFT JOIN pg_catalog.pg_namespace n
			ON n.nspname = t.table_schema
		LEFT JOIN pg_catalog.pg_class c
			ON c.relnamespace = n.oid AND c.relname = t.table_name
		WHERE t.table_schema = $1
		  AND t.table_type IN ('BASE TABLE', 'VIEW')
		ORDER BY t.table_name
	`

// ListTables 列出指定Schema下的所有表
func (p *PostgreSQLPlugin) listTables(ctx context.Context, db *gorm.DB, schema string) ([]datatype.TableInfo, error) {
	var rows []postgresTableRow

	superMapSDXDetected, err := p.hasSuperMapSDXSystemTables(ctx, db)
	if err != nil {
		return nil, err
	}

	err = db.WithContext(ctx).Raw(postgresListTablesQuery, schema).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	tables := make([]datatype.TableInfo, 0, len(rows))
	for _, row := range rows {
		tables = append(tables, datatype.TableInfo{
			Name:              row.Name,
			Kind:              row.Kind,
			EstimatedRowCount: row.RowCount,
			SizeBytes:         row.SizeBytes,
			UpdatedAt:         row.UpdatedAt,
			Native:            postgresTableNative(row.TableType, row.Relkind),
		})
	}

	return p.filterSystemTables(tables, superMapSDXDetected), nil
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
	var columns []postgresColumnInfo

	query := `
		SELECT
			c.column_name as name,
			c.data_type as data_type,
			c.udt_name as udt_name,
			format_type(a.atttypid, a.atttypmod) as native_type,
			c.numeric_precision as numeric_precision,
			c.numeric_scale as numeric_scale,
			CASE WHEN c.is_nullable = 'YES' THEN true ELSE false END as nullable,
			CASE WHEN pk.column_name IS NOT NULL THEN true ELSE false END as primary_key,
			COALESCE(col_description(cls.oid, a.attnum), '') as comment
		FROM information_schema.columns c
		JOIN pg_catalog.pg_namespace n
			ON n.nspname = c.table_schema
		JOIN pg_catalog.pg_class cls
			ON cls.relnamespace = n.oid
			AND cls.relname = c.table_name
		JOIN pg_catalog.pg_attribute a
			ON a.attrelid = cls.oid
			AND a.attname = c.column_name
			AND a.attnum > 0
			AND NOT a.attisdropped
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

	err := db.WithContext(ctx).Raw(query, schema, table).Scan(&columns).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list columns: %w", err)
	}

	fields := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		fields = append(fields, postgresFieldInfoFromColumn(column))
	}
	return plugin.NormalizeFieldInfos(fields), nil
}

// GetTableRowCount 获取表的行数
func (p *PostgreSQLPlugin) getTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	var count int64
	query := commonquery.ForDialect(p.SQLDialect()).CountTableSQL(schema, table, "")
	err := db.WithContext(ctx).Raw(query).Scan(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}

	return count, nil
}

// isSystemSchema 判断是否为系统 Schema
func (p *PostgreSQLPlugin) isSystemSchema(schemaName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(schemaName))
	for _, systemSchema := range p.systemSchemaNames() {
		if normalized == systemSchema {
			return true
		}
	}

	return strings.HasPrefix(normalized, "pg_toast_") || strings.HasPrefix(normalized, "pg_temp_")
}

func (p *PostgreSQLPlugin) systemSchemaNames() []string {
	names := map[string]struct{}{
		"information_schema": {},
		"pg_catalog":         {},
		"pg_toast":           {},
	}
	if p != nil && p.identity != nil {
		for _, schema := range p.identity.AdditionalSystemSchemas {
			if normalized := strings.ToLower(strings.TrimSpace(schema)); normalized != "" {
				names[normalized] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func (p *PostgreSQLPlugin) additionalSystemTableNames() []string {
	names := map[string]struct{}{}
	if p != nil && p.identity != nil {
		for _, table := range p.identity.AdditionalSystemTables {
			if normalized := strings.ToLower(strings.TrimSpace(table)); normalized != "" {
				names[normalized] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func (p *PostgreSQLPlugin) catalogSystemTableNames(superMapSDXDetected bool) []string {
	names := map[string]struct{}{}
	for _, name := range p.additionalSystemTableNames() {
		names[name] = struct{}{}
	}
	if superMapSDXDetected {
		for _, name := range superMapSDXSystemTableNames {
			names[name] = struct{}{}
		}
	}

	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func (p *PostgreSQLPlugin) isAdditionalSystemTableName(tableName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(tableName))
	for _, name := range p.additionalSystemTableNames() {
		if normalized == name {
			return true
		}
	}
	return false
}

func (p *PostgreSQLPlugin) rejectHiddenCatalogPath(path plugin.EngineCatalogPath) error {
	segments := plugin.EngineCatalogPathWithoutRoot(path).Segments
	if len(segments) == 0 {
		return nil
	}
	if p.isSystemSchema(segments[0].Name) {
		return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorNotFound, fmt.Errorf("catalog namespace %q is hidden", segments[0].Name))
	}
	if len(segments) > 1 && p.isAdditionalSystemTableName(segments[len(segments)-1].Name) {
		return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorNotFound, fmt.Errorf("catalog table %q is hidden", segments[len(segments)-1].Name))
	}
	return nil
}

func (p *PostgreSQLPlugin) hasSuperMapSDXSystemTables(ctx context.Context, db *gorm.DB) (bool, error) {
	var count int64
	err := db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND lower(table_name) IN (` + superMapSDXSystemTableSQLList() + `)
	`).Scan(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to detect SuperMap spatial workspace system tables: %w", err)
	}
	return count >= superMapSDXSystemTableThreshold, nil
}

func (p *PostgreSQLPlugin) filterSystemTables(tables []datatype.TableInfo, superMapSDXDetected bool) []datatype.TableInfo {
	systemTableNames := p.catalogSystemTableNames(superMapSDXDetected)
	if len(systemTableNames) == 0 {
		return tables
	}
	systemTableNameSet := make(map[string]struct{}, len(systemTableNames))
	for _, name := range systemTableNames {
		systemTableNameSet[name] = struct{}{}
	}

	filtered := make([]datatype.TableInfo, 0, len(tables))
	for _, table := range tables {
		if _, hidden := systemTableNameSet[strings.ToLower(strings.TrimSpace(table.Name))]; hidden {
			continue
		}
		filtered = append(filtered, table)
	}
	return filtered
}

func superMapSDXSystemTableSQLList() string {
	quoted := make([]string, 0, len(superMapSDXSystemTableNames))
	for _, name := range superMapSDXSystemTableNames {
		quoted = append(quoted, "'"+name+"'")
	}
	return strings.Join(quoted, ",")
}
