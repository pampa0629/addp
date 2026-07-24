package continuous

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
)

type ChangeEvent struct {
	Operation string
	Row       map[string]interface{}
}

type SchemaChangeError struct {
	Scope              string
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

func decodeDatabaseDebeziumRecord(record engineplugin.ChangeRecord, plan *planner.ContinuousPlan, validateSource func(json.RawMessage, *planner.DatabaseCDCSourcePlan, string) error) (*ChangeEvent, error) {
	value := bytes.TrimSpace(record.Value)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return nil, fmt.Errorf("Debezium tombstone records are not supported")
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
	if plan.CDC.Provider == "mysql" {
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
	return &ChangeEvent{Operation: operation, Row: row}, nil
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
	if database != expected.Database || schema != expected.Schema || table != expected.Table {
		return fmt.Errorf("Debezium source table identity %s.%s.%s does not match expected %s.%s.%s", database, schema, table, expected.Database, expected.Schema, expected.Table)
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
			converted, err = coercePostgreSQLCDCGeometry(value, mapping.Source, plan.CDC.SpatialInfo)
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
	if datatype.ParseGeometryType(column.GeometryType) != geometryTopology(geometry) {
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
