package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
	serviceModels "github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateServiceRemovesRecordWhenLineagePublicationFails(t *testing.T) {
	db := openQueryServiceCreateTestDB(t)
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/meta/items/33":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(commonModels.MetaItem{
				ID: 33, TenantID: 7, EngineID: 9, ItemType: "table", Name: "sales", FullName: "public.sales", Fingerprint: "sales-fingerprint",
				Attributes: map[string]interface{}{
					"item": map[string]interface{}{"data_type": "table"},
					"type_info": map[string]interface{}{"table": map[string]interface{}{
						"fields": []interface{}{
							map[string]interface{}{"name": "id", "type": "bigint", "native_type": "int8", "nullable": false, "primary_key": true},
							map[string]interface{}{"name": "name", "type": "string", "native_type": "text", "nullable": true},
						},
						"primary_key": []interface{}{"id"},
					}},
				},
			})
		case "/api/v1/meta/lineage/services":
			http.Error(w, "lineage unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer metaServer.Close()

	metaClient := commonClient.NewMetaClient(metaServer.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "test-token", nil
	}))
	svc := NewQueryServiceService(repository.NewQueryServiceRepository(db), nil, metaClient, "")
	engineID := uint(9)
	_, err := svc.CreateService(&serviceModels.CreateQueryServiceRequest{
		ServiceName: "lineage_failure", Title: "Lineage failure", ConfigType: "table", EngineID: &engineID,
		DataConfig: map[string]interface{}{
			"locator":           "addp://engine/9/path/public/sales?type=table&item_id=33",
			"default_fields":    []string{"id", "name"},
			"filterable_fields": []string{"name"},
		},
	}, 7, 11)
	if err == nil {
		t.Fatal("CreateService() error = nil, want lineage publication failure")
	}
	var count int64
	if err := db.Model(&serviceModels.QueryService{}).Where("service_name = ?", "lineage_failure").Count(&count).Error; err != nil {
		t.Fatalf("count query services: %v", err)
	}
	if count != 0 {
		t.Fatalf("persisted query services = %d, want 0 after lineage publication failure", count)
	}
}

func openQueryServiceCreateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:query-service-create-lineage?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS service").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE service.query_services (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		service_name TEXT NOT NULL UNIQUE,
		title TEXT NOT NULL,
		description TEXT,
		keywords TEXT,
		config_type TEXT NOT NULL,
		engine_id INTEGER,
		runtime_engine_id INTEGER,
		schema_name TEXT,
		table_name TEXT,
		sql_query TEXT,
		data_config JSON NOT NULL,
		protocols JSON NOT NULL,
		public_access BOOLEAN,
		max_features INTEGER,
		status TEXT,
		error_message TEXT,
		created_by INTEGER NOT NULL,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestBuildTableDependencySnapshotUsesCommonFacts(t *testing.T) {
	t.Parallel()

	scannedAt := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	item := &commonModels.MetaItem{
		ID:          33,
		EngineID:    9,
		ItemType:    "object",
		Name:        "sales.parquet",
		Fingerprint: "item-fingerprint-33",
		ScannedAt:   &scannedAt,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "parquet",
				"layout":    "single",
			},
			"storage": map[string]interface{}{
				"physical_path": "bucket/public/sales.parquet",
			},
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"fields": []interface{}{
						map[string]interface{}{"name": "id", "type": "bigint", "native_type": "int8", "primary_key": true},
						map[string]interface{}{"name": "shape", "type": "geometry", "native_type": "geometry"},
					},
					"primary_key": []interface{}{"id"},
					"row_count":   int64(100),
				},
			},
			"capabilities": map[string]interface{}{
				"spatial": map[string]interface{}{
					"geometry_columns": []interface{}{
						map[string]interface{}{"name": "shape", "geometry_type": "Point", "srid": 32650, "crs_ref": "EPSG:32650"},
					},
					"primary_geometry_column": "shape",
					"extent":                  []interface{}{500000.0, 3500000.0, 510000.0, 3510000.0},
				},
			},
		},
	}

	snapshot, err := buildTableDependencySnapshot(item, scannedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("buildTableDependencySnapshot() error = %v", err)
	}
	if snapshot.Source == nil || snapshot.Source.ItemID != 33 || snapshot.Source.ItemFingerprint != "item-fingerprint-33" {
		t.Fatalf("source = %#v", snapshot.Source)
	}
	if snapshot.Table == nil || len(snapshot.Table.Fields) != 2 || snapshot.Table.RowCount != nil {
		t.Fatalf("table = %#v", snapshot.Table)
	}
	if snapshot.Spatial == nil || snapshot.Spatial.PrimaryGeometryName() != "shape" || snapshot.Spatial.Extent == nil {
		t.Fatalf("spatial = %#v", snapshot.Spatial)
	}
	if snapshot.ObjectTable == nil || snapshot.ObjectTable.PhysicalPath != "bucket/public/sales.parquet" {
		t.Fatalf("object table = %#v", snapshot.ObjectTable)
	}
	if snapshot.DependencyHash == "" {
		t.Fatal("dependency hash is empty")
	}
}

func TestValidateQueryFieldPolicyNormalizesAndRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	table := &datatype.TableInfo{Fields: []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "name", Type: datatype.FieldTypeString},
	}}
	config := serviceModels.JSONB{
		"default_fields":    []interface{}{" id ", "name"},
		"filterable_fields": []string{"name"},
	}
	if err := validateQueryFieldPolicy(config, table); err != nil {
		t.Fatalf("validateQueryFieldPolicy() error = %v", err)
	}
	if fields, ok := config["default_fields"].([]string); !ok || len(fields) != 2 || fields[0] != "id" {
		t.Fatalf("normalized default fields = %#v", config["default_fields"])
	}
	if err := validateQueryFieldPolicy(serviceModels.JSONB{"filterable_fields": []interface{}{"missing"}}, table); err == nil {
		t.Fatal("unknown filterable field must be rejected")
	}
}

func TestDependencyHashIgnoresExtentAndScanTimes(t *testing.T) {
	t.Parallel()

	srid := 4326
	first := &serviceModels.QueryServiceDependencySnapshot{
		Source: &serviceModels.QueryServiceSourceRef{ItemID: 1, ItemFingerprint: "fp"},
		Table:  &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "shape", Type: datatype.FieldTypeGeometry}}},
		Spatial: &datatype.SpatialInfo{
			GeometryColumns:       []datatype.GeometryColumnInfo{{Name: "shape", SRID: &srid}},
			PrimaryGeometryColumn: "shape",
			Extent:                boundingBoxPtr(0, 0, 1, 1),
		},
	}
	second := &serviceModels.QueryServiceDependencySnapshot{
		Source:  &serviceModels.QueryServiceSourceRef{ItemID: 1, ItemFingerprint: "fp"},
		Table:   first.Table.Clone(),
		Spatial: first.Spatial.Clone(),
	}
	second.Spatial.Extent = boundingBoxPtr(10, 10, 20, 20)

	if queryServiceDependencyHash(first) != queryServiceDependencyHash(second) {
		t.Fatal("extent change must not change dependency hash")
	}
	second.Spatial.GeometryColumns[0].Name = "new_shape"
	if queryServiceDependencyHash(first) == queryServiceDependencyHash(second) {
		t.Fatal("geometry contract change must change dependency hash")
	}
}

func TestDependencyHashIncludesFederatedObjectTableMapping(t *testing.T) {
	t.Parallel()

	first := buildSQLDependencySnapshot("SELECT * FROM lake.sales", nil, time.Now())
	first.FederatedObjectTables = map[string]map[string]string{
		"lake": {"sales": "bucket/sales-v1.parquet"},
	}
	second := buildSQLDependencySnapshot("SELECT * FROM lake.sales", nil, time.Now())
	second.FederatedObjectTables = map[string]map[string]string{
		"lake": {"sales": "bucket/sales-v2.parquet"},
	}
	if queryServiceDependencyHash(first) == queryServiceDependencyHash(second) {
		t.Fatal("federated object table mapping change must change dependency hash")
	}
}

func TestDependencyHashIncludesFederatedSourceEngineIDs(t *testing.T) {
	t.Parallel()

	first := buildSQLDependencySnapshot("SELECT * FROM source.sales", nil, time.Now())
	first.FederatedSourceEngineIDs = []uint{9}
	second := buildSQLDependencySnapshot("SELECT * FROM source.sales", nil, time.Now())
	second.FederatedSourceEngineIDs = []uint{10}
	if queryServiceDependencyHash(first) == queryServiceDependencyHash(second) {
		t.Fatal("federated source engine change must change dependency hash")
	}
}

func TestTableResourceRefFromRequestDerivesExecutionSnapshot(t *testing.T) {
	t.Parallel()

	engineID := uint(9)
	ref, err := tableResourceRefFromRequest(&serviceModels.CreateQueryServiceRequest{
		ConfigType: "table",
		EngineID:   &engineID,
		DataConfig: map[string]interface{}{
			"locator": "addp://engine/9/path/public/sales?type=table&item_id=33",
		},
	})
	if err != nil {
		t.Fatalf("tableResourceRefFromRequest() error = %v", err)
	}

	if ref.EngineID != 9 || ref.SchemaName != "public" || ref.TableName != "sales" || ref.ItemID != 33 {
		t.Fatalf("table ref = %+v", ref)
	}
}

func TestTableResourceRefFromRequestRejectsLegacyTableIdentity(t *testing.T) {
	t.Parallel()

	_, err := tableResourceRefFromRequest(&serviceModels.CreateQueryServiceRequest{
		ConfigType: "table",
		SchemaName: "public",
		TableName:  "sales",
	})
	if err == nil {
		t.Fatal("tableResourceRefFromRequest() error = nil, want missing locator error")
	}
}

func TestQueryServiceUserDataConfigRejectsManagedSnapshotFields(t *testing.T) {
	t.Parallel()

	_, err := queryServiceUserDataConfig(map[string]interface{}{
		"geometry": map[string]interface{}{"column": "shape"},
	}, "table")
	if err == nil {
		t.Fatal("queryServiceUserDataConfig() error = nil, want managed field rejection")
	}
}

func TestMatchesFederatedObjectTable(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		reference string
		name      string
		fullName  string
		want      bool
	}{
		{reference: "sales", name: "sales", want: true},
		{reference: "sales_2026", name: "sales 2026", want: true},
		{reference: "public.sales", name: "sales", fullName: "public.sales", want: true},
		{reference: "public.sales", name: "sales", fullName: "bucket/public.sales", want: true},
		{reference: "customers", name: "sales", fullName: "public.sales", want: false},
	} {
		if got := matchesFederatedObjectTable(test.reference, test.name, test.fullName); got != test.want {
			t.Fatalf("matchesFederatedObjectTable(%q, %q, %q) = %t, want %t", test.reference, test.name, test.fullName, got, test.want)
		}
	}
}

func boundingBoxPtr(minX, minY, maxX, maxY float64) *datatype.BoundingBox {
	bbox := datatype.NewBoundingBox(minX, minY, maxX, maxY)
	return &bbox
}
