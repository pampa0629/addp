package capture

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	postgresqltypes "github.com/addp/common/format/mappers/postgresql"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	_ "github.com/lib/pq"
)

type CapturePlan struct {
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
	SourceSpatialInfo           *datatype.SpatialInfo
	TargetKeys                  []string
}

type PlanResolver interface {
	Resolve(ctx context.Context, task *models.TransferTask) (*CapturePlan, error)
	ResolveForCleanup(ctx context.Context, task *models.TransferTask) (*CapturePlan, error)
}

type PostgreSQLPlanResolver struct {
	engines planner.EngineResolver
}

func NewPostgreSQLPlanResolver(engines planner.EngineResolver) *PostgreSQLPlanResolver {
	return &PostgreSQLPlanResolver{engines: engines}
}

func (r *PostgreSQLPlanResolver) Resolve(ctx context.Context, task *models.TransferTask) (*CapturePlan, error) {
	plan, err := r.resolveBindings(task)
	if err != nil {
		return nil, err
	}
	if err := validateSourceFields(ctx, plan); err != nil {
		return nil, err
	}
	if err := validateSourcePrimaryKey(ctx, plan); err != nil {
		return nil, err
	}
	if err := validateTargetDoesNotExist(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (r *PostgreSQLPlanResolver) ResolveForCleanup(_ context.Context, task *models.TransferTask) (*CapturePlan, error) {
	if task == nil || r == nil || r.engines == nil {
		return nil, fmt.Errorf("PostgreSQL CDC capture cleanup requires task and engine resolver")
	}
	spec, err := planner.ParsePostgreSQLCDCTaskSpec(task.Config)
	if err != nil {
		return nil, err
	}
	sourceRef, err := spec.Source.EngineRef()
	if err != nil {
		return nil, err
	}
	source, err := r.engines.ResolveEngine(sourceRef)
	if err != nil {
		return nil, fmt.Errorf("resolve PostgreSQL CDC source engine for cleanup: %w", err)
	}
	if strings.ToLower(source.Type) != "postgresql" {
		return nil, fmt.Errorf("PostgreSQL CDC source engine is not PostgreSQL")
	}
	sourceLocator, _ := spec.Source.ResourceLocator()
	sourceDatabase := engineplugin.GetString(source.ConnInfo, "database")
	if sourceDatabase == "" {
		return nil, fmt.Errorf("PostgreSQL CDC source database is required")
	}
	return &CapturePlan{
		SourceConnInfo: source.ConnInfo, SourceEngineID: source.EngineID,
		SourceDatabase: sourceDatabase, SourceSchema: sourceLocator.Path[0], SourceTable: sourceLocator.Path[1],
		SourceIdentity: strings.TrimSpace(spec.Source.Locator), SourceConnectionFingerprint: postgresConnectionFingerprint(source.ConnInfo),
	}, nil
}

func (r *PostgreSQLPlanResolver) resolveBindings(task *models.TransferTask) (*CapturePlan, error) {
	if task == nil || r == nil || r.engines == nil {
		return nil, fmt.Errorf("PostgreSQL CDC capture requires task and engine resolver")
	}
	spec, err := planner.ParsePostgreSQLCDCTaskSpec(task.Config)
	if err != nil {
		return nil, err
	}
	sourceRef, err := spec.Source.EngineRef()
	if err != nil {
		return nil, err
	}
	targetRef, err := spec.Target.EngineRef()
	if err != nil {
		return nil, err
	}
	source, err := r.engines.ResolveEngine(sourceRef)
	if err != nil {
		return nil, fmt.Errorf("resolve PostgreSQL CDC source engine: %w", err)
	}
	target, err := r.engines.ResolveEngine(targetRef)
	if err != nil {
		return nil, fmt.Errorf("resolve PostgreSQL CDC target engine: %w", err)
	}
	if strings.ToLower(source.Type) != "postgresql" || strings.ToLower(target.Type) != "postgresql" {
		return nil, fmt.Errorf("PostgreSQL CDC v1 requires PostgreSQL source and target")
	}
	sourceLocator, _ := spec.Source.ResourceLocator()
	targetParent, _ := spec.Target.ParentResourceLocator()
	sourceKeys, targetKeys, err := planner.PostgreSQLCDCSourceToTargetKeys(spec)
	if err != nil {
		return nil, err
	}
	plan := &CapturePlan{
		SourceConnInfo: source.ConnInfo, TargetConnInfo: target.ConnInfo,
		SourceEngineID: source.EngineID, TargetEngineID: target.EngineID,
		SourceDatabase: engineplugin.GetString(source.ConnInfo, "database"),
		SourceSchema:   sourceLocator.Path[0], SourceTable: sourceLocator.Path[1],
		TargetSchema: targetParent.Path[0], TargetTable: strings.TrimSpace(spec.Target.Name),
		SourceIdentity: strings.TrimSpace(spec.Source.Locator), SourceConnectionFingerprint: postgresConnectionFingerprint(source.ConnInfo),
		SourceKeys: sourceKeys, SourceFieldTypes: make(map[string]datatype.FieldType, len(spec.Transforms[0].Fields)), TargetKeys: targetKeys,
	}
	for _, field := range spec.Transforms[0].Fields {
		sourceField := strings.TrimSpace(field.Source)
		plan.SourceFields = append(plan.SourceFields, sourceField)
		plan.SourceFieldTypes[sourceField] = datatype.ParseFieldType(field.TargetType)
	}
	if plan.SourceDatabase == "" {
		return nil, fmt.Errorf("PostgreSQL CDC source database is required")
	}
	return plan, nil
}

func validateSourceFields(ctx context.Context, plan *CapturePlan) error {
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
	if actualType == datatype.FieldTypeUnknown || !planner.PostgreSQLCDCFieldTypeSupported(actualType) {
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

func validateSourcePrimaryKey(ctx context.Context, plan *CapturePlan) error {
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

func buildConnectorConfig(plan *CapturePlan, resource *models.CaptureResource, connectLoopbackHost string) map[string]string {
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
		"slot.name":                      resource.SlotName,
		"publication.name":               resource.PublicationName,
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
	}
}
