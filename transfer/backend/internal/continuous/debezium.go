package continuous

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/addp/transfer/internal/planner"
	"github.com/twpayne/go-geom"
)

const (
	changeEventOperationSnapshot = "snapshot"
	changeEventOperationUpsert   = "upsert"
	changeEventOperationDelete   = "delete"
	changeEventOperationSkip     = "skip"
)

type ChangeEvent struct {
	Operation        string
	Row              map[string]interface{}
	SnapshotComplete bool
}

type debeziumSnapshotNotification struct {
	Type      string
	Connector string
	Completed bool
}

type SchemaChangeError struct {
	Scope              string
	SourcePartition    string
	SourceOffset       int64
	MissingFields      []string
	UnexpectedFields   []string
	IncompatibleFields []string
	Details            map[string]string
}

func (e *SchemaChangeError) Error() string {
	message := fmt.Sprintf("schema change blocked for %s: missing=%v unexpected=%v incompatible=%v", e.Scope, e.MissingFields, e.UnexpectedFields, e.IncompatibleFields)
	if len(e.Details) > 0 {
		message += fmt.Sprintf(" details=%v", e.Details)
	}
	return message
}

func decodePostgreSQLDebeziumRecord(record engineplugin.ChangeRecord, plan *planner.ContinuousPlan) (*ChangeEvent, error) {
	if plan == nil || plan.CDC == nil || plan.CDC.Provider != "postgresql" || plan.Envelope != planner.ContinuousEnvelopePostgreSQLDebezium {
		return nil, fmt.Errorf("PostgreSQL Debezium adapter requires a CDC continuous plan")
	}
	return decodeDatabaseDebeziumRecord(record, plan, validatePostgreSQLDebeziumSource)
}

func decodeMySQLDebeziumRecord(record engineplugin.ChangeRecord, plan *planner.ContinuousPlan) (*ChangeEvent, error) {
	if plan == nil || plan.CDC == nil || plan.CDC.Provider != "mysql" || plan.Envelope != planner.ContinuousEnvelopeMySQLDebezium {
		return nil, fmt.Errorf("MySQL Debezium adapter requires a CDC continuous plan")
	}
	return decodeDatabaseDebeziumRecord(record, plan, validateMySQLDebeziumSource)
}

func decodeOracleDebeziumRecord(record engineplugin.ChangeRecord, plan *planner.ContinuousPlan) (*ChangeEvent, error) {
	if plan == nil || plan.CDC == nil || plan.CDC.Provider != "oracle" || plan.Envelope != planner.ContinuousEnvelopeOracleDebezium {
		return nil, fmt.Errorf("Oracle Debezium adapter requires a CDC continuous plan")
	}
	return decodeDatabaseDebeziumRecord(record, plan, validateOracleDebeziumSource)
}

func decodeDatabaseDebeziumRecord(record engineplugin.ChangeRecord, plan *planner.ContinuousPlan, validateSource func(json.RawMessage, *planner.DatabaseCDCSourcePlan, string) error) (*ChangeEvent, error) {
	value := bytes.TrimSpace(record.Value)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return nil, fmt.Errorf("Debezium tombstone records are not supported")
	}
	if notification, ok, err := decodeDebeziumSnapshotNotification(value, plan); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return &ChangeEvent{Operation: changeEventOperationSkip, SnapshotComplete: notification.Completed}, nil
	}
	envelope, err := decodeRawJSONObject(value, "Debezium value")
	if err != nil {
		return nil, err
	}
	if err := validateObjectFields(envelope,
		[]string{"before", "after", "source", "op"},
		[]string{"before", "after", "source", "op", "ts_ms", "ts_us", "ts_ns", "transaction"},
		"Debezium envelope"); err != nil {
		return nil, err
	}
	var op string
	if err := json.Unmarshal(envelope["op"], &op); err != nil || strings.TrimSpace(op) == "" {
		return nil, incompatibleSchemaField("Debezium envelope", "op")
	}
	if op == "t" || op == "m" {
		return nil, fmt.Errorf("unsupported Debezium operation %q", op)
	}
	if op != "r" && op != "c" && op != "u" && op != "d" {
		return nil, fmt.Errorf("unsupported Debezium operation %q", op)
	}
	if err := validateSource(envelope["source"], plan.CDC, op); err != nil {
		return nil, err
	}
	keyRow, err := decodeAndMapDebeziumKey(record.Key, plan)
	if err != nil {
		return nil, err
	}
	if plan.CDC.Provider == "mysql" || plan.CDC.Provider == "oracle" {
		before, err := decodeNullableJSONObject(envelope["before"], "Debezium before")
		if err != nil {
			return nil, err
		}
		if op == "r" || op == "c" {
			if before != nil {
				return nil, fmt.Errorf("MySQL Debezium operation %q requires null before", op)
			}
		} else {
			if before == nil {
				return nil, fmt.Errorf("MySQL Debezium operation %q requires full before object", op)
			}
			beforeRow, err := mapCDCSourceRow(before, plan)
			if plan.CDC.Provider == "oracle" && plan.CDC.SpatialInfo != nil {
				beforeRow, err = mapCDCSourceKeyRow(before, plan)
			}
			if err != nil {
				return nil, err
			}
			for _, key := range plan.Target.Keys {
				if !reflect.DeepEqual(beforeRow[key], keyRow[key]) {
					return nil, fmt.Errorf("Debezium record key does not match before field %q", key)
				}
			}
		}
	}
	if op == "d" {
		after, err := decodeNullableJSONObject(envelope["after"], "Debezium after")
		if err != nil {
			return nil, err
		}
		if after != nil {
			return nil, fmt.Errorf("Debezium delete event after must be null")
		}
		return &ChangeEvent{Operation: changeEventOperationDelete, Row: keyRow}, nil
	}
	after, err := decodeNullableJSONObject(envelope["after"], "Debezium after")
	if err != nil {
		return nil, err
	}
	if after == nil {
		return nil, fmt.Errorf("Debezium operation %q requires a non-null after object", op)
	}
	row, err := mapCDCSourceRow(after, plan)
	if err != nil {
		return nil, err
	}
	for _, key := range plan.Target.Keys {
		if !reflect.DeepEqual(row[key], keyRow[key]) {
			return nil, fmt.Errorf("Debezium record key does not match after field %q", key)
		}
	}
	operation := changeEventOperationUpsert
	if op == "r" {
		operation = changeEventOperationSnapshot
	}
	return &ChangeEvent{
		Operation: operation,
		Row:       row,
	}, nil
}

func decodeDebeziumSnapshotNotification(value []byte, plan *planner.ContinuousPlan) (*debeziumSnapshotNotification, bool, error) {
	object, err := decodeRawJSONObject(value, "Debezium value")
	if err != nil {
		return nil, false, nil
	}
	markers := []string{"id", "type", "aggregate_type", "additional_data", "timestamp"}
	looksLikeNotification := false
	for _, field := range markers {
		if _, ok := object[field]; ok {
			looksLikeNotification = true
			break
		}
	}
	if !looksLikeNotification {
		return nil, false, nil
	}
	if err := validateObjectFields(object, markers, markers, "Debezium snapshot notification"); err != nil {
		return nil, true, err
	}
	var id, notificationType, aggregateType string
	if err := json.Unmarshal(object["id"], &id); err != nil || strings.TrimSpace(id) == "" {
		return nil, true, incompatibleSchemaField("Debezium snapshot notification", "id")
	}
	if err := json.Unmarshal(object["type"], &notificationType); err != nil {
		return nil, true, incompatibleSchemaField("Debezium snapshot notification", "type")
	}
	if notificationType != "STARTED" && notificationType != "IN_PROGRESS" && notificationType != "TABLE_SCAN_COMPLETED" && notificationType != "COMPLETED" {
		return nil, true, fmt.Errorf("unsupported Debezium snapshot notification type %q", notificationType)
	}
	if err := json.Unmarshal(object["aggregate_type"], &aggregateType); err != nil || aggregateType != "Initial Snapshot" {
		return nil, true, incompatibleSchemaField("Debezium snapshot notification", "aggregate_type")
	}
	var timestamp json.Number
	if err := json.Unmarshal(object["timestamp"], &timestamp); err != nil {
		return nil, true, incompatibleSchemaField("Debezium snapshot notification", "timestamp")
	}
	if _, err := timestamp.Int64(); err != nil || strings.HasPrefix(timestamp.String(), "-") {
		return nil, true, incompatibleSchemaField("Debezium snapshot notification", "timestamp")
	}
	additional, err := decodeRawJSONObject(object["additional_data"], "Debezium snapshot notification additional_data")
	if err != nil {
		return nil, true, err
	}
	if err := validateObjectFields(additional, []string{"connector_name"}, []string{"connector_name"}, "Debezium snapshot notification additional_data"); err != nil {
		return nil, true, err
	}
	var connectorName string
	if err := json.Unmarshal(additional["connector_name"], &connectorName); err != nil || strings.TrimSpace(connectorName) == "" {
		return nil, true, incompatibleSchemaField("Debezium snapshot notification additional_data", "connector_name")
	}
	if plan == nil || plan.CDC == nil || strings.TrimSpace(plan.CDC.ConnectorName) == "" {
		return nil, true, fmt.Errorf("Debezium snapshot notification requires expected connector name")
	}
	if connectorName != plan.CDC.ConnectorName {
		return nil, true, fmt.Errorf("Debezium snapshot notification connector %q does not match expected %q", connectorName, plan.CDC.ConnectorName)
	}
	return &debeziumSnapshotNotification{Type: notificationType, Connector: connectorName, Completed: notificationType == "COMPLETED"}, true, nil
}

func validatePostgreSQLDebeziumSource(raw json.RawMessage, expected *planner.DatabaseCDCSourcePlan, _ string) error {
	source, err := decodeRawJSONObject(raw, "Debezium source")
	if err != nil {
		return incompatibleSchemaField("Debezium envelope", "source")
	}
	allowed := []string{"version", "connector", "name", "ts_ms", "snapshot", "db", "sequence", "ts_us", "ts_ns", "schema", "table", "txId", "lsn", "xmin", "origin", "origin_lsn"}
	if err := validateObjectFields(source, []string{"connector", "db", "schema", "table"}, allowed, "Debezium source"); err != nil {
		return err
	}
	var connector, database, schema, table string
	if err := json.Unmarshal(source["connector"], &connector); err != nil {
		return incompatibleSchemaField("Debezium source", "connector")
	}
	if connector != "postgresql" {
		return fmt.Errorf("Debezium source connector must be postgresql")
	}
	if err := json.Unmarshal(source["db"], &database); err != nil {
		return incompatibleSchemaField("Debezium source", "db")
	}
	if err := json.Unmarshal(source["schema"], &schema); err != nil {
		return incompatibleSchemaField("Debezium source", "schema")
	}
	if err := json.Unmarshal(source["table"], &table); err != nil {
		return incompatibleSchemaField("Debezium source", "table")
	}
	expectedTable := expected.Table
	if strings.TrimSpace(expected.CaptureTable) != "" {
		expectedTable = expected.CaptureTable
	}
	if database != expected.Database || schema != expected.Schema || table != expectedTable {
		return fmt.Errorf("Debezium source table identity %s.%s.%s does not match expected %s.%s.%s", database, schema, table, expected.Database, expected.Schema, expectedTable)
	}
	return nil
}

func validateMySQLDebeziumSource(raw json.RawMessage, expected *planner.DatabaseCDCSourcePlan, op string) error {
	source, err := decodeRawJSONObject(raw, "Debezium source")
	if err != nil {
		return incompatibleSchemaField("Debezium envelope", "source")
	}
	allowed := []string{"version", "connector", "name", "ts_ms", "snapshot", "db", "sequence", "ts_us", "ts_ns", "table", "server_id", "gtid", "file", "pos", "row", "thread", "query"}
	if err := validateObjectFields(source, []string{"connector", "snapshot", "db", "table", "server_id", "file", "pos", "row"}, allowed, "Debezium source"); err != nil {
		return err
	}
	var connector, database, table, file string
	if err := json.Unmarshal(source["connector"], &connector); err != nil || connector != "mysql" {
		return incompatibleSchemaField("Debezium source", "connector")
	}
	if err := json.Unmarshal(source["db"], &database); err != nil {
		return incompatibleSchemaField("Debezium source", "db")
	}
	if err := json.Unmarshal(source["table"], &table); err != nil {
		return incompatibleSchemaField("Debezium source", "table")
	}
	if err := json.Unmarshal(source["file"], &file); err != nil || strings.TrimSpace(file) == "" {
		return incompatibleSchemaField("Debezium source", "file")
	}
	numbers := make(map[string]int64, 3)
	for _, field := range []string{"server_id", "pos", "row"} {
		var number json.Number
		if err := json.Unmarshal(source[field], &number); err != nil {
			return incompatibleSchemaField("Debezium source", field)
		}
		value, err := number.Int64()
		if err != nil || value < 0 {
			return incompatibleSchemaField("Debezium source", field)
		}
		numbers[field] = value
	}
	var snapshot string
	if err := json.Unmarshal(source["snapshot"], &snapshot); err != nil {
		return incompatibleSchemaField("Debezium source", "snapshot")
	}
	if op == "r" {
		if numbers["server_id"] != 0 || (snapshot != "true" && snapshot != "last") {
			return incompatibleSchemaField("Debezium source", "snapshot")
		}
	} else if numbers["server_id"] == 0 || snapshot != "false" {
		return incompatibleSchemaField("Debezium source", "server_id")
	}
	if database != expected.Database || table != expected.Table {
		return fmt.Errorf("Debezium source table identity %s.%s does not match expected %s.%s", database, table, expected.Database, expected.Table)
	}
	return nil
}

func validateOracleDebeziumSource(raw json.RawMessage, expected *planner.DatabaseCDCSourcePlan, op string) error {
	source, err := decodeRawJSONObject(raw, "Debezium source")
	if err != nil {
		return incompatibleSchemaField("Debezium envelope", "source")
	}
	allowed := []string{
		"version", "connector", "name", "ts_ms", "snapshot", "db", "sequence", "ts_us", "ts_ns", "schema", "table",
		"txId", "scn", "commit_scn", "lcr_position", "rs_id", "ssn", "redo_thread", "user_name", "redo_sql", "row_id",
		"commit_ts_ms", "start_scn", "start_ts_ms", "txSeq",
	}
	if err := validateObjectFields(source, []string{"connector", "snapshot", "db", "schema", "table", "scn"}, allowed, "Debezium source"); err != nil {
		return err
	}
	var connector, snapshot, database, schema, table, scn string
	for field, target := range map[string]*string{
		"connector": &connector, "snapshot": &snapshot, "db": &database,
		"schema": &schema, "table": &table, "scn": &scn,
	} {
		if err := json.Unmarshal(source[field], target); err != nil {
			return incompatibleSchemaField("Debezium source", field)
		}
	}
	if connector != "oracle" {
		return incompatibleSchemaField("Debezium source", "connector")
	}
	if _, ok := new(big.Int).SetString(scn, 10); !ok || strings.HasPrefix(scn, "-") {
		return incompatibleSchemaField("Debezium source", "scn")
	}
	for _, field := range []string{"commit_scn", "start_scn"} {
		if err := validateOptionalOracleSCN(source, field); err != nil {
			return err
		}
	}
	for _, field := range []string{"ts_ms", "ts_us", "ts_ns", "ssn", "redo_thread", "commit_ts_ms", "start_ts_ms", "txSeq"} {
		if err := validateOptionalNonNegativeInteger(source, field); err != nil {
			return err
		}
	}
	for _, field := range []string{"txId", "lcr_position", "rs_id", "user_name", "redo_sql", "row_id"} {
		if err := validateOptionalString(source, field); err != nil {
			return err
		}
	}
	if op == "r" {
		if snapshot != "first" && snapshot != "last" && snapshot != "true" {
			return incompatibleSchemaField("Debezium source", "snapshot")
		}
	} else if snapshot != "false" {
		return incompatibleSchemaField("Debezium source", "snapshot")
	}
	expectedTable := expected.Table
	if strings.TrimSpace(expected.CaptureTable) != "" {
		expectedTable = expected.CaptureTable
	}
	if database != expected.Database || schema != expected.Schema || table != expectedTable {
		return fmt.Errorf("Debezium source table identity %s.%s.%s does not match expected %s.%s.%s", database, schema, table, expected.Database, expected.Schema, expectedTable)
	}
	return nil
}

func validateOptionalOracleSCN(source map[string]json.RawMessage, field string) error {
	raw, ok := source[field]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return incompatibleSchemaField("Debezium source", field)
	}
	if _, ok := new(big.Int).SetString(value, 10); !ok || strings.HasPrefix(value, "-") {
		return incompatibleSchemaField("Debezium source", field)
	}
	return nil
}

func validateOptionalNonNegativeInteger(source map[string]json.RawMessage, field string) error {
	raw, ok := source[field]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded interface{}
	if err := decoder.Decode(&decoded); err != nil {
		return incompatibleSchemaField("Debezium source", field)
	}
	number, ok := decoded.(json.Number)
	if !ok {
		return incompatibleSchemaField("Debezium source", field)
	}
	value, err := number.Int64()
	if err != nil || value < 0 {
		return incompatibleSchemaField("Debezium source", field)
	}
	return nil
}

func validateOptionalString(source map[string]json.RawMessage, field string) error {
	raw, ok := source[field]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return incompatibleSchemaField("Debezium source", field)
	}
	return nil
}

func decodeAndMapDebeziumKey(raw []byte, plan *planner.ContinuousPlan) (map[string]interface{}, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("Debezium record key is required")
	}
	source, err := decodeJSONObject(raw, "Debezium record key")
	if err != nil {
		return nil, err
	}
	expected := make(map[string]bool, len(plan.SourceKeys))
	for _, key := range plan.SourceKeys {
		expected[key] = true
	}
	missing, unexpected := objectFieldDiff(source, expected)
	if len(missing) > 0 || len(unexpected) > 0 {
		return nil, &SchemaChangeError{Scope: "Debezium record key", MissingFields: missing, UnexpectedFields: unexpected}
	}
	mappings := make(map[string]planner.ContinuousFieldPlan, len(plan.Mappings))
	for _, mapping := range plan.Mappings {
		mappings[mapping.Source] = mapping
	}
	row := make(map[string]interface{}, len(plan.SourceKeys))
	for _, sourceKey := range plan.SourceKeys {
		mapping, ok := mappings[sourceKey]
		if !ok {
			return nil, fmt.Errorf("Debezium source key %q is not mapped", sourceKey)
		}
		value := source[sourceKey]
		if value == nil {
			return nil, incompatibleSchemaField("Debezium record key", sourceKey)
		}
		converted, err := coerceDatabaseCDCValue(value, mapping, plan)
		if err != nil {
			return nil, incompatibleSchemaField("Debezium record key", sourceKey)
		}
		row[mapping.Target] = converted
	}
	return row, nil
}

func mapCDCSourceRow(source map[string]interface{}, plan *planner.ContinuousPlan) (map[string]interface{}, error) {
	expected := make(map[string]bool, len(plan.Mappings))
	for _, mapping := range plan.Mappings {
		expected[mapping.Source] = true
	}
	missing, unexpected := objectFieldDiff(source, expected)
	if len(missing) > 0 || len(unexpected) > 0 {
		return nil, &SchemaChangeError{Scope: "Debezium after", MissingFields: missing, UnexpectedFields: unexpected}
	}
	row := make(map[string]interface{}, len(plan.Mappings))
	for _, mapping := range plan.Mappings {
		value := source[mapping.Source]
		if value == nil {
			if !mapping.Nullable {
				return nil, incompatibleSchemaField("Debezium after", mapping.Source)
			}
			row[mapping.Target] = nil
			continue
		}
		var converted interface{}
		var err error
		if mapping.Type == datatype.FieldTypeGeometry {
			converted, err = coerceDatabaseCDCGeometry(value, mapping.Source, plan)
		} else {
			converted, err = coerceDatabaseCDCValue(value, mapping, plan)
		}
		if err != nil {
			schemaErr := incompatibleSchemaField("Debezium after", mapping.Source)
			schemaErr.Details = map[string]string{mapping.Source: err.Error()}
			return nil, schemaErr
		}
		row[mapping.Target] = converted
	}
	return row, nil
}

func mapCDCSourceKeyRow(source map[string]interface{}, plan *planner.ContinuousPlan) (map[string]interface{}, error) {
	expected := make(map[string]bool, len(plan.Mappings))
	mappings := make(map[string]planner.ContinuousFieldPlan, len(plan.Mappings))
	for _, mapping := range plan.Mappings {
		expected[mapping.Source] = true
		mappings[mapping.Source] = mapping
	}
	missing, unexpected := objectFieldDiff(source, expected)
	if len(missing) > 0 || len(unexpected) > 0 {
		return nil, &SchemaChangeError{Scope: "Debezium before", MissingFields: missing, UnexpectedFields: unexpected}
	}
	row := make(map[string]interface{}, len(plan.SourceKeys))
	for _, sourceKey := range plan.SourceKeys {
		mapping, ok := mappings[sourceKey]
		if !ok || source[sourceKey] == nil {
			return nil, incompatibleSchemaField("Debezium before", sourceKey)
		}
		converted, err := coerceDatabaseCDCValue(source[sourceKey], mapping, plan)
		if err != nil {
			return nil, incompatibleSchemaField("Debezium before", sourceKey)
		}
		row[mapping.Target] = converted
	}
	return row, nil
}

func coerceDatabaseCDCValue(value interface{}, mapping planner.ContinuousFieldPlan, plan *planner.ContinuousPlan) (interface{}, error) {
	if mapping.Type == datatype.FieldTypeBytes {
		if plan == nil || plan.CDC == nil || plan.CDC.Provider != "mysql" {
			return nil, fmt.Errorf("bytes CDC values are only supported by MySQL v1")
		}
		encoded, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("MySQL CDC binary field %q must be base64 text", mapping.Source)
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("MySQL CDC binary field %q is not valid base64", mapping.Source)
		}
		return decoded, nil
	}
	if plan != nil && plan.CDC != nil && plan.CDC.Provider == "oracle" {
		if text, ok := value.(string); ok {
			switch mapping.Type {
			case datatype.FieldTypeInt, datatype.FieldTypeBigInt, datatype.FieldTypeFloat, datatype.FieldTypeDouble:
				if strings.TrimSpace(text) == "" {
					return nil, fmt.Errorf("Oracle CDC numeric field %q is empty", mapping.Source)
				}
				return coerceContinuousValue(json.Number(text), mapping.Type)
			}
		}
	}
	return coerceContinuousValue(value, mapping.Type)
}

func coercePostgreSQLCDCGeometry(value interface{}, fieldName string, spatialInfo *datatype.SpatialInfo) ([]byte, error) {
	geometryValue, ok := value.(map[string]interface{})
	if !ok || len(geometryValue) != 2 {
		return nil, fmt.Errorf("PostgreSQL CDC geometry %q must contain exactly wkb and srid", fieldName)
	}
	wkbValue, hasWKB := geometryValue["wkb"]
	sridValue, hasSRID := geometryValue["srid"]
	if !hasWKB || !hasSRID {
		return nil, fmt.Errorf("PostgreSQL CDC geometry %q must contain wkb and srid", fieldName)
	}
	wkbBase64, ok := wkbValue.(string)
	if !ok || strings.TrimSpace(wkbBase64) == "" {
		return nil, fmt.Errorf("PostgreSQL CDC geometry %q wkb must be base64 text", fieldName)
	}
	sridNumber, ok := sridValue.(json.Number)
	if !ok {
		return nil, fmt.Errorf("PostgreSQL CDC geometry %q srid must be an integer", fieldName)
	}
	srid64, err := sridNumber.Int64()
	if err != nil || srid64 <= 0 || int64(int(srid64)) != srid64 {
		return nil, fmt.Errorf("PostgreSQL CDC geometry %q srid is invalid", fieldName)
	}
	column := spatialGeometryColumn(spatialInfo, fieldName)
	if column == nil || column.SRID == nil || column.Dimension == nil || *column.SRID <= 0 {
		return nil, fmt.Errorf("PostgreSQL CDC geometry %q has no frozen spatial facts", fieldName)
	}
	if int(srid64) != *column.SRID {
		return nil, fmt.Errorf("PostgreSQL CDC geometry %q srid changed from %d to %d", fieldName, *column.SRID, srid64)
	}
	wkbValueBytes, err := base64.StdEncoding.Strict().DecodeString(wkbBase64)
	if err != nil || len(wkbValueBytes) == 0 {
		return nil, fmt.Errorf("PostgreSQL CDC geometry %q wkb is not valid base64", fieldName)
	}
	geometry, err := commonSpatial.ParseGeometryBytes(wkbValueBytes)
	if err != nil {
		return nil, fmt.Errorf("decode PostgreSQL CDC geometry %q: %w", fieldName, err)
	}
	if geometry.SRID() > 0 && geometry.SRID() != *column.SRID {
		return nil, fmt.Errorf("PostgreSQL CDC geometry %q embedded SRID changed from %d to %d", fieldName, *column.SRID, geometry.SRID())
	}
	expectedTopology := datatype.ParseGeometryType(column.GeometryType)
	if expectedTopology != datatype.GeometryTypeGeometry && expectedTopology != geometryTopology(geometry) {
		return nil, fmt.Errorf("PostgreSQL CDC geometry %q topology changed", fieldName)
	}
	if !geometryLayoutMatchesDimension(geometry.Layout(), *column.Dimension) {
		return nil, fmt.Errorf("PostgreSQL CDC geometry %q dimension changed", fieldName)
	}
	ewkb, err := commonSpatial.GeomToEWKB(geometry, *column.SRID)
	if err != nil {
		return nil, fmt.Errorf("encode PostgreSQL CDC geometry %q as EWKB: %w", fieldName, err)
	}
	return ewkb, nil
}

func coerceDatabaseCDCGeometry(value interface{}, fieldName string, plan *planner.ContinuousPlan) ([]byte, error) {
	if plan == nil || plan.CDC == nil {
		return nil, fmt.Errorf("database CDC geometry %q requires a CDC plan", fieldName)
	}
	if plan.CDC.Provider == "postgresql" {
		return coercePostgreSQLCDCGeometry(value, fieldName, plan.CDC.SpatialInfo)
	}
	if plan.CDC.Provider != "oracle" {
		return nil, fmt.Errorf("%s CDC does not support geometry field %q", plan.CDC.Provider, fieldName)
	}
	var geometry geom.T
	var err error
	switch encoded := value.(type) {
	case string:
		encoded = strings.TrimSpace(encoded)
		if encoded == "" {
			return nil, fmt.Errorf("Oracle Spatial CDC geometry %q is empty", fieldName)
		}
		if strings.HasPrefix(encoded, "{") {
			geometry, err = commonSpatial.DecodeGeometryValue(encoded, string(commonSpatial.GeometryEncodingGeoJSON), 0)
		} else {
			var wkb []byte
			wkb, err = base64.StdEncoding.Strict().DecodeString(encoded)
			if err == nil && len(wkb) > 0 {
				geometry, err = commonSpatial.ParseGeometryBytes(wkb)
			}
		}
	case []byte:
		if len(strings.TrimSpace(string(encoded))) > 0 && strings.HasPrefix(strings.TrimSpace(string(encoded)), "{") {
			geometry, err = commonSpatial.DecodeGeometryValue(encoded, string(commonSpatial.GeometryEncodingGeoJSON), 0)
		} else {
			geometry, err = commonSpatial.ParseGeometryBytes(encoded)
		}
	case map[string]interface{}:
		geometry, err = commonSpatial.DecodeGeometryValue(encoded, string(commonSpatial.GeometryEncodingGeoJSON), 0)
	default:
		err = fmt.Errorf("unsupported Oracle Spatial CDC geometry value type %T", value)
	}
	if err != nil {
		return nil, fmt.Errorf("decode Oracle Spatial CDC geometry %q: %w", fieldName, err)
	}
	if geometry == nil {
		return nil, fmt.Errorf("decode Oracle Spatial CDC geometry %q: empty geometry", fieldName)
	}
	column := spatialGeometryColumn(plan.CDC.SpatialInfo, fieldName)
	if column == nil || column.SRID == nil || column.Dimension == nil || *column.SRID <= 0 {
		return nil, fmt.Errorf("Oracle Spatial CDC geometry %q has no frozen spatial facts", fieldName)
	}
	if geometry.SRID() > 0 && geometry.SRID() != *column.SRID {
		return nil, fmt.Errorf("Oracle Spatial CDC geometry %q embedded SRID changed from %d to %d", fieldName, *column.SRID, geometry.SRID())
	}
	expectedTopology := datatype.ParseGeometryType(column.GeometryType)
	if expectedTopology == datatype.GeometryTypeUnknown ||
		(expectedTopology != datatype.GeometryTypeGeometry && expectedTopology != geometryTopology(geometry)) {
		return nil, fmt.Errorf("Oracle Spatial CDC geometry %q topology changed", fieldName)
	}
	if !geometryLayoutMatchesDimension(geometry.Layout(), *column.Dimension) {
		return nil, fmt.Errorf("Oracle Spatial CDC geometry %q dimension changed", fieldName)
	}
	ewkb, err := commonSpatial.GeomToEWKB(geometry, *column.SRID)
	if err != nil {
		return nil, fmt.Errorf("encode Oracle Spatial CDC geometry %q as EWKB: %w", fieldName, err)
	}
	return ewkb, nil
}

func spatialGeometryColumn(info *datatype.SpatialInfo, fieldName string) *datatype.GeometryColumnInfo {
	if info == nil {
		return nil
	}
	for i := range info.GeometryColumns {
		if info.GeometryColumns[i].Name == fieldName {
			return &info.GeometryColumns[i]
		}
	}
	return nil
}

func geometryTopology(geometry geom.T) datatype.GeometryType {
	switch geometry.(type) {
	case *geom.Point:
		return datatype.GeometryTypePoint
	case *geom.MultiPoint:
		return datatype.GeometryTypeMultiPoint
	case *geom.LineString:
		return datatype.GeometryTypeLineString
	case *geom.MultiLineString:
		return datatype.GeometryTypeMultiLineString
	case *geom.Polygon:
		return datatype.GeometryTypePolygon
	case *geom.MultiPolygon:
		return datatype.GeometryTypeMultiPolygon
	case *geom.GeometryCollection:
		return datatype.GeometryTypeGeometryCollection
	default:
		return datatype.GeometryTypeUnknown
	}
}

func geometryLayoutMatchesDimension(layout geom.Layout, dimension int) bool {
	switch dimension {
	case 2:
		return layout == geom.XY
	case 3:
		return layout == geom.XYZ
	default:
		return false
	}
}

func decodeJSONObject(raw []byte, label string) (map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]interface{}
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, fmt.Errorf("%s must be a JSON object: %w", label, err)
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s must contain exactly one JSON object", label)
	}
	return value, nil
}

func decodeRawJSONObject(raw []byte, label string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value map[string]json.RawMessage
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, fmt.Errorf("%s must be a JSON object: %w", label, err)
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s must contain exactly one JSON object", label)
	}
	return value, nil
}

func decodeNullableJSONObject(raw json.RawMessage, label string) (map[string]interface{}, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	return decodeJSONObject(trimmed, label)
}

func validateObjectFields(value map[string]json.RawMessage, required, allowed []string, label string) error {
	allowedSet := make(map[string]bool, len(allowed))
	for _, field := range allowed {
		allowedSet[field] = true
	}
	missing := make([]string, 0)
	for _, field := range required {
		if _, ok := value[field]; !ok {
			missing = append(missing, field)
		}
	}
	unexpected := make([]string, 0)
	for field := range value {
		if !allowedSet[field] {
			unexpected = append(unexpected, field)
		}
	}
	if len(missing) > 0 || len(unexpected) > 0 {
		sort.Strings(missing)
		sort.Strings(unexpected)
		return &SchemaChangeError{Scope: label, MissingFields: missing, UnexpectedFields: unexpected}
	}
	return nil
}

func incompatibleSchemaField(scope, field string) *SchemaChangeError {
	return &SchemaChangeError{Scope: scope, IncompatibleFields: []string{field}}
}

func objectFieldDiff(source map[string]interface{}, expected map[string]bool) ([]string, []string) {
	missing := make([]string, 0)
	unexpected := make([]string, 0)
	for field := range expected {
		if _, ok := source[field]; !ok {
			missing = append(missing, field)
		}
	}
	for field := range source {
		if !expected[field] {
			unexpected = append(unexpected, field)
		}
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	return missing, unexpected
}
