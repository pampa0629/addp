package opengauss

import (
	"strings"
	"testing"

	"github.com/addp/common/datatype"
)

func TestBuildOpenGaussMergeSQLUsesNativeMerge(t *testing.T) {
	statement, err := buildOpenGaussMergeSQL("target_schema", "target_table", []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "name", Type: datatype.FieldTypeString},
		{Name: "amount", Type: datatype.FieldTypeDecimal},
	}, []string{"id"})
	if err != nil {
		t.Fatal(err)
	}
	want := `MERGE INTO "target_schema"."target_table" target USING (SELECT CAST($1 AS BIGINT) AS "id", CAST($2 AS TEXT) AS "name", CAST($3 AS NUMERIC) AS "amount") source ON (target."id" = source."id") WHEN MATCHED THEN UPDATE SET "name" = source."name", "amount" = source."amount" WHEN NOT MATCHED THEN INSERT ("id", "name", "amount") VALUES (source."id", source."name", source."amount")`
	if statement != want {
		t.Fatalf("buildOpenGaussMergeSQL() = %q, want %q", statement, want)
	}
	if strings.Contains(statement, "ON CONFLICT") {
		t.Fatalf("openGauss MERGE must not contain PostgreSQL ON CONFLICT: %s", statement)
	}
}

func TestBuildOpenGaussMergeSQLDoesNotUpdateCompositeKeys(t *testing.T) {
	statement, err := buildOpenGaussMergeSQL("public", "items", []datatype.FieldInfo{
		{Name: "tenant_id", Type: datatype.FieldTypeBigInt},
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "value", Type: datatype.FieldTypeString},
	}, []string{"tenant_id", "id"})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`target."tenant_id" = source."tenant_id" AND target."id" = source."id"`,
		`UPDATE SET "value" = source."value"`,
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("MERGE = %q, want fragment %q", statement, fragment)
		}
	}
	if strings.Contains(statement, `UPDATE SET "tenant_id"`) || strings.Contains(statement, `"id" = source."id",`) {
		t.Fatalf("MERGE updates a configured key: %s", statement)
	}
}

func TestBuildOpenGaussMergeSQLSupportsKeyOnlyTable(t *testing.T) {
	statement, err := buildOpenGaussMergeSQL("public", "keys_only", []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
	}, []string{"id"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(statement, "WHEN MATCHED") {
		t.Fatalf("key-only MERGE must not update matching rows: %s", statement)
	}
	if !strings.Contains(statement, `WHEN NOT MATCHED THEN INSERT ("id") VALUES (source."id")`) {
		t.Fatalf("key-only MERGE does not insert unmatched rows: %s", statement)
	}
}

func TestBuildOpenGaussMergeSQLRejectsMissingKeyColumn(t *testing.T) {
	_, err := buildOpenGaussMergeSQL("public", "items", []datatype.FieldInfo{
		{Name: "name", Type: datatype.FieldTypeString},
	}, []string{"id"})
	if err == nil || !strings.Contains(err.Error(), `missing key field "id"`) {
		t.Fatalf("missing key error = %v", err)
	}
}

func TestOpenGaussUpsertRowArgsRejectsMissingField(t *testing.T) {
	_, err := openGaussUpsertRowArgs(map[string]interface{}{"id": int64(1)}, []string{"id", "name"})
	if err == nil || !strings.Contains(err.Error(), `missing field "name"`) {
		t.Fatalf("missing field error = %v", err)
	}
}
