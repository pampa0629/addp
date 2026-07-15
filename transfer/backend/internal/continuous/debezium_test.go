package continuous

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/addp/transfer/internal/planner"
	"github.com/twpayne/go-geom"
)

func TestDecodePostgreSQLDebeziumRecordNormalizesSnapshotUpsertAndDelete(t *testing.T) {
	plan := postgresqlCDCAdapterPlan()
	tests := []struct {
		name      string
		key       string
		value     string
		operation string
		wantName  interface{}
	}{
		{
			name: "snapshot", key: `{"id":1}`,
			value:     debeziumEnvelope("r", `null`, `{"id":1,"name":"snapshot"}`, "business", "public", "orders"),
			operation: changeEventOperationSnapshot, wantName: "snapshot",
		},
		{
			name: "upsert", key: `{"id":1}`,
			value:     debeziumEnvelope("u", `{"id":1,"name":"old"}`, `{"id":1,"name":"updated"}`, "business", "public", "orders"),
			operation: changeEventOperationUpsert, wantName: "updated",
		},
		{
			name: "delete", key: `{"id":1}`,
			value:     debeziumEnvelope("d", `{"id":1,"name":"deleted"}`, `null`, "business", "public", "orders"),
			operation: changeEventOperationDelete, wantName: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event, err := decodePostgreSQLDebeziumRecord(plugin.ChangeRecord{Key: []byte(test.key), Value: []byte(test.value)}, plan)
			if err != nil {
				t.Fatalf("decodePostgreSQLDebeziumRecord() error = %v", err)
			}
			if event.Operation != test.operation || event.Row["id"] != int64(1) || event.Row["name"] != test.wantName {
				t.Fatalf("event = %#v", event)
			}
		})
	}
}

func TestDecodePostgreSQLDebeziumRecordRejectsProtocolEventsWithoutAdvancing(t *testing.T) {
	plan := postgresqlCDCAdapterPlan()
	tests := []struct {
		name  string
		key   []byte
		value []byte
		want  string
	}{
		{name: "tombstone", key: []byte(`{"id":1}`), value: nil, want: "tombstone"},
		{name: "truncate", key: []byte(`{"id":1}`), value: []byte(debeziumEnvelope("t", `null`, `null`, "business", "public", "orders")), want: "unsupported Debezium operation"},
		{name: "message", key: []byte(`{"id":1}`), value: []byte(debeziumEnvelope("m", `null`, `null`, "business", "public", "orders")), want: "unsupported Debezium operation"},
		{name: "source mismatch", key: []byte(`{"id":1}`), value: []byte(debeziumEnvelope("c", `null`, `{"id":1,"name":"bad"}`, "business", "other", "orders")), want: "source table identity"},
		{name: "key mismatch", key: []byte(`{"id":2}`), value: []byte(debeziumEnvelope("c", `null`, `{"id":1,"name":"bad"}`, "business", "public", "orders")), want: "record key does not match"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodePostgreSQLDebeziumRecord(plugin.ChangeRecord{Key: test.key, Value: test.value}, plan); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDecodePostgreSQLDebeziumRecordReportsSchemaDiff(t *testing.T) {
	plan := postgresqlCDCAdapterPlan()
	value := debeziumEnvelope("c", `null`, `{"id":1,"name":"ok","extra":true}`, "business", "public", "orders")
	_, err := decodePostgreSQLDebeziumRecord(plugin.ChangeRecord{Key: []byte(`{"id":1}`), Value: []byte(value)}, plan)
	var schemaErr *SchemaChangeError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("error = %v, want SchemaChangeError", err)
	}
	if len(schemaErr.UnexpectedFields) != 1 || schemaErr.UnexpectedFields[0] != "extra" {
		t.Fatalf("schema diff = %#v", schemaErr)
	}
}

func TestDecodePostgreSQLDebeziumRecordReportsEnvelopeAndSourceSchemaDiff(t *testing.T) {
	plan := postgresqlCDCAdapterPlan()
	tests := []struct {
		name       string
		value      string
		scope      string
		missing    string
		unexpected string
	}{
		{
			name:  "missing envelope source",
			value: `{"before":null,"after":{"id":1,"name":"ok"},"op":"c","ts_ms":1,"ts_us":1000,"ts_ns":1000000,"transaction":null}`,
			scope: "Debezium envelope", missing: "source",
		},
		{
			name:  "unknown envelope field",
			value: strings.TrimSuffix(debeziumEnvelope("c", `null`, `{"id":1,"name":"ok"}`, "business", "public", "orders"), "}") + `,"new_field":true}`,
			scope: "Debezium envelope", unexpected: "new_field",
		},
		{
			name:  "missing source table",
			value: strings.Replace(debeziumEnvelope("c", `null`, `{"id":1,"name":"ok"}`, "business", "public", "orders"), `,"table":"orders"`, "", 1),
			scope: "Debezium source", missing: "table",
		},
		{
			name:  "unknown source field",
			value: strings.Replace(debeziumEnvelope("c", `null`, `{"id":1,"name":"ok"}`, "business", "public", "orders"), `,"origin_lsn":1}`, `,"origin_lsn":1,"new_source_field":true}`, 1),
			scope: "Debezium source", unexpected: "new_source_field",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodePostgreSQLDebeziumRecord(plugin.ChangeRecord{Key: []byte(`{"id":1}`), Value: []byte(test.value)}, plan)
			var schemaErr *SchemaChangeError
			if !errors.As(err, &schemaErr) {
				t.Fatalf("error = %v, want SchemaChangeError", err)
			}
			if schemaErr.Scope != test.scope || (test.missing != "" && !containsTestString(schemaErr.MissingFields, test.missing)) ||
				(test.unexpected != "" && !containsTestString(schemaErr.UnexpectedFields, test.unexpected)) {
				t.Fatalf("schema diff = %#v", schemaErr)
			}
		})
	}
}

func TestCoerceContinuousValueAcceptsFrozenDebeziumSchemalessTypes(t *testing.T) {
	tests := []struct {
		name      string
		value     interface{}
		fieldType datatype.FieldType
		want      interface{}
	}{
		{name: "decimal string", value: "12345678901234567890.1234", fieldType: datatype.FieldTypeDecimal, want: "12345678901234567890.1234"},
		{name: "date epoch days", value: pluginJSONNumber("1"), fieldType: datatype.FieldTypeDate, want: "1970-01-02"},
		{name: "time epoch millis", value: pluginJSONNumber("3723004"), fieldType: datatype.FieldTypeTime, want: "01:02:03.004"},
		{name: "timestamp epoch millis", value: pluginJSONNumber("1704164645006"), fieldType: datatype.FieldTypeTimestamp, want: time.Date(2024, 1, 2, 3, 4, 5, 6_000_000, time.UTC)},
		{name: "zoned timestamp", value: "2024-01-02T03:04:05.006Z", fieldType: datatype.FieldTypeTimestamp, want: time.Date(2024, 1, 2, 3, 4, 5, 6_000_000, time.UTC)},
		{name: "json logical string", value: `{"enabled":true}`, fieldType: datatype.FieldTypeJSON, want: `{"enabled":true}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := coerceContinuousValue(test.value, test.fieldType)
			if err != nil {
				t.Fatalf("coerceContinuousValue() error = %v", err)
			}
			if !equalContinuousTestValue(got, test.want) {
				t.Fatalf("coerceContinuousValue() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestDecodePostgreSQLDebeziumRecordReportsIncompatibleField(t *testing.T) {
	plan := postgresqlCDCAdapterPlan()
	value := debeziumEnvelope("c", `null`, `{"id":1,"name":123}`, "business", "public", "orders")
	_, err := decodePostgreSQLDebeziumRecord(plugin.ChangeRecord{Key: []byte(`{"id":1}`), Value: []byte(value)}, plan)
	var schemaErr *SchemaChangeError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("error = %v, want SchemaChangeError", err)
	}
	if schemaErr.Scope != "Debezium after" || !containsTestString(schemaErr.IncompatibleFields, "name") {
		t.Fatalf("schema diff = %#v", schemaErr)
	}
}

func TestDecodePostgreSQLDebeziumGeometryAsEWKB(t *testing.T) {
	plan := postgresqlCDCGeometryAdapterPlan("MultiPolygon", 4549, 2)
	geometry := geom.NewMultiPolygonFlat(geom.XY, []float64{
		0, 0, 10, 0, 10, 10, 0, 0,
	}, [][]int{{8}})
	wkb, err := commonSpatial.GeomToEWKB(geometry, 4549)
	if err != nil {
		t.Fatal(err)
	}
	geometryValue, err := json.Marshal(map[string]interface{}{
		"wkb": base64.StdEncoding.EncodeToString(wkb), "srid": 4549,
	})
	if err != nil {
		t.Fatal(err)
	}
	after := `{"id":1,"shape":` + string(geometryValue) + `}`
	event, err := decodePostgreSQLDebeziumRecord(plugin.ChangeRecord{
		Key:   []byte(`{"id":1}`),
		Value: []byte(debeziumEnvelope("c", `null`, after, "business", "public", "orders")),
	}, plan)
	if err != nil {
		t.Fatalf("decodePostgreSQLDebeziumRecord() error = %v", err)
	}
	ewkb, ok := event.Row["geometry"].([]byte)
	if !ok || len(ewkb) == 0 {
		t.Fatalf("geometry row value = %T, want non-empty []byte EWKB", event.Row["geometry"])
	}
	decoded, err := commonSpatial.DecodeGeometryValue(ewkb, "ewkb", 0)
	if err != nil {
		t.Fatalf("decode EWKB: %v", err)
	}
	if decoded.SRID() != 4549 {
		t.Fatalf("EWKB SRID = %d, want 4549", decoded.SRID())
	}
	if _, ok := decoded.(*geom.MultiPolygon); !ok {
		t.Fatalf("EWKB geometry = %T, want *geom.MultiPolygon", decoded)
	}
}

func TestDecodePostgreSQLDebeziumGeometryRejectsSchemaDrift(t *testing.T) {
	pointXY := geom.NewPointFlat(geom.XY, []float64{1, 2})
	pointXYZ := geom.NewPointFlat(geom.XYZ, []float64{1, 2, 3})
	multiPolygon := geom.NewMultiPolygonFlat(geom.XY, []float64{0, 0, 1, 0, 1, 1, 0, 0}, [][]int{{8}})
	encode := func(geometry geom.T) string {
		wkb, err := commonSpatial.GeomToWKB(geometry)
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(wkb)
	}
	tests := []struct {
		name  string
		value map[string]interface{}
	}{
		{name: "unexpected member", value: map[string]interface{}{"wkb": encode(multiPolygon), "srid": 4549, "encoding": "wkb"}},
		{name: "wrong srid", value: map[string]interface{}{"wkb": encode(multiPolygon), "srid": 4326}},
		{name: "wrong topology", value: map[string]interface{}{"wkb": encode(pointXY), "srid": 4549}},
		{name: "wrong dimension", value: map[string]interface{}{"wkb": encode(pointXYZ), "srid": 4549}},
		{name: "invalid base64", value: map[string]interface{}{"wkb": "not-base64", "srid": 4549}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := postgresqlCDCGeometryAdapterPlan("MultiPolygon", 4549, 2)
			geometryValue, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			after := `{"id":1,"shape":` + string(geometryValue) + `}`
			_, err = decodePostgreSQLDebeziumRecord(plugin.ChangeRecord{
				Key:   []byte(`{"id":1}`),
				Value: []byte(debeziumEnvelope("c", `null`, after, "business", "public", "orders")),
			}, plan)
			var schemaErr *SchemaChangeError
			if !errors.As(err, &schemaErr) || !containsTestString(schemaErr.IncompatibleFields, "shape") {
				t.Fatalf("error = %v, want incompatible geometry field", err)
			}
		})
	}
}

func pluginJSONNumber(value string) interface{} {
	var decoded interface{}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	_ = decoder.Decode(&decoded)
	return decoded
}

func equalContinuousTestValue(got, want interface{}) bool {
	gotTime, gotIsTime := got.(time.Time)
	wantTime, wantIsTime := want.(time.Time)
	if gotIsTime || wantIsTime {
		return gotIsTime && wantIsTime && gotTime.Equal(wantTime)
	}
	return got == want
}

func containsTestString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func postgresqlCDCAdapterPlan() *planner.ContinuousPlan {
	return &planner.ContinuousPlan{
		Envelope: planner.ContinuousEnvelopePostgreSQLDebezium,
		Mappings: []planner.ContinuousFieldPlan{
			{Source: "id", Target: "id", Type: datatype.FieldTypeBigInt, Nullable: false},
			{Source: "name", Target: "name", Type: datatype.FieldTypeString, Nullable: true},
		},
		SourceKeys: []string{"id"},
		Target:     planner.ContinuousTargetPlan{Keys: []string{"id"}},
		CDC: &planner.PostgreSQLCDCSourcePlan{
			Database: "business", Schema: "public", Table: "orders",
		},
	}
}

func postgresqlCDCGeometryAdapterPlan(geometryType string, srid, dimension int) *planner.ContinuousPlan {
	plan := postgresqlCDCAdapterPlan()
	plan.Mappings = []planner.ContinuousFieldPlan{
		{Source: "id", Target: "id", Type: datatype.FieldTypeBigInt, Nullable: false},
		{Source: "shape", Target: "geometry", Type: datatype.FieldTypeGeometry, Nullable: true},
	}
	plan.CDC.SpatialInfo = datatype.NewSingleGeometrySpatialInfo("shape", geometryType, srid, dimension)
	return plan
}

func debeziumEnvelope(op, before, after, database, schema, table string) string {
	return `{"before":` + before + `,"after":` + after + `,"source":{` +
		`"version":"3.6.0.Final","connector":"postgresql","name":"addp",` +
		`"ts_ms":1,"snapshot":"false","db":"` + database + `","sequence":"[]",` +
		`"ts_us":1000,"ts_ns":1000000,"schema":"` + schema + `","table":"` + table + `",` +
		`"txId":1,"lsn":1,"xmin":null,"origin":"postgresql","origin_lsn":1},"op":"` + op + `","ts_ms":1,"ts_us":1000,"ts_ns":1000000,"transaction":null}`
}
