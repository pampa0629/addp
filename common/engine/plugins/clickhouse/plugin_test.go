package clickhouse

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
)

func TestClickHouseCapabilitiesDeclareTableWriteProviders(t *testing.T) {
	caps := (&ClickHousePlugin{}).Capabilities()
	if caps.Storage == nil || caps.Storage.Store == nil {
		t.Fatalf("clickhouse capabilities missing store: %#v", caps.Storage)
	}
	if !caps.Storage.Store.TableWritePrepare {
		t.Fatalf("clickhouse capabilities do not declare table_write_prepare: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.TableWriteSession {
		t.Fatalf("clickhouse capabilities do not declare table_write_session: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.BatchWrite {
		t.Fatalf("clickhouse capabilities do not declare batch_write: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.Delete {
		t.Fatalf("clickhouse capabilities do not declare delete: %#v", caps.Storage.Store)
	}
	if err := plugin.ValidatePluginCapabilities(&ClickHousePlugin{}); err != nil {
		t.Fatalf("ValidatePluginCapabilities failed: %v", err)
	}
}

func TestClickHouseIsSystemSchema(t *testing.T) {
	plugin := &ClickHousePlugin{}

	for _, name := range []string{"system", "information_schema", "INFORMATION_SCHEMA"} {
		if !plugin.isSystemSchema(name) {
			t.Fatalf("isSystemSchema(%q) = false, want true", name)
		}
	}

	if plugin.isSystemSchema("analytics") {
		t.Fatal("isSystemSchema(\"analytics\") = true, want false")
	}
}

func TestClickHouseFieldInfoMapsDefaultExpression(t *testing.T) {
	field := clickhouseFieldInfo(clickhouseColumnRow{
		Name:              "status",
		NativeType:        "String",
		Nullable:          true,
		Comment:           "order status",
		DefaultKind:       "DEFAULT",
		DefaultExpression: "'new'",
	})

	if field.Name != "status" || field.Type != datatype.FieldTypeString || field.NativeType != "String" {
		t.Fatalf("field identity = %#v", field)
	}
	if !field.Nullable || field.Comment != "order status" {
		t.Fatalf("field nullable/comment = %#v", field)
	}
	if field.DefaultExpression != "'new'" || field.Generated || field.GenerationExpression != "" {
		t.Fatalf("field default/generation = %#v", field)
	}
	if field.PrimaryKey {
		t.Fatalf("ClickHouse field should not map native key facts to ADDP primary_key: %#v", field)
	}
}

func TestClickHouseFieldInfoMapsNativeTypesToCanonicalTypes(t *testing.T) {
	tests := map[string]datatype.FieldType{
		"Int64":                         datatype.FieldTypeBigInt,
		"Nullable(String)":              datatype.FieldTypeString,
		"Decimal(18, 2)":                datatype.FieldTypeDecimal,
		"Array(LowCardinality(String))": datatype.FieldTypeArray,
	}
	for nativeType, want := range tests {
		field := clickhouseFieldInfo(clickhouseColumnRow{Name: "value", NativeType: nativeType})
		if field.Type != want || field.NativeType != nativeType {
			t.Fatalf("clickhouseFieldInfo(%q) = %#v, want type %q", nativeType, field, want)
		}
	}
}

func TestClickHouseFieldInfoMapsGeneratedExpression(t *testing.T) {
	tests := []string{"MATERIALIZED", "ALIAS"}
	for _, defaultKind := range tests {
		t.Run(defaultKind, func(t *testing.T) {
			field := clickhouseFieldInfo(clickhouseColumnRow{
				Name:              "total",
				NativeType:        "Int64",
				DefaultKind:       defaultKind,
				DefaultExpression: "price * quantity",
			})

			if !field.Generated || field.GenerationExpression != "price * quantity" {
				t.Fatalf("field generation = %#v", field)
			}
			if field.DefaultExpression != "" {
				t.Fatalf("field default expression = %q, want empty for generated column", field.DefaultExpression)
			}
		})
	}
}

func TestClickHouseSQLTypeForField(t *testing.T) {
	tests := []struct {
		name  string
		field datatype.FieldInfo
		want  string
	}{
		{name: "string nullable", field: datatype.FieldInfo{Name: "name", Type: datatype.FieldTypeString, Nullable: true}, want: "Nullable(String)"},
		{name: "bigint", field: datatype.FieldInfo{Name: "id", Type: datatype.FieldTypeBigInt}, want: "Int64"},
		{name: "decimal", field: datatype.FieldInfo{Name: "amount", Type: datatype.FieldTypeDecimal}, want: "Decimal(38,10)"},
		{name: "timestamp", field: datatype.FieldInfo{Name: "created_at", Type: datatype.FieldTypeTimestamp}, want: "DateTime"},
		{name: "json fallback", field: datatype.FieldInfo{Name: "payload", Type: datatype.FieldTypeJSON}, want: "String"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clickhouseSQLTypeForField(tt.field); got != tt.want {
				t.Fatalf("clickhouseSQLTypeForField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClickHouseWriteFieldsSkipsGeneratedAndRejectsSpatial(t *testing.T) {
	fields, err := clickhouseWriteFields([]datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "total", Type: datatype.FieldTypeDouble, Generated: true},
	})
	if err != nil {
		t.Fatalf("clickhouseWriteFields failed: %v", err)
	}
	if got, want := fieldNames(fields), []string{"id"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("field names = %#v, want %#v", got, want)
	}

	_, err = clickhouseWriteFields([]datatype.FieldInfo{
		{Name: "geom", Type: datatype.FieldTypeGeometry},
	})
	if err == nil {
		t.Fatal("clickhouseWriteFields succeeded with spatial field, want error")
	}
}

func TestClickHouseSchemaEvolutionStatementsAddsMissingColumns(t *testing.T) {
	statements, err := clickhouseSchemaEvolutionStatements("analytics", "events", []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Nullable: true},
	}, []clickhouseWriteColumnInfo{
		{Name: "id", NativeType: "Int64"},
	})
	if err != nil {
		t.Fatalf("clickhouseSchemaEvolutionStatements failed: %v", err)
	}
	want := []string{
		"ALTER TABLE `analytics`.`events` ADD COLUMN `name` Nullable(String)",
		"ALTER TABLE `analytics`.`events` ADD COLUMN `amount` Nullable(Decimal(38,10))",
	}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("statements = %#v, want %#v", statements, want)
	}
}

func TestClickHouseSchemaEvolutionStatementsRejectsTypeConflict(t *testing.T) {
	_, err := clickhouseSchemaEvolutionStatements("analytics", "target", []datatype.FieldInfo{
		{Name: "amount", Type: datatype.FieldTypeDouble},
	}, []clickhouseWriteColumnInfo{
		{Name: "amount", NativeType: "String"},
	})
	if err == nil {
		t.Fatal("clickhouseSchemaEvolutionStatements succeeded with conflicting type, want error")
	}
}

func TestClickHouseSchemaEvolutionStatementsRejectsMissingNonNullColumnWithoutDefault(t *testing.T) {
	_, err := clickhouseSchemaEvolutionStatements("analytics", "target", []datatype.FieldInfo{
		{Name: "name", Type: datatype.FieldTypeString, Nullable: false},
	}, nil)
	if err == nil {
		t.Fatal("clickhouseSchemaEvolutionStatements succeeded with missing non-null column without default, want error")
	}
}

func TestClickHouseNormalizeTypeName(t *testing.T) {
	tests := map[string]string{
		"Nullable(Int64)":                  "Int64",
		"LowCardinality(String)":           "String",
		"Nullable(Decimal(38,10))":         "Decimal",
		"LowCardinality(Nullable(UInt32))": "UInt32",
	}
	for nativeType, want := range tests {
		if got := clickhouseNormalizeTypeName(nativeType); got != want {
			t.Fatalf("clickhouseNormalizeTypeName(%q) = %q, want %q", nativeType, got, want)
		}
	}
}

func TestClickHouseTablePathPartsRequiresDatabaseAndTable(t *testing.T) {
	_, _, err := clickhouseTablePathParts(plugin.CatalogPath{})
	if err == nil {
		t.Fatal("clickhouseTablePathParts() succeeded, want error")
	}
	if !strings.Contains(err.Error(), "database/table") {
		t.Fatalf("error = %q, want database/table", err)
	}
}

func TestBuildClickHouseInsertSQL(t *testing.T) {
	rows := []map[string]interface{}{
		{"id": 1, "city`name": "Hangzhou"},
		{"id": 2, "city`name": "Shanghai"},
	}

	sql, args := buildClickHouseInsertSQL("analytics", "target table", []string{"id", "city`name"}, rows)
	wantSQL := "INSERT INTO `analytics`.`target table` (`id`, `city``name`) VALUES (?, ?), (?, ?)"
	if sql != wantSQL {
		t.Fatalf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []interface{}{1, "Hangzhou", 2, "Shanghai"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestShouldUseClickHouseInsertWriteMethod(t *testing.T) {
	for _, method := range []string{"", "insert", "clickhouse_insert", "copy"} {
		if !shouldUseClickHouseInsertWriteMethod(method) {
			t.Fatalf("shouldUseClickHouseInsertWriteMethod(%q) = false, want true", method)
		}
	}
	if shouldUseClickHouseInsertWriteMethod("postgres_copy") {
		t.Fatal("shouldUseClickHouseInsertWriteMethod(postgres_copy) = true, want false")
	}
}

func TestClickHouseOpenTableWriteSessionRejectsResumeMarker(t *testing.T) {
	clickhousePlugin := &ClickHousePlugin{}
	_, err := clickhousePlugin.OpenTableWriteSession(nil, nil, plugin.CatalogPath{}, plugin.TableWriteSessionOptions{
		ResumeMarker: &resume.Marker{Version: resume.MarkerVersionV1},
	})
	if err == nil {
		t.Fatal("OpenTableWriteSession succeeded with resume marker, want explicit unsupported error")
	}
}

func TestClickHouseTableWriteSessionBuildCommitMarker(t *testing.T) {
	session := &clickhouseTableWriteSession{
		database:       "analytics",
		table:          "events",
		columns:        []string{"id", "name"},
		batchesWritten: 2,
		rowsWritten:    3,
	}
	session.commitMarker = session.buildCommitMarker()

	marker := session.CommitMarker()
	if marker == nil {
		t.Fatal("CommitMarker() = nil, want marker after close")
	}
	if marker.Version != resume.MarkerVersionV1 ||
		marker.Provider != "clickhouse.table_write_session" ||
		marker.PositionUnit != "session_commit" {
		t.Fatalf("marker identity = %#v, want clickhouse session commit marker", marker)
	}
	if marker.CommitPosition["rows_committed"] != int64(3) ||
		marker.CommitPosition["batches_committed"] != int64(2) {
		t.Fatalf("commit position = %#v, want committed rows and batches", marker.CommitPosition)
	}
	if marker.Fingerprint["target"] != "analytics/events" ||
		marker.Fingerprint["database"] != "analytics" ||
		marker.Fingerprint["table"] != "events" ||
		marker.Fingerprint["method"] != "clickhouse_insert" {
		t.Fatalf("fingerprint = %#v, want target facts", marker.Fingerprint)
	}
	columns, ok := marker.Fingerprint["columns"].([]string)
	if !ok || len(columns) != 2 || columns[0] != "id" || columns[1] != "name" {
		t.Fatalf("columns fingerprint = %#v, want copied column list", marker.Fingerprint["columns"])
	}

	columns[0] = "mutated"
	fresh := session.CommitMarker()
	freshColumns := fresh.Fingerprint["columns"].([]string)
	if freshColumns[0] != "id" {
		t.Fatalf("CommitMarker() exposed mutable columns: %#v", freshColumns)
	}
}

func fieldNames(fields []datatype.FieldInfo) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
	}
	return names
}
