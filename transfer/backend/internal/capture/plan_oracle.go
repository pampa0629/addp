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
	"time"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	oracleplugin "github.com/addp/common/engine/plugins/oracle"
	oracletypes "github.com/addp/common/format/mappers/oracle"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

func oracleCDCConnectionInfo(source engineplugin.ConnectionInfo) engineplugin.ConnectionInfo {
	return engineplugin.ConnectionInfo{
		"host":         engineplugin.GetString(source, "host"),
		"port":         engineplugin.GetInt(source, "port"),
		"service_name": engineplugin.GetString(source, "service_name"),
		"user":         engineplugin.GetString(source, "cdc_user"),
		"password":     engineplugin.GetString(source, "cdc_password"),
	}
}

func oracleConnectionFingerprint(connInfo engineplugin.ConnectionInfo) string {
	host := strings.ToLower(strings.TrimSpace(engineplugin.NormalizeHost(engineplugin.GetString(connInfo, "host"))))
	port := engineplugin.GetInt(connInfo, "port")
	if port == 0 {
		port = 1521
	}
	serviceName := strings.TrimSpace(engineplugin.GetString(connInfo, "service_name"))
	identity := host + ":" + strconv.Itoa(port) + "/" + serviceName
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}

func openOracle(connInfo engineplugin.ConnectionInfo) (*sql.DB, error) {
	dsn, err := (&oracleplugin.OraclePlugin{}).BuildDSN(connInfo)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(30 * time.Second)
	return db, nil
}

func validateOracleCaptureSettings(ctx context.Context, plan *CapturePlan) error {
	if plan == nil || strings.TrimSpace(plan.SourceCDBName) == "" || strings.TrimSpace(engineplugin.GetString(plan.CDCConnInfo, "user")) == "" || engineplugin.GetString(plan.CDCConnInfo, "password") == "" {
		return fmt.Errorf("Oracle CDC requires cdc_database_name, cdc_user and cdc_password in the source engine connection")
	}
	db, err := openOracle(plan.CDCConnInfo)
	if err != nil {
		return fmt.Errorf("open Oracle CDC connection: %w", err)
	}
	defer db.Close()
	var logMode, forceLogging, supplementalMin string
	if err := db.QueryRowContext(ctx, `SELECT LOG_MODE, FORCE_LOGGING, SUPPLEMENTAL_LOG_DATA_MIN FROM V$DATABASE`).Scan(&logMode, &forceLogging, &supplementalMin); err != nil {
		return fmt.Errorf("query Oracle CDC database settings: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(logMode), "ARCHIVELOG") || !strings.EqualFold(strings.TrimSpace(forceLogging), "YES") || !strings.EqualFold(strings.TrimSpace(supplementalMin), "YES") {
		return fmt.Errorf("Oracle CDC requires ARCHIVELOG, FORCE_LOGGING=YES and SUPPLEMENTAL_LOG_DATA_MIN=YES (actual log_mode=%q force_logging=%q supplemental_log_data_min=%q)", logMode, forceLogging, supplementalMin)
	}
	var allColumnGroups int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ALL_LOG_GROUPS
		WHERE OWNER = :1 AND TABLE_NAME = :2
		  AND LOG_GROUP_TYPE = 'ALL COLUMN LOGGING' AND ALWAYS = 'ALWAYS'`, plan.SourceSchema, plan.SourceTable).Scan(&allColumnGroups); err != nil {
		return fmt.Errorf("query Oracle CDC table supplemental logging: %w", err)
	}
	if allColumnGroups == 0 {
		return fmt.Errorf("Oracle CDC source table %s.%s requires SUPPLEMENTAL LOG DATA (ALL) COLUMNS", plan.SourceSchema, plan.SourceTable)
	}
	query := `SELECT 1 FROM ` + oracleQuoteIdentifier(plan.SourceSchema) + `.` + oracleQuoteIdentifier(plan.SourceTable) + ` WHERE 1 = 0`
	if rows, err := db.QueryContext(ctx, query); err != nil {
		return fmt.Errorf("Oracle CDC credentials cannot read source table: %w", err)
	} else {
		rows.Close()
	}
	return nil
}

func validateOracleSourceFields(ctx context.Context, plan *CapturePlan) error {
	db, err := openOracle(plan.SourceConnInfo)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME, DATA_TYPE, DATA_PRECISION, DATA_SCALE, DATETIME_PRECISION, NULLABLE
		FROM ALL_TAB_COLUMNS
		WHERE OWNER = :1 AND TABLE_NAME = :2
		ORDER BY COLUMN_ID`, plan.SourceSchema, plan.SourceTable)
	if err != nil {
		return fmt.Errorf("query Oracle CDC source fields: %w", err)
	}
	defer rows.Close()
	actual := make([]string, 0)
	for rows.Next() {
		var name, dataType, nullableText string
		var precision, scale, temporalPrecision sql.NullInt64
		if err := rows.Scan(&name, &dataType, &precision, &scale, &temporalPrecision, &nullableText); err != nil {
			return err
		}
		actual = append(actual, name)
		nativeType := oracleCDCNativeType(dataType, precision, scale)
		if err := validateOracleCDCSourceFieldType(name, nativeType, dataType, temporalPrecision, plan.SourceFieldTypes[name]); err != nil {
			return err
		}
		actualNullable := strings.EqualFold(nullableText, "Y")
		if actualNullable != plan.SourceFieldNullables[name] {
			return fmt.Errorf("Oracle CDC source field %q nullable=%t, but field_mapping declares nullable=%t", name, actualNullable, plan.SourceFieldNullables[name])
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	configured := append([]string(nil), plan.SourceFields...)
	sort.Strings(actual)
	sort.Strings(configured)
	if !reflect.DeepEqual(actual, configured) {
		return fmt.Errorf("Oracle CDC field mapping must cover the complete source schema: actual=%v configured=%v", actual, configured)
	}
	return nil
}

func oracleCDCNativeType(dataType string, precision, scale sql.NullInt64) string {
	normalized := strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(dataType)), " "))
	if (normalized == "NUMBER" || normalized == "DECIMAL" || normalized == "NUMERIC") && precision.Valid {
		valueScale := int64(0)
		if scale.Valid {
			valueScale = scale.Int64
		}
		return fmt.Sprintf("%s(%d,%d)", normalized, precision.Int64, valueScale)
	}
	return normalized
}

func validateOracleCDCSourceFieldType(name, nativeType, dataType string, temporalPrecision sql.NullInt64, configuredType datatype.FieldType) error {
	base := strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(dataType)), " "))
	if strings.Contains(base, "LOB") || base == "LONG" || strings.Contains(base, "RAW") || base == "XMLTYPE" || base == "JSON" || base == "MDSYS.SDO_GEOMETRY" || base == "SDO_GEOMETRY" {
		return fmt.Errorf("Oracle CDC source field %q uses unsupported Oracle type %q", name, nativeType)
	}
	actualType := (&oracletypes.TypeMapper{}).ToCommon(nativeType)
	if actualType == datatype.FieldTypeGeometry || actualType == datatype.FieldTypeBytes || actualType == datatype.FieldTypeUnknown || !planner.ContinuousFieldTypeSupported(actualType) {
		return fmt.Errorf("Oracle CDC source field %q uses unsupported Oracle type %q", name, nativeType)
	}
	if actualType != configuredType {
		return fmt.Errorf("Oracle CDC source field %q type %q maps to %q, but field_mapping declares %q", name, nativeType, actualType, configuredType)
	}
	if strings.HasPrefix(base, "TIMESTAMP") && (!temporalPrecision.Valid || temporalPrecision.Int64 > 3) {
		return fmt.Errorf("Oracle CDC source field %q type %q must declare precision no greater than 3", name, nativeType)
	}
	return nil
}

func validateOracleSourcePrimaryKey(ctx context.Context, plan *CapturePlan) error {
	db, err := openOracle(plan.SourceConnInfo)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT cols.COLUMN_NAME
		FROM ALL_CONSTRAINTS cons
		JOIN ALL_CONS_COLUMNS cols
		  ON cols.OWNER = cons.OWNER AND cols.CONSTRAINT_NAME = cons.CONSTRAINT_NAME
		WHERE cons.OWNER = :1 AND cons.TABLE_NAME = :2 AND cons.CONSTRAINT_TYPE = 'P'
		ORDER BY cols.POSITION`, plan.SourceSchema, plan.SourceTable)
	if err != nil {
		return fmt.Errorf("query Oracle CDC source primary key: %w", err)
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
		return fmt.Errorf("Oracle CDC source table requires a primary key")
	}
	if !reflect.DeepEqual(actual, plan.SourceKeys) {
		return fmt.Errorf("Oracle CDC source primary key %v must map one-to-one to configured target keys via source fields %v", actual, plan.SourceKeys)
	}
	return nil
}

func buildOracleConnectorConfig(plan *CapturePlan, resource *models.CaptureResource, config SupervisorConfig) (map[string]string, error) {
	if resource.Oracle == nil || strings.TrimSpace(resource.Oracle.SchemaHistoryTopicName) == "" {
		return nil, fmt.Errorf("Oracle connector config requires provider resources")
	}
	host := strings.TrimSpace(engineplugin.GetString(plan.CDCConnInfo, "host"))
	port := engineplugin.GetInt(plan.CDCConnInfo, "port")
	if port == 0 {
		port = 1521
	}
	if (host == "localhost" || host == "127.0.0.1" || host == "::1") && strings.TrimSpace(config.ConnectLoopbackHost) != "" {
		host = strings.TrimSpace(config.ConnectLoopbackHost)
	}
	bootstrapServers := strings.TrimSpace(config.ConnectBootstrapServers)
	if bootstrapServers == "" {
		return nil, fmt.Errorf("Oracle connector requires Kafka Connect-visible bootstrap servers")
	}
	connectorConfig := map[string]string{
		"connector.class":             "io.debezium.connector.oracle.OracleConnector",
		"tasks.max":                   "1",
		"database.hostname":           host,
		"database.port":               strconv.Itoa(port),
		"database.user":               engineplugin.GetString(plan.CDCConnInfo, "user"),
		"database.password":           engineplugin.GetString(plan.CDCConnInfo, "password"),
		"database.dbname":             plan.SourceCDBName,
		"database.pdb.name":           plan.SourceDatabase,
		"database.connection.adapter": "LogMiner",
		"topic.prefix":                resource.ConnectorName,
		"table.include.list":          regexp.QuoteMeta(plan.SourceSchema + "." + plan.SourceTable),
		"snapshot.mode":               "initial",
		"log.mining.strategy":         "online_catalog",
		"decimal.handling.mode":       "string",
		"time.precision.mode":         "connect",
		"lob.enabled":                 "false",
		"tombstones.on.delete":        "false",
		"include.schema.changes":      "false",
		"schema.history.internal.kafka.bootstrap.servers":        bootstrapServers,
		"schema.history.internal.kafka.topic":                    resource.Oracle.SchemaHistoryTopicName,
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
	if err := applySchemaHistoryKafkaConfig(connectorConfig, config, "Oracle"); err != nil {
		return nil, err
	}
	return connectorConfig, nil
}

func oracleQuoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
