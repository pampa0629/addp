package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	oraclemapping "github.com/addp/common/format/mappers/oracle"
	commonquery "github.com/addp/common/query"
	gormoracle "github.com/godoes/gorm-oracle"
	_ "github.com/sijms/go-ora/v2"
	"gorm.io/gorm"
)

type OraclePlugin struct{}

var (
	_ plugin.ConnectionIdentityProvider           = (*OraclePlugin)(nil)
	_ plugin.DSNProvider                          = (*OraclePlugin)(nil)
	_ plugin.ConnectionPoolPlugin                 = (*OraclePlugin)(nil)
	_ plugin.CatalogModelProvider                 = (*OraclePlugin)(nil)
	_ plugin.CatalogProvider                      = (*OraclePlugin)(nil)
	_ plugin.CatalogFactsProvider                 = (*OraclePlugin)(nil)
	_ plugin.SQLQueryRuntimeProvider              = (*OraclePlugin)(nil)
	_ plugin.ParameterizedSQLQueryRuntimeProvider = (*OraclePlugin)(nil)
	_ plugin.BatchReadableProvider                = (*OraclePlugin)(nil)
	_ plugin.TableReadSessionProvider             = (*OraclePlugin)(nil)
)

var oracleSystemSchemas = map[string]struct{}{
	"ANONYMOUS": {}, "APPQOSSYS": {}, "AUDSYS": {}, "CTXSYS": {}, "DBSNMP": {},
	"DIP": {}, "DVF": {}, "DVSYS": {}, "GGSYS": {}, "GSMADMIN_INTERNAL": {},
	"GSMCATUSER": {}, "GSMUSER": {}, "LBACSYS": {}, "MDDATA": {}, "MDSYS": {},
	"OJVMSYS": {}, "OLAPSYS": {}, "ORACLE_OCM": {}, "ORDDATA": {}, "ORDPLUGINS": {},
	"ORDSYS": {}, "OUTLN": {}, "REMOTE_SCHEDULER_AGENT": {}, "SDE": {}, "SI_INFORMTN_SCHEMA": {},
	"SYS": {}, "SYSBACKUP": {}, "SYSDG": {}, "SYSKM": {}, "SYSRAC": {}, "SYSTEM": {},
	"WMSYS": {}, "XDB": {}, "XS$NULL": {},
}

var oracleTableNativeKeys = datatype.NewNativeAllowedKeys("object_type")

func init() {
	plugin.Register(&OraclePlugin{})
}

func (p *OraclePlugin) Type() string {
	return "oracle"
}

func (p *OraclePlugin) DisplayName() string {
	return "Oracle Database"
}

func (p *OraclePlugin) EngineOrigin() string {
	return "general"
}

func (p *OraclePlugin) DefaultPort() int {
	return 1521
}

func (p *OraclePlugin) RequiredFields() []string {
	return []string{"host", "service_name", "user", "password"}
}

func (p *OraclePlugin) SensitiveFields() []string {
	return []string{"password"}
}

func (p *OraclePlugin) ConnectionIdentityFields() []string {
	return []string{"host", "port", "service_name", "user"}
}

func (p *OraclePlugin) Capabilities() plugin.EngineCapabilities {
	caps := plugin.NewTabularCapabilities(p.Type(), plugin.CatalogTermSchema, plugin.TabularCapabilityOptions{
		TableReadSession:   true,
		SupportsParameters: true,
		Indexes:            true,
		Constraints:        true,
		Partitioning:       true,
	})
	return caps
}

func (p *OraclePlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.TabularCatalogModel(plugin.CatalogTermSchema)
}

func (p *OraclePlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *OraclePlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *OraclePlugin) BuildDSN(connInfo plugin.ConnectionInfo) (string, error) {
	if err := p.ValidateConnectionInfo(connInfo); err != nil {
		return "", err
	}
	host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
	port := plugin.GetInt(connInfo, "port")
	if port == 0 {
		port = p.DefaultPort()
	}
	return gormoracle.BuildUrl(
		host,
		port,
		plugin.GetString(connInfo, "service_name"),
		plugin.GetString(connInfo, "user"),
		plugin.GetString(connInfo, "password"),
		map[string]string{"CONNECTION TIMEOUT": "10"},
	), nil
}

func (p *OraclePlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build Oracle connection string: %w", err)
	}
	return plugin.TestSQLConnection(ctx, "oracle", dsn, "SELECT SYS_CONTEXT('USERENV', 'SERVICE_NAME') FROM DUAL")
}

func (p *OraclePlugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build Oracle connection string: %w", err)
	}
	return plugin.OpenGORMPool(gormoracle.Open(dsn), poolConfig)
}

func (p *OraclePlugin) GetDialect() string {
	return p.Type()
}

func (p *OraclePlugin) QueryLanguages() []string {
	return []string{"sql"}
}

func (p *OraclePlugin) GenerateSampleQuery(_ context.Context, _ plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return plugin.SampleSQLForCatalogPath(p.Type(), opts.Path, 10), "sql"
}

func (p *OraclePlugin) ExecuteRuntimeQuery(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	return p.ExecuteSQL(ctx, connInfo, req.Query, req.Options)
}

func (p *OraclePlugin) SQLDialect() string {
	return p.GetDialect()
}

func (p *OraclePlugin) SupportsParameterizedQueries() bool {
	return true
}

func (p *OraclePlugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, query string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return plugin.ExecuteSQLWithConnectionPool(ctx, p, connInfo, query, opts)
}

func (p *OraclePlugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return p.readBatch(ctx, connInfo, path, opts)
}

func (p *OraclePlugin) tabularCatalogCallbacks() plugin.TabularCatalogCallbacks {
	return plugin.TabularCatalogCallbacks{
		NamespaceTerm:         plugin.CatalogTermSchema,
		ListNamespaces:        p.listNamespaces,
		ListTables:            p.listTables,
		ListColumns:           p.listColumns,
		ListIndexes:           p.listIndexes,
		ListConstraints:       p.listConstraints,
		DescribePartitioning:  p.describePartitioning,
		RowCount:              p.getTableRowCount,
		IsSystemNamespaceFunc: p.isSystemSchema,
	}
}

func (p *OraclePlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	engine := &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}
	return plugin.ListTabularCatalogChildren(ctx, p.tabularCatalogCallbacks(), engine, parent, opts)
}

func (p *OraclePlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	engine := &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}
	return plugin.ResolveTabularCatalogPath(ctx, p.tabularCatalogCallbacks(), engine, path)
}

func (p *OraclePlugin) DescribeCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	engine := &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}
	return plugin.DescribeTabularCatalogFacts(ctx, p.tabularCatalogCallbacks(), engine, path, opts)
}

type oracleNamespaceRow struct {
	Name      string
	LeafCount int
}

func (p *OraclePlugin) listNamespaces(ctx context.Context, db *gorm.DB, root plugin.CatalogPath) ([]plugin.CatalogEntry, error) {
	var rows []oracleNamespaceRow
	err := db.WithContext(ctx).Raw(`
		SELECT u.username AS name,
		       COUNT(o.object_name) AS leaf_count
		  FROM all_users u
		  LEFT JOIN all_objects o
		    ON o.owner = u.username
		   AND o.object_type IN ('TABLE', 'VIEW', 'MATERIALIZED VIEW')
		   AND (o.object_type <> 'TABLE' OR NOT EXISTS (
		         SELECT 1 FROM all_mviews mv
		          WHERE mv.owner = o.owner AND mv.mview_name = o.object_name
		       ))
		 WHERE u.oracle_maintained = 'N'
		 GROUP BY u.username
		HAVING u.username = USER OR COUNT(o.object_name) > 0
		 ORDER BY u.username
	`).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list Oracle schemas: %w", err)
	}
	result := make([]plugin.CatalogEntry, 0, len(rows))
	for _, row := range rows {
		if p.isSystemSchema(row.Name) {
			continue
		}
		result = append(result, plugin.TabularNamespaceCatalogEntry(root, plugin.CatalogTermSchema, row.Name, row.LeafCount))
	}
	return result, nil
}

type oracleTableRow struct {
	Name       string
	ObjectType string
	Kind       string
	Comment    string
	NumRows    *int64
	CreatedAt  *time.Time
	UpdatedAt  *time.Time
}

func (p *OraclePlugin) listTables(ctx context.Context, db *gorm.DB, schema string) ([]datatype.TableInfo, error) {
	if p.isSystemSchema(schema) {
		return nil, plugin.WrapCatalogError(plugin.CatalogErrorUnsupported, fmt.Errorf("Oracle system schema %q is not exposed", schema))
	}
	var rows []oracleTableRow
	err := db.WithContext(ctx).Raw(`
		SELECT o.object_name AS name,
		       o.object_type,
		       CASE o.object_type
		         WHEN 'TABLE' THEN 'table'
		         WHEN 'VIEW' THEN 'view'
		         WHEN 'MATERIALIZED VIEW' THEN 'materialized_view'
		       END AS kind,
		       tc.comments AS "comment",
		       t.num_rows,
		       o.created AS created_at,
		       o.last_ddl_time AS updated_at
		  FROM all_objects o
		  LEFT JOIN all_tables t
		    ON t.owner = o.owner AND t.table_name = o.object_name
		  LEFT JOIN all_tab_comments tc
		    ON tc.owner = o.owner AND tc.table_name = o.object_name
		 WHERE o.owner = ?
		   AND o.object_type IN ('TABLE', 'VIEW', 'MATERIALIZED VIEW')
		   AND (o.object_type <> 'TABLE' OR NOT EXISTS (
		         SELECT 1 FROM all_mviews mv
		          WHERE mv.owner = o.owner AND mv.mview_name = o.object_name
		       ))
		 ORDER BY o.object_name
	`, schema).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list Oracle tables: %w", err)
	}
	tables := make([]datatype.TableInfo, 0, len(rows))
	for _, row := range rows {
		tables = append(tables, datatype.TableInfo{
			Name:              row.Name,
			Kind:              row.Kind,
			Comment:           row.Comment,
			EstimatedRowCount: row.NumRows,
			CreatedAt:         row.CreatedAt,
			UpdatedAt:         row.UpdatedAt,
			Native: datatype.FilterTableNative(map[string]interface{}{
				"object_type": row.ObjectType,
			}, oracleTableNativeKeys),
		})
	}
	return tables, nil
}

type oracleColumnRow struct {
	Name              string
	DataType          string
	DataTypeOwner     sql.NullString
	DataLength        int
	CharLength        sql.NullInt64
	NumericPrecision  sql.NullInt64
	NumericScale      sql.NullInt64
	NullableFlag      int
	PrimaryKeyFlag    int
	Comment           sql.NullString
	OrdinalPosition   int
	DefaultExpression sql.NullString
	VirtualColumn     string
}

func (p *OraclePlugin) listColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]datatype.FieldInfo, error) {
	if p.isSystemSchema(schema) {
		return nil, plugin.WrapCatalogError(plugin.CatalogErrorUnsupported, fmt.Errorf("Oracle system schema %q is not exposed", schema))
	}
	var rows []oracleColumnRow
	err := db.WithContext(ctx).Raw(`
		SELECT c.column_name AS name,
		       c.data_type,
		       c.data_type_owner,
		       c.data_length,
		       c.char_length,
		       c.data_precision AS numeric_precision,
		       c.data_scale AS numeric_scale,
		       CASE c.nullable WHEN 'Y' THEN 1 ELSE 0 END AS nullable_flag,
		       CASE WHEN pk.column_name IS NOT NULL THEN 1 ELSE 0 END AS primary_key_flag,
		       cc.comments AS "comment",
		       c.column_id AS ordinal_position,
		       c.data_default AS default_expression,
		       c.virtual_column
		  FROM all_tab_cols c
		  LEFT JOIN (
		        SELECT cols.owner, cols.table_name, cols.column_name
		          FROM all_constraints cons
		          JOIN all_cons_columns cols
		            ON cols.owner = cons.owner
		           AND cols.constraint_name = cons.constraint_name
		           AND cols.table_name = cons.table_name
		         WHERE cons.constraint_type = 'P'
		       ) pk
		    ON pk.owner = c.owner
		   AND pk.table_name = c.table_name
		   AND pk.column_name = c.column_name
		  LEFT JOIN all_col_comments cc
		    ON cc.owner = c.owner
		   AND cc.table_name = c.table_name
		   AND cc.column_name = c.column_name
		 WHERE c.owner = ?
		   AND c.table_name = ?
		   AND c.hidden_column = 'NO'
		 ORDER BY c.column_id
	`, schema, table).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list Oracle columns: %w", err)
	}
	fields := make([]datatype.FieldInfo, 0, len(rows))
	mapper := &oraclemapping.TypeMapper{}
	for _, row := range rows {
		nativeType := oracleColumnNativeType(row)
		field := datatype.FieldInfo{
			Name:              row.Name,
			Type:              mapper.ToCommon(nativeType),
			NativeType:        nativeType,
			Nullable:          row.NullableFlag == 1,
			PrimaryKey:        row.PrimaryKeyFlag == 1,
			Comment:           row.Comment.String,
			OrdinalPosition:   row.OrdinalPosition,
			DefaultExpression: strings.TrimSpace(row.DefaultExpression.String),
			Generated:         strings.EqualFold(row.VirtualColumn, "YES"),
		}
		if row.CharLength.Valid && row.CharLength.Int64 > 0 {
			field.Size = int(row.CharLength.Int64)
		} else if row.DataLength > 0 && field.Type == datatype.FieldTypeBytes {
			field.Size = row.DataLength
		}
		if row.NumericPrecision.Valid {
			field.Precision = int(row.NumericPrecision.Int64)
		}
		if row.NumericScale.Valid {
			field.Scale = int(row.NumericScale.Int64)
		}
		fields = append(fields, field)
	}
	return plugin.NormalizeFieldInfos(fields), nil
}

func oracleColumnNativeType(row oracleColumnRow) string {
	dataType := strings.ToUpper(strings.TrimSpace(row.DataType))
	if row.DataTypeOwner.Valid && strings.TrimSpace(row.DataTypeOwner.String) != "" {
		return strings.ToUpper(strings.TrimSpace(row.DataTypeOwner.String)) + "." + dataType
	}
	switch dataType {
	case "NUMBER", "DECIMAL", "NUMERIC", "FLOAT":
		if row.NumericPrecision.Valid {
			if row.NumericScale.Valid {
				return fmt.Sprintf("%s(%d,%d)", dataType, row.NumericPrecision.Int64, row.NumericScale.Int64)
			}
			return fmt.Sprintf("%s(%d)", dataType, row.NumericPrecision.Int64)
		}
	case "CHAR", "VARCHAR2", "NCHAR", "NVARCHAR2":
		if row.CharLength.Valid && row.CharLength.Int64 > 0 {
			return fmt.Sprintf("%s(%d)", dataType, row.CharLength.Int64)
		}
	case "RAW":
		if row.DataLength > 0 {
			return fmt.Sprintf("RAW(%d)", row.DataLength)
		}
	}
	return dataType
}

type oracleIndexRow struct {
	Name           string
	IndexType      string
	Uniqueness     string
	ColumnName     string
	ColumnPosition int
}

func (p *OraclePlugin) listIndexes(ctx context.Context, db *gorm.DB, schema, table string) ([]plugin.IndexFacts, error) {
	if p.isSystemSchema(schema) {
		return nil, plugin.WrapCatalogError(plugin.CatalogErrorUnsupported, fmt.Errorf("Oracle system schema %q is not exposed", schema))
	}
	var rows []oracleIndexRow
	err := db.WithContext(ctx).Raw(`
		SELECT i.index_name AS name,
		       i.index_type,
		       i.uniqueness,
		       ic.column_name,
		       ic.column_position
		  FROM all_indexes i
		  JOIN all_ind_columns ic
		    ON ic.index_owner = i.owner
		   AND ic.index_name = i.index_name
		   AND ic.table_owner = i.table_owner
		   AND ic.table_name = i.table_name
		  JOIN all_tab_cols c
		    ON c.owner = ic.table_owner
		   AND c.table_name = ic.table_name
		   AND c.column_name = ic.column_name
		   AND c.hidden_column = 'NO'
		 WHERE i.table_owner = ?
		   AND i.table_name = ?
		 ORDER BY i.index_name, ic.column_position
	`, schema, table).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list Oracle indexes: %w", err)
	}
	return buildOracleIndexFacts(rows), nil
}

func buildOracleIndexFacts(rows []oracleIndexRow) []plugin.IndexFacts {
	result := make([]plugin.IndexFacts, 0)
	byName := make(map[string]int)
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		field := strings.TrimSpace(row.ColumnName)
		if name == "" || field == "" {
			continue
		}
		index, found := byName[name]
		if !found {
			index = len(result)
			byName[name] = index
			result = append(result, plugin.IndexFacts{
				Name:      name,
				IsUnique:  strings.EqualFold(strings.TrimSpace(row.Uniqueness), "UNIQUE"),
				IndexType: strings.ToLower(strings.TrimSpace(row.IndexType)),
			})
		}
		result[index].Fields = append(result[index].Fields, field)
	}
	return result
}

type oracleConstraintRow struct {
	Name                string
	ConstraintType      string
	ColumnName          string
	Position            int
	ReferencedNamespace sql.NullString
	ReferencedTable     sql.NullString
	ReferencedColumn    sql.NullString
}

func (p *OraclePlugin) listConstraints(ctx context.Context, db *gorm.DB, schema, table string) ([]plugin.ConstraintFacts, error) {
	if p.isSystemSchema(schema) {
		return nil, plugin.WrapCatalogError(plugin.CatalogErrorUnsupported, fmt.Errorf("Oracle system schema %q is not exposed", schema))
	}
	var rows []oracleConstraintRow
	err := db.WithContext(ctx).Raw(`
		SELECT c.constraint_name AS name,
		       c.constraint_type,
		       cc.column_name,
		       cc.position,
		       rc.owner AS referenced_namespace,
		       rc.table_name AS referenced_table,
		       rcc.column_name AS referenced_column
		  FROM all_constraints c
		  JOIN all_cons_columns cc
		    ON cc.owner = c.owner
		   AND cc.constraint_name = c.constraint_name
		   AND cc.table_name = c.table_name
		  LEFT JOIN all_constraints rc
		    ON rc.owner = c.r_owner
		   AND rc.constraint_name = c.r_constraint_name
		  LEFT JOIN all_cons_columns rcc
		    ON rcc.owner = rc.owner
		   AND rcc.constraint_name = rc.constraint_name
		   AND rcc.position = cc.position
		 WHERE c.owner = ?
		   AND c.table_name = ?
		   AND c.constraint_type IN ('P', 'U', 'R')
		 ORDER BY c.constraint_name, cc.position
	`, schema, table).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list Oracle constraints: %w", err)
	}
	return buildOracleConstraintFacts(rows), nil
}

func buildOracleConstraintFacts(rows []oracleConstraintRow) []plugin.ConstraintFacts {
	result := make([]plugin.ConstraintFacts, 0)
	byName := make(map[string]int)
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		field := strings.TrimSpace(row.ColumnName)
		constraintType := oracleConstraintType(row.ConstraintType)
		if name == "" || field == "" || constraintType == "" {
			continue
		}
		index, found := byName[name]
		if !found {
			index = len(result)
			byName[name] = index
			result = append(result, plugin.ConstraintFacts{
				Name:                name,
				ConstraintType:      constraintType,
				ReferencedNamespace: strings.TrimSpace(row.ReferencedNamespace.String),
				ReferencedTable:     strings.TrimSpace(row.ReferencedTable.String),
			})
		}
		result[index].Fields = append(result[index].Fields, field)
		if referencedField := strings.TrimSpace(row.ReferencedColumn.String); referencedField != "" {
			result[index].ReferencedFields = append(result[index].ReferencedFields, referencedField)
		}
	}
	return result
}

func oracleConstraintType(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "P":
		return plugin.ConstraintTypePrimaryKey
	case "U":
		return plugin.ConstraintTypeUnique
	case "R":
		return plugin.ConstraintTypeForeignKey
	default:
		return ""
	}
}

type oraclePartitioningRow struct {
	Strategy             string
	SubpartitionStrategy string
	PartitionCount       int
}

type oraclePartitionKeyRow struct {
	ColumnName     string
	ColumnPosition int
}

func (p *OraclePlugin) describePartitioning(ctx context.Context, db *gorm.DB, schema, table string) (*plugin.TablePartitioningFacts, error) {
	if p.isSystemSchema(schema) {
		return nil, plugin.WrapCatalogError(plugin.CatalogErrorUnsupported, fmt.Errorf("Oracle system schema %q is not exposed", schema))
	}
	var rows []oraclePartitioningRow
	err := db.WithContext(ctx).Raw(`
		SELECT partitioning_type AS strategy,
		       subpartitioning_type AS subpartition_strategy,
		       partition_count
		  FROM all_part_tables
		 WHERE owner = ?
		   AND table_name = ?
	`, schema, table).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to describe Oracle partitioning: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	partitionKeys, err := p.listPartitionKeys(ctx, db, "all_part_key_columns", schema, table)
	if err != nil {
		return nil, err
	}
	subpartitionKeys, err := p.listPartitionKeys(ctx, db, "all_subpart_key_columns", schema, table)
	if err != nil {
		return nil, err
	}
	return &plugin.TablePartitioningFacts{
		Strategy:              normalizeOraclePartitionStrategy(rows[0].Strategy),
		KeyFields:             partitionKeys,
		SubpartitionStrategy:  normalizeOraclePartitionStrategy(rows[0].SubpartitionStrategy),
		SubpartitionKeyFields: subpartitionKeys,
		PartitionCount:        rows[0].PartitionCount,
	}, nil
}

func (p *OraclePlugin) listPartitionKeys(ctx context.Context, db *gorm.DB, dictionaryView, schema, table string) ([]string, error) {
	switch dictionaryView {
	case "all_part_key_columns", "all_subpart_key_columns":
	default:
		return nil, fmt.Errorf("unsupported Oracle partition dictionary view %q", dictionaryView)
	}
	var rows []oraclePartitionKeyRow
	query := fmt.Sprintf(`
		SELECT column_name, column_position
		  FROM %s
		 WHERE owner = ?
		   AND name = ?
		   AND object_type = 'TABLE'
		 ORDER BY column_position
	`, dictionaryView)
	if err := db.WithContext(ctx).Raw(query, schema, table).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list Oracle partition keys: %w", err)
	}
	fields := make([]string, 0, len(rows))
	for _, row := range rows {
		if field := strings.TrimSpace(row.ColumnName); field != "" {
			fields = append(fields, field)
		}
	}
	return fields, nil
}

func normalizeOraclePartitionStrategy(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "none" {
		return ""
	}
	return value
}

func (p *OraclePlugin) getTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	if p.isSystemSchema(schema) {
		return 0, plugin.WrapCatalogError(plugin.CatalogErrorUnsupported, fmt.Errorf("Oracle system schema %q is not exposed", schema))
	}
	var count int64
	query := commonquery.ForEngine(p.Type()).CountTableSQL(schema, table, "")
	if err := db.WithContext(ctx).Raw(query).Scan(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count Oracle table rows: %w", err)
	}
	return count, nil
}

func (p *OraclePlugin) isSystemSchema(schema string) bool {
	_, found := oracleSystemSchemas[strings.ToUpper(strings.TrimSpace(schema))]
	return found
}
