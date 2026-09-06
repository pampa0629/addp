package shared

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestMySQLCompatibleTableWriterMapsNonSpatialTypes(t *testing.T) {
	w := MySQLCompatibleTableWriter{EngineType: "oceanbase", DefaultPort: 2881}
	tests := []struct {
		field datatype.FieldInfo
		want  string
	}{
		{field: datatype.FieldInfo{Name: "name", Type: datatype.FieldTypeString}, want: "TEXT"},
		{field: datatype.FieldInfo{Name: "active", Type: datatype.FieldTypeBool}, want: "TINYINT(1)"},
		{field: datatype.FieldInfo{Name: "occurred_at", Type: datatype.FieldTypeTimestamp}, want: "DATETIME(6)"},
		{field: datatype.FieldInfo{Name: "event_time", Type: datatype.FieldTypeTime}, want: "TIME(6)"},
		{field: datatype.FieldInfo{Name: "request_id", Type: datatype.FieldTypeUUID}, want: "VARCHAR(36)"},
		{field: datatype.FieldInfo{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, want: "DECIMAL(18,2)"},
	}
	for _, tt := range tests {
		t.Run(tt.field.Name, func(t *testing.T) {
			got, err := w.sqlTypeForField(tt.field)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("sqlTypeForField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMySQLCompatibleTableWriterBuildsSafeAdditiveEvolution(t *testing.T) {
	w := MySQLCompatibleTableWriter{EngineType: "oceanbase", DefaultPort: 2881}
	existing := []mysqlCompatibleColumnInfo{
		{Name: "id", DataType: "bigint", NativeType: "bigint", PrimaryKey: true},
		{Name: "amount", DataType: "decimal", NativeType: "decimal(18,2)", NumericPrecision: sql.NullInt64{Int64: 18, Valid: true}, NumericScale: sql.NullInt64{Int64: 2, Valid: true}},
	}
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2},
		{Name: "note", Type: datatype.FieldTypeString, Nullable: true},
	}
	statements, err := w.schemaEvolutionStatements("business", "orders", fields, existing)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ALTER TABLE `business`.`orders` ADD COLUMN `note` TEXT"}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("schemaEvolutionStatements() = %#v, want %#v", statements, want)
	}

	fields[2].Nullable = false
	if _, err := w.schemaEvolutionStatements("business", "orders", fields, existing); err == nil || !strings.Contains(err.Error(), "cannot add non-null column") {
		t.Fatalf("non-null additive evolution error = %v", err)
	}
}

func TestMySQLCompatibleTableWriterBuildsDeterministicInsert(t *testing.T) {
	statement, args := mysqlCompatibleInsertSQL(
		"business",
		"orders",
		[]string{"id", "name"},
		[]map[string]interface{}{{"id": int64(1), "name": "王小丽"}, {"id": int64(2), "name": "OceanBase"}},
	)
	if want := "INSERT INTO `business`.`orders` (`id`, `name`) VALUES (?, ?), (?, ?)"; statement != want {
		t.Fatalf("mysqlCompatibleInsertSQL() = %q, want %q", statement, want)
	}
	if want := []interface{}{int64(1), "王小丽", int64(2), "OceanBase"}; !reflect.DeepEqual(args, want) {
		t.Fatalf("mysqlCompatibleInsertSQL() args = %#v, want %#v", args, want)
	}
}

func TestMySQLCompatibleTableWriterRejectsSpatialBoundary(t *testing.T) {
	w := MySQLCompatibleTableWriter{EngineType: "oceanbase", EngineName: "OceanBase", DefaultPort: 2881}
	fields := []datatype.FieldInfo{{Name: "shape", Type: datatype.FieldTypeGeometry}}
	if !HasSpatialTableWrite(fields, nil) {
		t.Fatal("geometry field must be recognized as a spatial table write")
	}
	err := w.PrepareTableWrite(nil, nil, plugin.EngineCatalogPath{}, plugin.TableWriteOptions{Fields: fields})
	if err == nil || !strings.Contains(err.Error(), "does not support spatial fields") {
		t.Fatalf("PrepareTableWrite() spatial error = %v", err)
	}
}

func TestMySQLCompatibleTableWriterMethodBoundary(t *testing.T) {
	w := MySQLCompatibleTableWriter{EngineType: "oceanbase", DefaultPort: 2881}
	for _, method := range []string{"", "insert", "copy", "oceanbase_insert"} {
		if !w.supportsInsertMethod(method) {
			t.Fatalf("method %q must be accepted", method)
		}
	}
	if w.supportsInsertMethod("mysql_insert") || w.supportsInsertMethod("upsert") {
		t.Fatal("engine-specific or upsert methods must not cross the OceanBase boundary")
	}
}
