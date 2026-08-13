package oracle

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
)

func TestIntegrationOracleCatalogAndRead(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_INTEGRATION=1 to run Oracle integration test")
	}

	connInfo := oracleIntegrationConnInfo(t)
	p := &OraclePlugin{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := p.TestConnection(ctx, connInfo); err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}

	root := plugin.CatalogRootPath(p.CatalogModel(), 92001)
	schemas, err := p.ListChildren(ctx, connInfo, root, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(root) error = %v", err)
	}
	businessSchema := findOracleCatalogEntry(schemas, strings.ToUpper(oracleIntegrationEnv("ADDP_TEST_ORACLE_USER", "ORACLE_APP_USER", "business")))
	if businessSchema == nil {
		t.Fatalf("business schema not found in %#v", oracleCatalogEntryNames(schemas))
	}
	if pdbAdmin := findOracleCatalogEntry(schemas, "PDBADMIN"); pdbAdmin != nil {
		t.Fatalf("PDBADMIN management schema must be filtered: %#v", oracleCatalogEntryNames(schemas))
	}

	items, err := p.ListChildren(ctx, connInfo, businessSchema.Path, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(schema) error = %v", err)
	}
	orders := findOracleCatalogEntry(items, "ORDERS")
	if orders == nil || orders.Kind != plugin.CatalogKindTable {
		t.Fatalf("ORDERS table not found in %#v", oracleCatalogEntryNames(items))
	}
	if summary := findOracleCatalogEntry(items, "ORDER_SUMMARY"); summary == nil || summary.Kind != "view" {
		t.Fatalf("ORDER_SUMMARY view not found in %#v", oracleCatalogEntryNames(items))
	}

	facts, err := p.DescribeCatalogFacts(ctx, connInfo, orders.Path, plugin.CatalogFactsOptions{
		IncludeStatistics:  true,
		IncludeIndexes:     true,
		IncludeConstraints: true,
	})
	if err != nil {
		t.Fatalf("DescribeCatalogFacts() error = %v", err)
	}
	if facts.Table == nil || facts.Table.RowCount == nil || *facts.Table.RowCount != 2 {
		t.Fatalf("ORDERS facts = %#v, want row_count=2", facts.Table)
	}
	assertOracleIntegrationField(t, facts.Table.Fields, "ID", datatype.FieldTypeBigInt, true)
	assertOracleIntegrationField(t, facts.Table.Fields, "AMOUNT", datatype.FieldTypeDecimal, false)
	assertOracleIntegrationField(t, facts.Table.Fields, "PAYLOAD", datatype.FieldTypeBytes, false)
	if findOracleIndexFacts(facts.Indexes, "ORDERS_ORDERED_AT_IDX") == nil {
		t.Fatalf("ORDERS indexes = %#v, want ORDERS_ORDERED_AT_IDX", facts.Indexes)
	}
	foreignKey := findOracleConstraintFacts(facts.Constraints, "ORDERS_CUSTOMER_FK")
	if foreignKey == nil || foreignKey.ConstraintType != plugin.ConstraintTypeForeignKey || foreignKey.ReferencedTable != "CUSTOMERS" {
		t.Fatalf("ORDERS constraints = %#v", facts.Constraints)
	}

	orderEvents := findOracleCatalogEntry(items, "ORDER_EVENTS")
	if orderEvents == nil {
		t.Fatalf("ORDER_EVENTS partitioned table not found in %#v", oracleCatalogEntryNames(items))
	}
	partitionFacts, err := p.DescribeCatalogFacts(ctx, connInfo, orderEvents.Path, plugin.CatalogFactsOptions{IncludePartitioning: true})
	if err != nil {
		t.Fatalf("DescribeCatalogFacts(ORDER_EVENTS) error = %v", err)
	}
	if partitionFacts.Partitioning == nil || partitionFacts.Partitioning.Strategy != "range" || strings.Join(partitionFacts.Partitioning.KeyFields, ",") != "EVENT_TIME" || partitionFacts.Partitioning.PartitionCount != 2 {
		t.Fatalf("ORDER_EVENTS partitioning = %#v", partitionFacts.Partitioning)
	}

	batch, err := p.ReadBatch(ctx, connInfo, orders.Path, plugin.BatchReadOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ReadBatch() error = %v", err)
	}
	if len(batch.Rows) != 2 || len(batch.Fields) != 7 {
		t.Fatalf("ReadBatch() rows=%d fields=%d", len(batch.Rows), len(batch.Fields))
	}
	if amount, ok := batch.Rows[0]["AMOUNT"].(string); !ok || amount == "" {
		t.Fatalf("AMOUNT = %#v, want lossless decimal string", batch.Rows[0]["AMOUNT"])
	}

	result, err := p.ExecuteSQL(ctx, connInfo,
		"SELECT ORDER_NO FROM ORDERS WHERE ID = :id",
		plugin.QueryOptions{ReadOnly: true, Limit: 1, Parameters: map[string]interface{}{"id": 1001}},
	)
	if err != nil {
		t.Fatalf("ExecuteSQL() error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["ORDER_NO"] != "ORD-1001" {
		t.Fatalf("ExecuteSQL() rows = %#v", result.Rows)
	}
}

func TestIntegrationOracleSDEWorkspaceNotDetected(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_INTEGRATION=1 to run Oracle integration test")
	}

	p := &OraclePlugin{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resolved, err := p.ResolveCapabilities(ctx, oracleIntegrationConnInfo(t), p.Capabilities())
	if err != nil {
		t.Fatalf("ResolveCapabilities() error = %v", err)
	}
	workspaces, err := plugin.SpatialWorkspacesFromExtensions(resolved.Extensions)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 1 || workspaces[0].Ecosystem != "arcgis" || workspaces[0].Kind != plugin.SpatialWorkspaceArcGISSDE ||
		workspaces[0].State != plugin.SpatialWorkspaceStateNotDetected || workspaces[0].CanEnable {
		t.Fatalf("Oracle SDE workspace = %#v, want not_detected", workspaces)
	}
	if resolved.Storage == nil || resolved.Storage.Store == nil || resolved.Storage.Store.ChangeStreamRead != nil {
		t.Fatalf("Oracle SDE probe must not change ordinary store capabilities: %#v", resolved.Storage)
	}
}

func TestIntegrationOracleSpatialFactsAndRead(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_SPATIAL_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_SPATIAL_INTEGRATION=1 to run Oracle Spatial integration test")
	}

	connInfo := oracleIntegrationConnInfo(t)
	p := &OraclePlugin{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	root := plugin.CatalogRootPath(p.CatalogModel(), 92001)
	schemas, err := p.ListChildren(ctx, connInfo, root, plugin.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	businessSchema := findOracleCatalogEntry(schemas, strings.ToUpper(oracleIntegrationEnv("ADDP_TEST_ORACLE_USER", "ORACLE_APP_USER", "business")))
	if businessSchema == nil {
		t.Fatalf("business schema not found in %#v", oracleCatalogEntryNames(schemas))
	}
	items, err := p.ListChildren(ctx, connInfo, businessSchema.Path, plugin.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, internalTableName := range oracleSpatialSecondaryTableNames(t, connInfo) {
		if internalTable := findOracleCatalogEntry(items, internalTableName); internalTable != nil {
			t.Fatalf("Oracle domain-index secondary table %q must be filtered: %#v", internalTableName, oracleCatalogEntryNames(items))
		}
	}
	locations := findOracleCatalogEntry(items, "CUSTOMER_LOCATIONS")
	if locations == nil {
		t.Fatalf("CUSTOMER_LOCATIONS not found in %#v", oracleCatalogEntryNames(items))
	}
	facts, err := p.DescribeCatalogFacts(ctx, connInfo, locations.Path, plugin.CatalogFactsOptions{
		IncludeSpatialFacts: true,
		IncludeIndexes:      true,
	})
	if err != nil {
		t.Fatalf("DescribeCatalogFacts(CUSTOMER_LOCATIONS) error = %v", err)
	}
	assertOracleIntegrationField(t, facts.Table.Fields, "SHAPE", datatype.FieldTypeGeometry, false)
	if facts.Spatial == nil || facts.Spatial.PrimaryGeometryName() != "SHAPE" || facts.Spatial.PrimaryGeometryType() != "Point" || facts.Spatial.PrimarySRIDValue() != 4326 {
		t.Fatalf("Oracle spatial facts = %#v", facts.Spatial)
	}
	if facts.Spatial.HasSpatialIndex == nil || !*facts.Spatial.HasSpatialIndex || !strings.EqualFold(facts.Spatial.IndexName, "CUSTOMER_LOCATIONS_SHAPE_SIDX") {
		t.Fatalf("Oracle spatial index facts = %#v", facts.Spatial)
	}

	batch, err := p.ReadBatch(ctx, connInfo, locations.Path, plugin.BatchReadOptions{
		Limit: 10,
		Hints: map[string]interface{}{plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB)},
	})
	if err != nil {
		t.Fatalf("ReadBatch(CUSTOMER_LOCATIONS) error = %v", err)
	}
	if len(batch.Rows) != 2 || batch.Spatial == nil || batch.Spatial.PrimarySRIDValue() != 4326 {
		t.Fatalf("Oracle spatial batch = %#v", batch)
	}
	geometryEWKB, ok := batch.Rows[0]["SHAPE"].([]byte)
	if !ok {
		t.Fatalf("Oracle spatial row geometry = %#v, want []byte EWKB", batch.Rows[0]["SHAPE"])
	}
	geometry, err := ewkb.Unmarshal(geometryEWKB)
	if err != nil || geometry.SRID() != 4326 {
		t.Fatalf("Oracle spatial row geometry = %#v, error = %v", geometry, err)
	}

	feature, err := p.ReadSpatialFeature(ctx, connInfo, locations.Path, plugin.SpatialFeatureReadOptions{
		GeometryField: "SHAPE",
		IdentityField: "ID",
		IdentityValue: 1,
	})
	if err != nil {
		t.Fatalf("ReadSpatialFeature() error = %v", err)
	}
	if feature == nil || feature.SRID != 4326 || len(feature.GeometryEWKB) == 0 || len(feature.CentroidEWKB) == 0 {
		t.Fatalf("Oracle spatial feature = %#v", feature)
	}

	spatialFeatures := findOracleCatalogEntry(items, "SPATIAL_FEATURES")
	if spatialFeatures == nil {
		t.Fatalf("SPATIAL_FEATURES not found in %#v", oracleCatalogEntryNames(items))
	}
	spatialFeatureFacts, err := p.DescribeCatalogFacts(ctx, connInfo, spatialFeatures.Path, plugin.CatalogFactsOptions{IncludeSpatialFacts: true})
	if err != nil {
		t.Fatalf("DescribeCatalogFacts(SPATIAL_FEATURES) error = %v", err)
	}
	if spatialFeatureFacts.Spatial == nil || spatialFeatureFacts.Spatial.PrimaryGeometryType() != string(datatype.GeometryTypeGeometry) || spatialFeatureFacts.Spatial.PrimarySRIDValue() != 4326 {
		t.Fatalf("SPATIAL_FEATURES spatial facts = %#v", spatialFeatureFacts.Spatial)
	}
	spatialFeatureBatch, err := p.ReadBatch(ctx, connInfo, spatialFeatures.Path, plugin.BatchReadOptions{
		Limit: 10,
		Hints: map[string]interface{}{plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB)},
	})
	if err != nil {
		t.Fatalf("ReadBatch(SPATIAL_FEATURES) error = %v", err)
	}
	wantGeometryTypes := map[int64]string{
		1: "LineString",
		2: "Polygon",
		3: "MultiPoint",
		4: "MultiLineString",
		5: "MultiPolygon",
		6: "GeometryCollection",
	}
	if len(spatialFeatureBatch.Rows) != len(wantGeometryTypes) {
		t.Fatalf("SPATIAL_FEATURES row count = %d, want %d", len(spatialFeatureBatch.Rows), len(wantGeometryTypes))
	}
	for _, row := range spatialFeatureBatch.Rows {
		id, ok := row["ID"].(int64)
		if !ok {
			t.Fatalf("SPATIAL_FEATURES ID = %#v, want int64", row["ID"])
		}
		encoded, ok := row["SHAPE"].([]byte)
		if !ok {
			t.Fatalf("SPATIAL_FEATURES SHAPE = %#v, want []byte EWKB", row["SHAPE"])
		}
		geometry, err := ewkb.Unmarshal(encoded)
		if err != nil {
			t.Fatalf("decode SPATIAL_FEATURES row %d EWKB: %v", id, err)
		}
		if geometry.SRID() != 4326 || oracleGeometryTypeName(geometry) != wantGeometryTypes[id] {
			t.Fatalf("SPATIAL_FEATURES row %d geometry = %T SRID=%d, want %s SRID=4326", id, geometry, geometry.SRID(), wantGeometryTypes[id])
		}
	}
}

func oracleSpatialSecondaryTableNames(t *testing.T, connInfo plugin.ConnectionInfo) []string {
	t.Helper()
	p := &OraclePlugin{}
	db, err := p.CreateConnectionPool(connInfo, nil)
	if err != nil {
		t.Fatalf("CreateConnectionPool() error = %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB() error = %v", err)
	}
	defer sqlDB.Close()

	var names []string
	err = db.Raw(`
		SELECT secondary_object_name
		  FROM all_secondary_objects
		 WHERE index_owner = USER
		   AND index_name IN ('CUSTOMER_LOCATIONS_SHAPE_SIDX', 'SPATIAL_FEATURES_SHAPE_SIDX')
		   AND secondary_object_owner = USER
		 ORDER BY secondary_object_name
	`).Scan(&names).Error
	if err != nil {
		t.Fatalf("query Oracle spatial secondary objects error = %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("Oracle spatial secondary object count = %d, want 2: %#v", len(names), names)
	}
	return names
}

func oracleGeometryTypeName(geometry geom.T) string {
	switch geometry.(type) {
	case *geom.Point:
		return "Point"
	case *geom.LineString:
		return "LineString"
	case *geom.Polygon:
		return "Polygon"
	case *geom.MultiPoint:
		return "MultiPoint"
	case *geom.MultiLineString:
		return "MultiLineString"
	case *geom.MultiPolygon:
		return "MultiPolygon"
	case *geom.GeometryCollection:
		return "GeometryCollection"
	default:
		return ""
	}
}

func findOracleIndexFacts(indexes []plugin.IndexFacts, name string) *plugin.IndexFacts {
	for index := range indexes {
		if strings.EqualFold(indexes[index].Name, name) {
			return &indexes[index]
		}
	}
	return nil
}

func findOracleConstraintFacts(constraints []plugin.ConstraintFacts, name string) *plugin.ConstraintFacts {
	for index := range constraints {
		if strings.EqualFold(constraints[index].Name, name) {
			return &constraints[index]
		}
	}
	return nil
}

func oracleIntegrationConnInfo(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	portText := oracleIntegrationEnv("ADDP_TEST_ORACLE_PORT", "ORACLE_PORT", "15210")
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("invalid Oracle integration port %q: %v", portText, err)
	}
	return plugin.ConnectionInfo{
		"host":         oracleIntegrationEnv("ADDP_TEST_ORACLE_HOST", "", "127.0.0.1"),
		"port":         port,
		"service_name": oracleIntegrationEnv("ADDP_TEST_ORACLE_SERVICE_NAME", "ORACLE_SERVICE_NAME", "FREEPDB1"),
		"user":         oracleIntegrationEnv("ADDP_TEST_ORACLE_USER", "ORACLE_APP_USER", "business"),
		"password":     oracleIntegrationEnv("ADDP_TEST_ORACLE_PASSWORD", "ORACLE_APP_PASSWORD", "business_oracle_password"),
	}
}

func oracleIntegrationEnv(primary, secondary, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(primary)); value != "" {
		return value
	}
	if secondary != "" {
		if value := strings.TrimSpace(os.Getenv(secondary)); value != "" {
			return value
		}
	}
	return fallback
}

func findOracleCatalogEntry(entries []plugin.CatalogEntry, name string) *plugin.CatalogEntry {
	for index := range entries {
		if strings.EqualFold(entries[index].Name, name) {
			return &entries[index]
		}
	}
	return nil
}

func oracleCatalogEntryNames(entries []plugin.CatalogEntry) []string {
	names := make([]string, len(entries))
	for index := range entries {
		names[index] = entries[index].Name
	}
	return names
}

func assertOracleIntegrationField(t *testing.T, fields []datatype.FieldInfo, name string, fieldType datatype.FieldType, primaryKey bool) {
	t.Helper()
	for _, field := range fields {
		if field.Name == name {
			if field.Type != fieldType || field.PrimaryKey != primaryKey {
				t.Fatalf("field %s = %#v, want type=%s primary_key=%v", name, field, fieldType, primaryKey)
			}
			return
		}
	}
	t.Fatalf("field %s not found in %#v", name, fields)
}
