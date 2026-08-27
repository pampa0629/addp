package doris

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
)

func TestExecuteSQLRejectsWriteInReadOnlyMode(t *testing.T) {
	t.Parallel()

	if _, err := (&DorisPlugin{}).ExecuteSQL(
		context.Background(), nil, "DELETE FROM orders", plugin.QueryOptions{ReadOnly: true},
	); err == nil {
		t.Fatal("ExecuteSQL() error = nil, want read-only rejection")
	}
}

func TestDorisCapabilitiesDeclareTableWriteProviders(t *testing.T) {
	caps := (&DorisPlugin{}).Capabilities()
	if caps.Storage == nil || caps.Storage.Store == nil {
		t.Fatalf("doris capabilities missing store: %#v", caps.Storage)
	}
	if !caps.Storage.Store.TableWritePrepare {
		t.Fatalf("doris capabilities do not declare table_write_prepare: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.TableWriteSession {
		t.Fatalf("doris capabilities do not declare table_write_session: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.BatchWrite {
		t.Fatalf("doris capabilities do not declare batch_write: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.Delete {
		t.Fatalf("doris capabilities do not declare delete: %#v", caps.Storage.Store)
	}
	if err := plugin.ValidatePluginCapabilities(&DorisPlugin{}); err != nil {
		t.Fatalf("ValidatePluginCapabilities failed: %v", err)
	}
}

func TestDorisCatalogFactsDialectDoesNotIncludeEngine(t *testing.T) {
	if dorisCatalogFactsDialect.IncludeEngine {
		t.Fatal("Doris must not enable table native engine before information_schema.tables.engine is confirmed stable")
	}
	if got := dorisCatalogFactsDialect.IsSystemSchema("__internal_schema"); !got {
		t.Fatal("Doris should filter __internal_schema as a system database")
	}
	if got := dorisCatalogFactsDialect.IsSystemSchema("analytics"); got {
		t.Fatal("Doris should not filter user database analytics")
	}
}

func TestDorisCatalogFieldTypeMapsNativeTypes(t *testing.T) {
	if dorisCatalogFactsDialect.MapFieldType == nil {
		t.Fatal("Doris EngineCatalogFacts dialect must declare its field type mapper")
	}
	tests := map[string]datatype.FieldType{
		"int":             datatype.FieldTypeInt,
		"largeint":        datatype.FieldTypeBigInt,
		"decimalv3(18,2)": datatype.FieldTypeDecimal,
		"string":          datatype.FieldTypeString,
		"datetimev2(6)":   datatype.FieldTypeTimestamp,
		"variant":         datatype.FieldTypeJSON,
	}
	for nativeType, want := range tests {
		if got := dorisCatalogFactsDialect.MapFieldType(nativeType); got != want {
			t.Fatalf("dorisCatalogFieldType(%q) = %q, want %q", nativeType, got, want)
		}
	}
}

func TestDorisDSNsInterpolateParametersClientSide(t *testing.T) {
	connInfo := plugin.ConnectionInfo{
		"host":     "doris.example",
		"port":     9030,
		"user":     "root",
		"password": "secret",
		"database": "analytics",
	}

	for name, build := range map[string]func() (string, error){
		"database": func() (string, error) {
			return (&DorisPlugin{}).BuildDSN(connInfo)
		},
		"server": func() (string, error) {
			return (&DorisPlugin{}).serverDSN(connInfo)
		},
	} {
		t.Run(name, func(t *testing.T) {
			dsn, err := build()
			if err != nil {
				t.Fatalf("build Doris DSN: %v", err)
			}
			if !strings.Contains(dsn, "interpolateParams=true") {
				t.Fatalf("Doris DSN = %q, want interpolateParams=true", dsn)
			}
		})
	}
}

func TestDorisSQLTypeForField(t *testing.T) {
	tests := []struct {
		name  string
		field datatype.FieldInfo
		want  string
	}{
		{name: "string", field: datatype.FieldInfo{Name: "name", Type: datatype.FieldTypeString}, want: "VARCHAR(65533)"},
		{name: "bigint", field: datatype.FieldInfo{Name: "id", Type: datatype.FieldTypeBigInt}, want: "BIGINT"},
		{name: "decimal", field: datatype.FieldInfo{Name: "amount", Type: datatype.FieldTypeDecimal}, want: "DECIMAL(38,10)"},
		{name: "time fallback", field: datatype.FieldInfo{Name: "clock", Type: datatype.FieldTypeTime}, want: "STRING"},
		{name: "json fallback", field: datatype.FieldInfo{Name: "payload", Type: datatype.FieldTypeJSON}, want: "STRING"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dorisSQLTypeForField(tt.field); got != tt.want {
				t.Fatalf("dorisSQLTypeForField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDorisWriteFieldsRejectsSpatial(t *testing.T) {
	_, err := dorisWriteFields([]datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "geom", Type: datatype.FieldTypeGeometry},
	})
	if err == nil {
		t.Fatal("dorisWriteFields succeeded with spatial field, want error")
	}
}

func TestDorisFieldsWithKeyFirst(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "name", Type: datatype.FieldTypeString},
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "score", Type: datatype.FieldTypeDouble},
	}
	got := dorisFieldsWithKeyFirst(fields, "id")
	want := []string{"id", "name", "score"}
	if names := fieldNames(got); !reflect.DeepEqual(names, want) {
		t.Fatalf("dorisFieldsWithKeyFirst names = %#v, want %#v", names, want)
	}
}

func TestDorisSchemaEvolutionStatementsAddsMissingColumns(t *testing.T) {
	statements, err := dorisSchemaEvolutionStatements("analytics", "events", []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Nullable: true},
	}, []dorisColumnInfo{
		{Name: "id", DataType: "bigint", NativeType: "bigint"},
	})
	if err != nil {
		t.Fatalf("dorisSchemaEvolutionStatements failed: %v", err)
	}
	want := []string{
		"ALTER TABLE `analytics`.`events` ADD COLUMN `name` VARCHAR(65533)",
		"ALTER TABLE `analytics`.`events` ADD COLUMN `amount` DECIMAL(38,10)",
	}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("statements = %#v, want %#v", statements, want)
	}
}

func TestDorisSchemaEvolutionStatementsRejectsTypeConflict(t *testing.T) {
	_, err := dorisSchemaEvolutionStatements("analytics", "target", []datatype.FieldInfo{
		{Name: "amount", Type: datatype.FieldTypeDouble},
	}, []dorisColumnInfo{
		{Name: "amount", DataType: "varchar", NativeType: "varchar(20)"},
	})
	if err == nil {
		t.Fatal("dorisSchemaEvolutionStatements succeeded with conflicting type, want error")
	}
}

func TestDorisSchemaEvolutionStatementsRejectsMissingNonNullColumnWithoutDefault(t *testing.T) {
	_, err := dorisSchemaEvolutionStatements("analytics", "target", []datatype.FieldInfo{
		{Name: "name", Type: datatype.FieldTypeString, Nullable: false},
	}, nil)
	if err == nil {
		t.Fatal("dorisSchemaEvolutionStatements succeeded with missing non-null column without default, want error")
	}
}

func TestDorisTablePathPartsRequiresDatabaseAndTable(t *testing.T) {
	_, _, err := dorisTablePathParts(plugin.EngineCatalogPath{})
	if err == nil {
		t.Fatal("dorisTablePathParts() succeeded, want error")
	}
	if !strings.Contains(err.Error(), "database/table") {
		t.Fatalf("error = %q, want database/table", err)
	}
}

func TestBuildDorisInsertSQL(t *testing.T) {
	rows := []map[string]interface{}{
		{"id": 1, "city`name": "Hangzhou"},
		{"id": 2, "city`name": "Shanghai"},
	}

	sql, args := buildDorisInsertSQL("analytics", "target table", []string{"id", "city`name"}, rows)
	wantSQL := "INSERT INTO `analytics`.`target table` (`id`, `city``name`) VALUES (?, ?), (?, ?)"
	if sql != wantSQL {
		t.Fatalf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []interface{}{1, "Hangzhou", 2, "Shanghai"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestShouldUseDorisInsertWriteMethod(t *testing.T) {
	for _, method := range []string{"", "insert", "doris_insert", "copy"} {
		if !shouldUseDorisInsertWriteMethod(method) {
			t.Fatalf("shouldUseDorisInsertWriteMethod(%q) = false, want true", method)
		}
	}
	if shouldUseDorisInsertWriteMethod("postgres_copy") {
		t.Fatal("shouldUseDorisInsertWriteMethod(postgres_copy) = true, want false")
	}
}

func TestDorisOpenTableWriteSessionRejectsResumeMarker(t *testing.T) {
	dorisPlugin := &DorisPlugin{}
	_, err := dorisPlugin.OpenTableWriteSession(nil, nil, plugin.EngineCatalogPath{}, plugin.TableWriteSessionOptions{
		ResumeMarker: &resume.Marker{Version: resume.MarkerVersionV1},
	})
	if err == nil {
		t.Fatal("OpenTableWriteSession succeeded with resume marker, want explicit unsupported error")
	}
}

func TestDorisTableWriteSessionBuildCommitMarker(t *testing.T) {
	session := &dorisTableWriteSession{
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
		marker.Provider != "doris.table_write_session" ||
		marker.PositionUnit != "session_commit" {
		t.Fatalf("marker identity = %#v, want doris session commit marker", marker)
	}
	if marker.CommitPosition["rows_committed"] != int64(3) ||
		marker.CommitPosition["batches_committed"] != int64(2) {
		t.Fatalf("commit position = %#v, want committed rows and batches", marker.CommitPosition)
	}
	if marker.Fingerprint["target"] != "analytics/events" ||
		marker.Fingerprint["database"] != "analytics" ||
		marker.Fingerprint["table"] != "events" ||
		marker.Fingerprint["method"] != "doris_insert" {
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
