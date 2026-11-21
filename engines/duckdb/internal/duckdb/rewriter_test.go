package duckdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

func TestIsObjectTableItemUsesContentAttributes(t *testing.T) {
	t.Parallel()

	item := commonModels.MetaItem{
		ItemType: "object",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    ".parquet",
			},
		},
	}

	if !IsObjectTableItem(item) {
		t.Fatal("object table parquet item should be recognized")
	}

	item.ItemType = "table"
	if IsObjectTableItem(item) {
		t.Fatal("catalog table item should not be recognized as object/file table")
	}
}

func TestIsObjectTableItemRejectsFormatsWithoutRuntimeReader(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"orc", "avro"} {
		item := commonModels.MetaItem{
			ItemType: "object",
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "table",
					"format":    format,
				},
			},
		}
		if IsObjectTableItem(item) {
			t.Fatalf("%s object must not be advertised as readable through read_parquet", format)
		}
	}
}

func TestBuildObjectTableMapUsesMetaItemList(t *testing.T) {
	t.Parallel()

	var requestedPath string
	var requestedAuthorization string
	var requestedLegacyHeaders bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedAuthorization = r.Header.Get("Authorization")
		requestedLegacyHeaders = r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != ""
		if r.URL.RawQuery != "" {
			t.Fatalf("query = %q, want empty branch query", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]commonModels.MetaItem{{
			ItemType: "object",
			Name:     "sales-data",
			FullName: "lake/sales",
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "table",
					"format":    "parquet",
				},
				"storage": map[string]interface{}{
					"physical_path": "bucket/lake/sales",
				},
			},
		}})
	}))
	defer server.Close()

	metaClient := commonClient.NewMetaClient(server.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "test-token", nil
	}))
	tables := BuildObjectTableMap(context.Background(), 7, []commonModels.Engine{{
		ID:         3,
		Name:       "lake",
		EngineType: "minio",
	}}, metaClient)

	if requestedPath != "/api/v1/meta/engines/3/items" {
		t.Fatalf("requested path = %q, want item list endpoint", requestedPath)
	}
	if requestedAuthorization != "Bearer test-token" || requestedLegacyHeaders {
		t.Fatalf("auth headers = authorization:%q legacy:%t", requestedAuthorization, requestedLegacyHeaders)
	}
	if tables["lake"]["sales-data"] != "bucket/lake/sales" ||
		tables["lake"]["sales_data"] != "bucket/lake/sales" ||
		tables["lake"]["lake/sales"] != "bucket/lake/sales" {
		t.Fatalf("tables = %#v", tables)
	}
}

func TestRewriteTwoPartReferenceDoesNotMatchTableNamePrefix(t *testing.T) {
	db, err := OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	query, paths, err := prepareObjectTables(t.Context(), conn,
		"SELECT 'Business_MinIO.lake3' AS ref FROM Business_MinIO.lake3_parquet",
		map[string]map[string]string{"Business_MinIO": {"lake3": "manager/lake3", "lake3_parquet": "manager/lake3.parquet"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(paths, []string{"s3://manager/lake3.parquet"}) || !strings.Contains(query, "'Business_MinIO.lake3'") || !strings.Contains(query, "'s3://manager/lake3.parquet'") {
		t.Fatalf("wrong object selection: %q, %v", query, paths)
	}
}

func TestParquetMembersStayFixedWhenDirectoryGrows(t *testing.T) {
	db, err := OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	dir := t.TempDir()
	write := func(name, value string) {
		t.Helper()
		_, err := conn.ExecContext(t.Context(), "COPY (SELECT "+value+" AS id) TO "+duckDBString(filepath.Join(dir, name))+" (FORMAT PARQUET)")
		if err != nil {
			t.Fatal(err)
		}
	}
	write("one's.parquet", "1")
	write("two.parquet", "2")
	files, err := parquetMembers(t.Context(), conn, dir)
	if err != nil || !slices.Equal(files, []string{filepath.Join(dir, "one's.parquet"), filepath.Join(dir, "two.parquet")}) {
		t.Fatal(files, err)
	}
	write("three.parquet", "3")
	if err := lockExternalAccessWithAllowedPaths(t.Context(), conn, files); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := conn.QueryRowContext(t.Context(), "SELECT sum(id) FROM read_parquet("+duckDBStringList(files)+")").Scan(&total); err != nil || total != 3 {
		t.Fatal(total, err)
	}
	if err := conn.QueryRowContext(t.Context(), "SELECT id FROM read_parquet("+duckDBString(filepath.Join(dir, "three.parquet"))+")").Scan(&total); err == nil {
		t.Fatal("new member read outside the prepared list")
	}
}

func TestObjectQueryKeepsQualifiedDataItemIdentity(t *testing.T) {
	db, err := OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tables := map[string]map[string]string{"lake": {
		"sales": "bucket/retail.parquet", "archive.sales": "bucket/archive.parquet",
	}}
	_, paths, err := prepareObjectTables(t.Context(), conn,
		"SELECT * FROM lake.sales a JOIN lake.archive.sales b USING (id)", tables)
	if err != nil || len(paths) != 2 || !slices.Contains(paths, "s3://bucket/retail.parquet") || !slices.Contains(paths, "s3://bucket/archive.parquet") {
		t.Fatalf("JOIN did not bind both declared tables: paths=%v err=%v", paths, err)
	}
	_, _, err = prepareObjectTables(t.Context(), conn,
		"SELECT * FROM lake.sales a JOIN lake.absent.sales b USING (id)", tables)
	if err == nil {
		t.Fatal("unknown qualified object table was accepted during preparation")
	}
}
