package capture

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	mysqltypes "github.com/addp/common/format/mappers/mysql"
	postgresqltypes "github.com/addp/common/format/mappers/postgresql"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

type CapturePlan struct {
	SourceType                  models.CaptureSourceType
	SourceConnInfo              engineplugin.ConnectionInfo
	TargetConnInfo              engineplugin.ConnectionInfo
	SourceEngineID              uint
	TargetEngineID              uint
	SourceDatabase              string
	SourceSchema                string
	SourceTable                 string
	TargetSchema                string
	TargetTable                 string
	SourceIdentity              string
	SourceConnectionFingerprint string
	SourceKeys                  []string
	SourceFields                []string
	SourceFieldTypes            map[string]datatype.FieldType
	SourceFieldNullables        map[string]bool
	SourceSpatialInfo           *datatype.SpatialInfo
	TargetKeys                  []string
}

type PlanResolver interface {
	Resolve(ctx context.Context, task *models.TransferTask) (*CapturePlan, error)
	ResolveForCleanup(ctx context.Context, task *models.TransferTask) (*CapturePlan, error)
}

type DatabasePlanResolver struct {
	engines planner.EngineResolver
}

func NewDatabasePlanResolver(engines planner.EngineResolver) *DatabasePlanResolver {
	return &DatabasePlanResolver{engines: engines}
}

func (r *DatabasePlanResolver) Resolve(ctx context.Context, task *models.TransferTask) (*CapturePlan, error) {
	plan, err := r.resolveBindings(task)
	if err != nil {
		return nil, err
	}
	switch plan.SourceType {
	case models.CaptureSourcePostgreSQL:
		if err := validatePostgreSQLSourceFields(ctx, plan); err != nil {
			return nil, err
		}
		if err := validatePostgreSQLSourcePrimaryKey(ctx, plan); err != nil {
			return nil, err
		}
	case models.CaptureSourceMySQL:
		if err := validateMySQLCaptureSettings(ctx, plan); err != nil {
			return nil, err
		}
		if err := validateMySQLSourceFields(ctx, plan); err != nil {
			return nil, err
		}
		if err := validateMySQLSourcePrimaryKey(ctx, plan); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported database CDC source type %q", plan.SourceType)
	}
	if err := validateTargetDoesNotExist(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (r *DatabasePlanResolver) ResolveForCleanup(_ context.Context, task *models.TransferTask) (*CapturePlan, error) {
	if task == nil || r == nil || r.engines == nil {
		return nil, fmt.Errorf("database CDC capture cleanup requires task and engine resolver")
	}
	spec, err := planner.ParseDatabaseCDCTaskSpec(task.Config)
	if err != nil {
		return nil, err
	}
	sourceRef, err := spec.Source.EngineRef()
	if err != nil {
		return nil, err
	}
	source, err := r.engines.ResolveEngine(sourceRef)
	if err != nil {
		return nil, fmt.Errorf("resolve database CDC source engine for cleanup: %w", err)
	}
	sourceType := strings.ToLower(strings.TrimSpace(source.Type))
	if sourceType != "postgresql" && sourceType != "mysql" {
		return nil, fmt.Errorf("unsupported database CDC source engine %q", sourceType)
	}
	sourceLocator, _ := spec.Source.ResourceLocator()
	plan := &CapturePlan{
		SourceConnInfo: source.ConnInfo, SourceEngineID: source.EngineID,
		SourceTable: sourceLocator.Path[1], SourceIdentity: strings.TrimSpace(spec.Source.Locator),
	}
	switch sourceType {
	case "postgresql":
		plan.SourceType = models.CaptureSourcePostgreSQL
		plan.SourceDatabase = engineplugin.GetString(source.ConnInfo, "database")
		plan.SourceSchema = sourceLocator.Path[0]
		plan.SourceConnectionFingerprint = postgresConnectionFingerprint(source.ConnInfo)
	case "mysql":
		plan.SourceType = models.CaptureSourceMySQL
		plan.SourceDatabase = sourceLocator.Path[0]
		plan.SourceConnectionFingerprint = mysqlConnectionFingerprint(source.ConnInfo, plan.SourceDatabase)
	}
	if plan.SourceDatabase == "" {
		return nil, fmt.Errorf("database CDC source database is required")
	}
	return plan, nil
}

func (r *DatabasePlanResolver) resolveBindings(task *models.TransferTask) (*CapturePlan, error) {
	if task == nil || r == nil || r.engines == nil {
		return nil, fmt.Errorf("database CDC capture requires task and engine resolver")
	}
	spec, err := planner.ParseDatabaseCDCTaskSpec(task.Config)
	if err != nil {
		return nil, err
	}
	bindings, err := planner.ResolveDatabaseCDCBindings(spec, r.engines)
	if err != nil {
		return nil, err
	}
	sourceLocator, _ := spec.Source.ResourceLocator()
	targetParent, _ := spec.Target.ParentResourceLocator()
	sourceKeys, targetKeys, err := planner.DatabaseCDCSourceToTargetKeys(spec)
	if err != nil {
		return nil, err
	}
	plan := &CapturePlan{
		SourceConnInfo: bindings.Source.ConnInfo, TargetConnInfo: bindings.Target.ConnInfo,
		SourceEngineID: bindings.Source.EngineID, TargetEngineID: bindings.Target.EngineID,
		SourceTable:  sourceLocator.Path[1],
		TargetSchema: targetParent.Path[0], TargetTable: strings.TrimSpace(spec.Target.Name),
		SourceIdentity: strings.TrimSpace(spec.Source.Locator),
		SourceKeys:     sourceKeys, SourceFieldTypes: make(map[string]datatype.FieldType, len(spec.Transforms[0].Fields)),
		SourceFieldNullables: make(map[string]bool, len(spec.Transforms[0].Fields)), TargetKeys: targetKeys,
	}
	switch bindings.SourceType {
	case "postgresql":
		plan.SourceType = models.CaptureSourcePostgreSQL
		plan.SourceDatabase = engineplugin.GetString(bindings.Source.ConnInfo, "database")
		plan.SourceSchema = sourceLocator.Path[0]
		plan.SourceConnectionFingerprint = postgresConnectionFingerprint(bindings.Source.ConnInfo)
	case "mysql":
		plan.SourceType = models.CaptureSourceMySQL
		plan.SourceDatabase = sourceLocator.Path[0]
		plan.SourceConnectionFingerprint = mysqlConnectionFingerprint(bindings.Source.ConnInfo, plan.SourceDatabase)
	}
	for _, field := range spec.Transforms[0].Fields {
		sourceField := strings.TrimSpace(field.Source)
		plan.SourceFields = append(plan.SourceFields, sourceField)
		plan.SourceFieldTypes[sourceField] = datatype.ParseFieldType(field.TargetType)
		plan.SourceFieldNullables[sourceField] = *field.Nullable
	}
	if plan.SourceDatabase == "" {
		return nil, fmt.Errorf("database CDC source database is required")
	}
	return plan, nil
}

func validatePostgreSQLSourceFields(ctx context.Context, plan *CapturePlan) error {
	db, err := openPostgreSQL(plan.SourceConnInfo)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT a.attname, format_type(a.atttypid, a.atttypmod), ic.datetime_precision, NOT a.attnotnull
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN information_schema.columns ic
		  ON ic.table_schema = n.nspname AND ic.table_name = c.relname AND ic.column_name = a.attname
		WHERE n.nspname = $1 AND c.relname = $2
		  AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, plan.SourceSchema, plan.SourceTable)
	if err != nil {
		return fmt.Errorf("query PostgreSQL CDC source fields: %w", err)
	}
	defer rows.Close()
	actual := make([]string, 0)
	actualTypes := make(map[string]string)
	temporalPrecisions := make(map[string]sql.NullInt64)
	nullableFields := make(map[string]bool)
	for rows.Next() {
		var name, nativeType string
		var temporalPrecision sql.NullInt64
		var nullable bool
		if err := rows.Scan(&name, &nativeType, &temporalPrecision, &nullable); err != nil {
			return err
		}
		actual = append(actual, name)
		actualTypes[name] = nativeType
		temporalPrecisions[name] = temporalPrecision
		nullableFields[name] = nullable
	}
	if err := rows.Err(); err != nil {
		return err
	}
	configured := append([]string(nil), plan.SourceFields...)
	sort.Strings(actual)
	sort.Strings(configured)
	if !reflect.DeepEqual(actual, configured) {
		return fmt.Errorf("PostgreSQL CDC field mapping must cover the complete source schema: actual=%v configured=%v", actual, configured)
	}
	spatialColumns := make([]datatype.GeometryColumnInfo, 0)
	for _, name := range actual {
		nativeType := actualTypes[name]
		if err := validatePostgreSQLCDCSourceFieldType(name, nativeType, temporalPrecisions[name], plan.SourceFieldTypes[name]); err != nil {
			return err
		}
		if nullableFields[name] != plan.SourceFieldNullables[name] {
			return fmt.Errorf("PostgreSQL CDC source field %q nullable=%t, but field_mapping declares nullable=%t", name, nullableFields[name], plan.SourceFieldNullables[name])
		}
		if plan.SourceFieldTypes[name] == datatype.FieldTypeGeometry {
			geometryType, srid, dimension, err := parsePostgreSQLCDCGeometryType(nativeType)
			if err != nil {
				return fmt.Errorf("PostgreSQL CDC source field %q: %w", name, err)
			}
			nullable := nullableFields[name]
			spatialColumns = append(spatialColumns, datatype.GeometryColumnInfo{
				Name: name, GeometryType: geometryType, SRID: &srid,
				CRSRef: datatype.EPSGCRSRef(srid), Dimension: &dimension, Nullable: &nullable,
			})
		}
	}
	if len(spatialColumns) > 0 {
		plan.SourceSpatialInfo = &datatype.SpatialInfo{
			GeometryColumns: spatialColumns, PrimaryGeometryColumn: spatialColumns[0].Name,
		}
		if len(spatialColumns) == 1 {
			plan.SourceSpatialInfo.SRID = spatialColumns[0].SRID
			plan.SourceSpatialInfo.CRSRef = spatialColumns[0].CRSRef
		}
	}
	return nil
}

func postgresqlCDCCommonFieldType(nativeType string) datatype.FieldType {
	return (&postgresqltypes.TypeMapper{}).ToCommon(nativeType)
}

func validatePostgreSQLCDCSourceFieldType(name, nativeType string, temporalPrecision sql.NullInt64, configuredType datatype.FieldType) error {
	actualType := postgresqlCDCCommonFieldType(nativeType)
	if actualType == datatype.FieldTypeUnknown || (!planner.ContinuousFieldTypeSupported(actualType) && actualType != datatype.FieldTypeGeometry) {
		return fmt.Errorf("PostgreSQL CDC source field %q uses unsupported PostgreSQL type %q", name, nativeType)
	}
	if actualType != configuredType {
		return fmt.Errorf("PostgreSQL CDC source field %q type %q maps to %q, but field_mapping declares %q", name, nativeType, actualType, configuredType)
	}
	if usesConnectMillisecondTemporalEncoding(nativeType, actualType) {
		if !temporalPrecision.Valid {
			return fmt.Errorf("PostgreSQL CDC source field %q type %q has unknown temporal precision", name, nativeType)
		}
		if temporalPrecision.Int64 > 3 {
			return fmt.Errorf("PostgreSQL CDC source field %q type %q uses precision %d, but CDC v1 only preserves millisecond precision", name, nativeType, temporalPrecision.Int64)
		}
	}
	return nil
}

func parsePostgreSQLCDCGeometryType(nativeType string) (string, int, int, error) {
	value := strings.TrimSpace(nativeType)
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "geometry(") || !strings.HasSuffix(lower, ")") {
		return "", 0, 0, fmt.Errorf("geometry type %q must declare a concrete OGC type and positive SRID", nativeType)
	}
	inner := strings.TrimSpace(value[strings.Index(value, "(")+1 : len(value)-1])
	parts := strings.Split(inner, ",")
	if len(parts) != 2 {
		return "", 0, 0, fmt.Errorf("geometry type %q must declare exactly type and SRID", nativeType)
	}
	typeToken := strings.TrimSpace(parts[0])
	typeLower := strings.ToLower(typeToken)
	if strings.HasSuffix(typeLower, "zm") || strings.HasSuffix(typeLower, "m") {
		return "", 0, 0, fmt.Errorf("geometry type %q uses unsupported M/ZM coordinates", nativeType)
	}
	dimension := 2
	if strings.HasSuffix(typeLower, "z") {
		dimension = 3
	}
	geometryType := datatype.StandardGeometryType(typeToken)
	if geometryType == "" || geometryType == string(datatype.GeometryTypeGeometry) {
		return "", 0, 0, fmt.Errorf("geometry type %q must use a concrete supported OGC type", nativeType)
	}
	srid, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || srid <= 0 {
		return "", 0, 0, fmt.Errorf("geometry type %q must use a positive SRID", nativeType)
	}
	return geometryType, srid, dimension, nil
}

func usesConnectMillisecondTemporalEncoding(nativeType string, fieldType datatype.FieldType) bool {
	if fieldType == datatype.FieldTypeTime {
		return true
	}
	if fieldType != datatype.FieldTypeTimestamp {
		return false
	}
	return !strings.Contains(strings.ToLower(nativeType), "with time zone")
}

func postgresConnectionFingerprint(connInfo engineplugin.ConnectionInfo) string {
	parts := engineplugin.ParseDriverConnInfo(connInfo, 5432, "")
	identity := strings.ToLower(strings.TrimSpace(parts.Host)) + ":" + strconv.Itoa(parts.Port) + "/" + strings.TrimSpace(parts.Database)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}

func mysqlConnectionFingerprint(connInfo engineplugin.ConnectionInfo, database string) string {
	parts := engineplugin.ParseDriverConnInfo(connInfo, 3306, "")
	identity := strings.ToLower(strings.TrimSpace(parts.Host)) + ":" + strconv.Itoa(parts.Port) + "/" + strings.TrimSpace(database)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}

func validatePostgreSQLSourcePrimaryKey(ctx context.Context, plan *CapturePlan) error {
	db, err := openPostgreSQL(plan.SourceConnInfo)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN unnest(i.indkey) WITH ORDINALITY AS k(attnum, ordinality) ON TRUE
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
		WHERE i.indisprimary AND n.nspname = $1 AND c.relname = $2
		ORDER BY k.ordinality`, plan.SourceSchema, plan.SourceTable)
	if err != nil {
		return fmt.Errorf("query PostgreSQL CDC source primary key: %w", err)
	}
	defer rows.Close()
	var actual []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		actual = append(actual, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(actual) == 0 {
		return fmt.Errorf("PostgreSQL CDC source table requires a primary key")
	}
	for _, key := range actual {
		if plan.SourceFieldTypes[key] == datatype.FieldTypeGeometry {
			return fmt.Errorf("PostgreSQL CDC source primary key field %q cannot use geometry", key)
		}
	}
	if !reflect.DeepEqual(actual, plan.SourceKeys) {
		return fmt.Errorf("PostgreSQL CDC source primary key %v must map one-to-one to configured target keys via source fields %v", actual, plan.SourceKeys)
	}
	return nil
}

func validateMySQLCaptureSettings(ctx context.Context, plan *CapturePlan) error {
	db, err := openMySQL(plan.SourceConnInfo)
	if err != nil {
		return err
	}
	defer db.Close()
	var version, logBin, binlogFormat, rowImage, gtidMode string
	var serverID uint64
	if err := db.QueryRowContext(ctx, `
		SELECT VERSION(), @@GLOBAL.log_bin, @@GLOBAL.binlog_format,
		       @@GLOBAL.binlog_row_image, @@GLOBAL.server_id, @@GLOBAL.gtid_mode`).
		Scan(&version, &logBin, &binlogFormat, &rowImage, &serverID, &gtidMode); err != nil {
		return fmt.Errorf("query MySQL CDC server settings: %w", err)
	}
	major := strings.SplitN(strings.TrimSpace(version), ".", 2)[0]
	if major != "8" {
		return fmt.Errorf("MySQL CDC v1 requires MySQL 8.x, got %q", version)
	}
	if !mysqlSystemVariableEnabled(logBin) || !strings.EqualFold(binlogFormat, "ROW") || !strings.EqualFold(rowImage, "FULL") || serverID == 0 {
		return fmt.Errorf("MySQL CDC requires log_bin=ON, binlog_format=ROW, binlog_row_image=FULL and non-zero server_id (actual log_bin=%q binlog_format=%q binlog_row_image=%q server_id=%d)", logBin, binlogFormat, rowImage, serverID)
	}
	if !strings.EqualFold(gtidMode, "ON") && !strings.EqualFold(gtidMode, "OFF") {
		return fmt.Errorf("MySQL CDC v1 supports gtid_mode ON or OFF, got %q", gtidMode)
	}
	rows, err := db.QueryContext(ctx, `SHOW MASTER STATUS`)
	if err != nil {
		return fmt.Errorf("MySQL CDC connector credentials cannot read binary log status: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return fmt.Errorf("MySQL CDC binary log status is empty")
	}
	return nil
}

func mysqlSystemVariableEnabled(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "ON") || strings.TrimSpace(value) == "1"
}

func validateMySQLSourceFields(ctx context.Context, plan *CapturePlan) error {
	db, err := openMySQL(plan.SourceConnInfo)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT column_name, column_type, data_type, datetime_precision, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position`, plan.SourceDatabase, plan.SourceTable)
	if err != nil {
		return fmt.Errorf("query MySQL CDC source fields: %w", err)
	}
	defer rows.Close()
	actual := make([]string, 0)
	for rows.Next() {
		var name, columnType, dataType, nullableText string
		var temporalPrecision sql.NullInt64
		var defaultValue sql.NullString
		if err := rows.Scan(&name, &columnType, &dataType, &temporalPrecision, &nullableText, &defaultValue); err != nil {
			return err
		}
		actual = append(actual, name)
		if err := validateMySQLCDCSourceFieldType(name, columnType, dataType, temporalPrecision, defaultValue, plan.SourceFieldTypes[name]); err != nil {
			return err
		}
		actualNullable := strings.EqualFold(nullableText, "YES")
		if actualNullable != plan.SourceFieldNullables[name] {
			return fmt.Errorf("MySQL CDC source field %q nullable=%t, but field_mapping declares nullable=%t", name, actualNullable, plan.SourceFieldNullables[name])
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	configured := append([]string(nil), plan.SourceFields...)
	sort.Strings(actual)
	sort.Strings(configured)
	if !reflect.DeepEqual(actual, configured) {
		return fmt.Errorf("MySQL CDC field mapping must cover the complete source schema: actual=%v configured=%v", actual, configured)
	}
	return nil
}

func validateMySQLCDCSourceFieldType(name, columnType, dataType string, temporalPrecision sql.NullInt64, defaultValue sql.NullString, configuredType datatype.FieldType) error {
	columnTypeLower := strings.ToLower(strings.TrimSpace(columnType))
	dataTypeLower := strings.ToLower(strings.TrimSpace(dataType))
	if strings.Contains(columnTypeLower, " unsigned") {
		return fmt.Errorf("MySQL CDC source field %q uses unsupported unsigned type %q", name, columnType)
	}
	if dataTypeLower == "tinyint" && strings.HasPrefix(columnTypeLower, "tinyint(1)") {
		return fmt.Errorf("MySQL CDC source field %q uses ambiguous tinyint(1) boolean encoding", name)
	}
	actualType := (&mysqltypes.TypeMapper{}).ToCommon(dataTypeLower)
	switch dataTypeLower {
	case "tinyint", "smallint", "mediumint", "int", "integer", "bigint",
		"char", "varchar", "tinytext", "text", "mediumtext", "longtext",
		"decimal", "numeric", "float", "double", "real",
		"date", "time", "datetime", "timestamp", "json",
		"binary", "varbinary", "tinyblob", "blob", "mediumblob", "longblob":
	default:
		return fmt.Errorf("MySQL CDC source field %q uses unsupported MySQL type %q", name, columnType)
	}
	if actualType == datatype.FieldTypeUnknown || actualType != configuredType {
		return fmt.Errorf("MySQL CDC source field %q type %q maps to %q, but field_mapping declares %q", name, columnType, actualType, configuredType)
	}
	if dataTypeLower == "time" || dataTypeLower == "datetime" || dataTypeLower == "timestamp" {
		if !temporalPrecision.Valid || temporalPrecision.Int64 > 3 {
			return fmt.Errorf("MySQL CDC source field %q type %q must declare precision no greater than 3", name, columnType)
		}
	}
	if defaultValue.Valid && (dataTypeLower == "date" || dataTypeLower == "datetime" || dataTypeLower == "timestamp") && strings.HasPrefix(defaultValue.String, "0000-00-00") {
		return fmt.Errorf("MySQL CDC source field %q has unsupported zero-date default", name)
	}
	return nil
}

func validateMySQLSourcePrimaryKey(ctx context.Context, plan *CapturePlan) error {
	db, err := openMySQL(plan.SourceConnInfo)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT column_name
		FROM information_schema.statistics
		WHERE table_schema = ? AND table_name = ? AND index_name = 'PRIMARY'
		ORDER BY seq_in_index`, plan.SourceDatabase, plan.SourceTable)
	if err != nil {
		return fmt.Errorf("query MySQL CDC source primary key: %w", err)
	}
	defer rows.Close()
	var actual []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		actual = append(actual, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(actual) == 0 {
		return fmt.Errorf("MySQL CDC source table requires a primary key")
	}
	if !reflect.DeepEqual(actual, plan.SourceKeys) {
		return fmt.Errorf("MySQL CDC source primary key %v must map one-to-one to configured target keys via source fields %v", actual, plan.SourceKeys)
	}
	return nil
}

func validateTargetDoesNotExist(ctx context.Context, plan *CapturePlan) error {
	db, err := openPostgreSQL(plan.TargetConnInfo)
	if err != nil {
		return err
	}
	defer db.Close()
	var exists bool
	err = db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r','p')
		)`, plan.TargetSchema, plan.TargetTable).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check PostgreSQL CDC target table: %w", err)
	}
	if exists {
		return fmt.Errorf("PostgreSQL CDC initial snapshot target table must not already exist")
	}
	return nil
}

func openPostgreSQL(connInfo engineplugin.ConnectionInfo) (*sql.DB, error) {
	dsn, err := engineplugin.BuildPostgreSQLDSN(connInfo, 5432)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func openMySQL(connInfo engineplugin.ConnectionInfo) (*sql.DB, error) {
	dsn, err := engineplugin.BuildMySQLCompatibleDSN(connInfo, 3306, "MySQL", map[string]string{
		"timeout": "10s", "readTimeout": "15s", "writeTimeout": "15s", "parseTime": "false",
	})
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(30 * time.Second)
	return db, nil
}

func buildConnectorConfig(plan *CapturePlan, resource *models.CaptureResource, config SupervisorConfig) (map[string]string, error) {
	if plan == nil || resource == nil || plan.SourceType != resource.SourceType {
		return nil, fmt.Errorf("database connector config requires matching capture generation and provider resources")
	}
	switch plan.SourceType {
	case models.CaptureSourcePostgreSQL:
		return buildPostgreSQLConnectorConfig(plan, resource, config.ConnectLoopbackHost)
	case models.CaptureSourceMySQL:
		return buildMySQLConnectorConfig(plan, resource, config)
	default:
		return nil, fmt.Errorf("unsupported database connector source type %q", plan.SourceType)
	}
}

func buildPostgreSQLConnectorConfig(plan *CapturePlan, resource *models.CaptureResource, connectLoopbackHost string) (map[string]string, error) {
	if resource.PostgreSQL == nil {
		return nil, fmt.Errorf("PostgreSQL connector config requires provider resources")
	}
	parts := engineplugin.ParseDriverConnInfo(plan.SourceConnInfo, 5432, "")
	host := parts.Host
	if (host == "localhost" || host == "127.0.0.1" || host == "::1") && strings.TrimSpace(connectLoopbackHost) != "" {
		host = strings.TrimSpace(connectLoopbackHost)
	}
	sslMode := strings.TrimSpace(engineplugin.GetString(plan.SourceConnInfo, "sslmode"))
	if sslMode == "" {
		sslMode = "disable"
	}
	return map[string]string{
		"connector.class":                "io.debezium.connector.postgresql.PostgresConnector",
		"tasks.max":                      "1",
		"database.hostname":              host,
		"database.port":                  strconv.Itoa(parts.Port),
		"database.user":                  parts.User,
		"database.password":              parts.Password,
		"database.dbname":                parts.Database,
		"database.sslmode":               sslMode,
		"topic.prefix":                   resource.ConnectorName,
		"plugin.name":                    "pgoutput",
		"slot.name":                      resource.PostgreSQL.SlotName,
		"publication.name":               resource.PostgreSQL.PublicationName,
		"publication.autocreate.mode":    "filtered",
		"slot.drop.on.stop":              "false",
		"table.include.list":             regexp.QuoteMeta(plan.SourceSchema + "." + plan.SourceTable),
		"snapshot.mode":                  "initial",
		"decimal.handling.mode":          "string",
		"time.precision.mode":            "connect",
		"tombstones.on.delete":           "false",
		"include.schema.changes":         "false",
		"key.converter":                  "org.apache.kafka.connect.json.JsonConverter",
		"key.converter.schemas.enable":   "false",
		"value.converter":                "org.apache.kafka.connect.json.JsonConverter",
		"value.converter.schemas.enable": "false",
		"transforms":                     "route",
		"transforms.route.type":          "org.apache.kafka.connect.transforms.RegexRouter",
		"transforms.route.regex":         ".*",
		"transforms.route.replacement":   resource.TopicName,
	}, nil
}

func buildMySQLConnectorConfig(plan *CapturePlan, resource *models.CaptureResource, config SupervisorConfig) (map[string]string, error) {
	if resource.MySQL == nil || resource.MySQL.ConnectorServerID == 0 || strings.TrimSpace(resource.MySQL.SchemaHistoryTopicName) == "" {
		return nil, fmt.Errorf("MySQL connector config requires provider resources")
	}
	parts := engineplugin.ParseDriverConnInfo(plan.SourceConnInfo, 3306, "")
	if err := parts.Require("MySQL", "host", "user"); err != nil {
		return nil, err
	}
	host := parts.Host
	if (host == "localhost" || host == "127.0.0.1" || host == "::1") && strings.TrimSpace(config.ConnectLoopbackHost) != "" {
		host = strings.TrimSpace(config.ConnectLoopbackHost)
	}
	bootstrapServers := strings.TrimSpace(config.ConnectBootstrapServers)
	if bootstrapServers == "" {
		return nil, fmt.Errorf("MySQL connector requires Kafka Connect-visible bootstrap servers")
	}
	connectorConfig := map[string]string{
		"connector.class":             "io.debezium.connector.mysql.MySqlConnector",
		"tasks.max":                   "1",
		"database.hostname":           host,
		"database.port":               strconv.Itoa(parts.Port),
		"database.user":               parts.User,
		"database.password":           parts.Password,
		"database.server.id":          strconv.FormatUint(uint64(resource.MySQL.ConnectorServerID), 10),
		"database.connectionTimeZone": "UTC",
		"topic.prefix":                resource.ConnectorName,
		"database.include.list":       regexp.QuoteMeta(plan.SourceDatabase),
		"table.include.list":          regexp.QuoteMeta(plan.SourceDatabase + "." + plan.SourceTable),
		"snapshot.mode":               "initial",
		"decimal.handling.mode":       "string",
		"time.precision.mode":         "connect",
		"binary.handling.mode":        "base64",
		"tombstones.on.delete":        "false",
		"include.schema.changes":      "false",
		"include.query":               "false",
		"schema.history.internal.kafka.bootstrap.servers":        bootstrapServers,
		"schema.history.internal.kafka.topic":                    resource.MySQL.SchemaHistoryTopicName,
		"schema.history.internal.store.only.captured.tables.ddl": "true",
		"schema.history.internal.skip.unparseable.ddl":           "false",
		"key.converter":                  "org.apache.kafka.connect.json.JsonConverter",
		"key.converter.schemas.enable":   "false",
		"value.converter":                "org.apache.kafka.connect.json.JsonConverter",
		"value.converter.schemas.enable": "false",
		"transforms":                     "route",
		"transforms.route.type":          "org.apache.kafka.connect.transforms.RegexRouter",
		"transforms.route.regex":         ".*",
		"transforms.route.replacement":   resource.TopicName,
	}
	protocol := strings.ToLower(strings.TrimSpace(config.ConnectKafkaSecurityProtocol))
	if protocol == "" {
		protocol = "sasl_plaintext"
	}
	if protocol != "plaintext" && protocol != "ssl" && protocol != "sasl_plaintext" && protocol != "sasl_ssl" {
		return nil, fmt.Errorf("unsupported MySQL connector Kafka security protocol %q", protocol)
	}
	securityProtocol := strings.ToUpper(protocol)
	for _, role := range []string{"producer", "consumer"} {
		connectorConfig["schema.history.internal."+role+".security.protocol"] = securityProtocol
	}
	if protocol == "sasl_plaintext" || protocol == "sasl_ssl" {
		if strings.TrimSpace(config.ConnectKafkaUsername) == "" || config.ConnectKafkaPassword == "" {
			return nil, fmt.Errorf("MySQL connector schema history requires Kafka Connect SASL credentials")
		}
		mechanism, loginModule, err := kafkaConnectSASL(config.ConnectKafkaSASLMechanism)
		if err != nil {
			return nil, err
		}
		jaas := loginModule + ` required username="` + escapeJAASValue(config.ConnectKafkaUsername) + `" password="` + escapeJAASValue(config.ConnectKafkaPassword) + `";`
		for _, role := range []string{"producer", "consumer"} {
			prefix := "schema.history.internal." + role + "."
			connectorConfig[prefix+"sasl.mechanism"] = mechanism
			connectorConfig[prefix+"sasl.jaas.config"] = jaas
		}
	}
	if protocol == "ssl" || protocol == "sasl_ssl" {
		caFile := strings.TrimSpace(config.ConnectKafkaTLSCACertFile)
		if caFile != "" {
			pem, err := os.ReadFile(caFile)
			if err != nil {
				return nil, fmt.Errorf("read Kafka Connect schema history CA certificate: %w", err)
			}
			if len(strings.TrimSpace(string(pem))) == 0 {
				return nil, fmt.Errorf("Kafka Connect schema history CA certificate is empty")
			}
			for _, role := range []string{"producer", "consumer"} {
				prefix := "schema.history.internal." + role + "."
				connectorConfig[prefix+"ssl.truststore.type"] = "PEM"
				connectorConfig[prefix+"ssl.truststore.certificates"] = string(pem)
			}
		}
	}
	return connectorConfig, nil
}

func kafkaConnectSASL(value string) (string, string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "plain":
		return "PLAIN", "org.apache.kafka.common.security.plain.PlainLoginModule", nil
	case "scram-sha-256":
		return "SCRAM-SHA-256", "org.apache.kafka.common.security.scram.ScramLoginModule", nil
	case "scram-sha-512":
		return "SCRAM-SHA-512", "org.apache.kafka.common.security.scram.ScramLoginModule", nil
	default:
		return "", "", fmt.Errorf("unsupported Kafka Connect SASL mechanism %q", value)
	}
}

func escapeJAASValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}
