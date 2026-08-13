package oracle

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	pluginshared "github.com/addp/common/engine/plugins/shared"
	commonquery "github.com/addp/common/query"
	"github.com/google/uuid"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
)

func TestIntegrationOraclePartitionedTableChangeApply(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_INTEGRATION=1 to run Oracle integration test")
	}
	connInfo := oracleIntegrationConnInfo(t)
	p := &OraclePlugin{}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	schema := strings.ToUpper(connInfo["user"].(string))
	table := "ADDP_CDC_TARGET_" + strings.ToUpper(uuid.NewString()[:8])
	defer dropOracleApplyIntegrationTable(db, schema, table)
	path := oracleApplyIntegrationPath(schema, table)
	opts := oracleApplyIntegrationOptions()
	if err := p.PreparePartitionedTableChangeApply(ctx, connInfo, path, opts); err != nil {
		t.Fatalf("PreparePartitionedTableChangeApply: %v", err)
	}
	first := oracleApplyBatch("0", 0, 3,
		oracleApplyUpsert(1, 1, "first"),
		oracleApplyUpsert(2, 1, "latest"),
		oracleApplyUpsert(3, 2, "second"),
	)
	result, err := p.ApplyPartitionedTableChanges(ctx, connInfo, path, first, opts)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if result.AppliedRecords != 2 || result.SkippedRecords != 1 {
		t.Fatalf("first result=%#v", result)
	}
	assertOracleApplyRow(t, db, schema, table, 1, "latest")
	stale := oracleApplyBatch("0", 0, 2, oracleApplyUpsert(1, 1, "stale"), oracleApplyUpsert(2, 2, "stale"))
	result, err = p.ApplyPartitionedTableChanges(ctx, connInfo, path, stale, opts)
	if err != nil {
		t.Fatalf("stale apply: %v", err)
	}
	if result.AppliedRecords != 0 || result.SkippedRecords != 2 {
		t.Fatalf("stale result=%#v", result)
	}
	skip := plugin.PartitionedTableChange{Operation: plugin.TableChangeOperationSkip, Position: pluginshared.KafkaOffsetPosition("0", 4)}
	if _, err := p.ApplyPartitionedTableChanges(ctx, connInfo, path, oracleApplyBatch("0", 3, 4, skip), opts); err != nil {
		t.Fatalf("skip apply: %v", err)
	}
	deleted := plugin.PartitionedTableChange{
		Operation: plugin.TableChangeOperationDelete,
		Position:  pluginshared.KafkaOffsetPosition("0", 5),
		Row:       map[string]interface{}{"ID": int64(1)},
	}
	if _, err := p.ApplyPartitionedTableChanges(ctx, connInfo, path, oracleApplyBatch("0", 4, 5, deleted), opts); err != nil {
		t.Fatalf("delete apply: %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+commonquery.ForEngine("oracle").QualifiedTable(schema, table)+" WHERE \"ID\" = :1", 1).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("deleted row count=%d", count)
	}
	gap := oracleApplyBatch("0", 6, 7, oracleApplyUpsert(7, 3, "gap"))
	if _, err := p.ApplyPartitionedTableChanges(ctx, connInfo, path, gap, opts); err == nil || !strings.Contains(err.Error(), "ledger gap") {
		t.Fatalf("gap error=%v", err)
	}
	drift := opts
	drift.SourceIdentity = "addp://engine/22/path/BUSINESS/OTHER?type=table"
	if _, err := p.ApplyPartitionedTableChanges(ctx, connInfo, path, oracleApplyBatch("0", 5, 6, oracleApplyUpsert(6, 3, "drift")), drift); err == nil || !strings.Contains(err.Error(), "identity drift") {
		t.Fatalf("identity drift error=%v", err)
	}
}

func TestIntegrationOraclePartitionedApplyRollbackAndCancel(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_INTEGRATION=1 to run Oracle integration test")
	}
	connInfo := oracleIntegrationConnInfo(t)
	p := &OraclePlugin{}
	dsn, _ := p.BuildDSN(connInfo)
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := strings.ToUpper(connInfo["user"].(string))
	table := "ADDP_CDC_TARGET_" + strings.ToUpper(uuid.NewString()[:8])
	defer dropOracleApplyIntegrationTable(db, schema, table)
	path := oracleApplyIntegrationPath(schema, table)
	opts := oracleApplyIntegrationOptions()
	ctx := context.Background()
	if err := p.PreparePartitionedTableChangeApply(ctx, connInfo, path, opts); err != nil {
		t.Fatal(err)
	}
	invalid := plugin.PartitionedTableChange{
		Operation: plugin.TableChangeOperationUpsert,
		Position:  pluginshared.KafkaOffsetPosition("0", 1),
		Row:       map[string]interface{}{"ID": int64(1), "NAME": nil},
	}
	if _, err := p.ApplyPartitionedTableChanges(ctx, connInfo, path, oracleApplyBatch("0", 0, 1, invalid), opts); err == nil {
		t.Fatal("NULL business write unexpectedly succeeded")
	}
	qualifiedLedger := commonquery.ForEngine("oracle").QualifiedTable(schema, oracleTransferApplyLedgerTable)
	var ledgerCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+qualifiedLedger+" WHERE apply_identity = :1", opts.ApplyIdentity).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 0 {
		t.Fatalf("rolled-back ledger rows=%d", ledgerCount)
	}
	if _, err := p.ApplyPartitionedTableChanges(ctx, connInfo, path, oracleApplyBatch("0", 0, 1, oracleApplyUpsert(1, 1, "created")), opts); err != nil {
		t.Fatal(err)
	}
	lockTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var offset int64
	if err := lockTx.QueryRow("SELECT next_offset FROM "+qualifiedLedger+" WHERE apply_identity = :1 AND partition_key = :2 FOR UPDATE", opts.ApplyIdentity, "0").Scan(&offset); err != nil {
		_ = lockTx.Rollback()
		t.Fatal(err)
	}
	applyCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	_, applyErr := p.ApplyPartitionedTableChanges(applyCtx, connInfo, path, oracleApplyBatch("0", 1, 2, oracleApplyUpsert(2, 2, "blocked")), opts)
	cancel()
	if applyErr == nil {
		_ = lockTx.Rollback()
		t.Fatal("apply succeeded while ledger was locked")
	}
	if err := lockTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyPartitionedTableChanges(ctx, connInfo, path, oracleApplyBatch("0", 1, 2, oracleApplyUpsert(2, 2, "recovered")), opts); err != nil {
		t.Fatalf("apply after lock release: %v", err)
	}
}

func TestIntegrationOraclePartitionedApplySpatialEWKB(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_SPATIAL_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_SPATIAL_INTEGRATION=1 to run Oracle Spatial integration test")
	}
	connInfo := oracleIntegrationConnInfo(t)
	p := &OraclePlugin{}
	dsn, _ := p.BuildDSN(connInfo)
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := strings.ToUpper(connInfo["user"].(string))
	table := "ADDP_SPATIAL_TARGET_" + strings.ToUpper(uuid.NewString()[:8])
	defer dropOracleApplyIntegrationTable(db, schema, table)
	path := oracleApplyIntegrationPath(schema, table)
	srid, dimension := 4326, 2
	opts := plugin.PartitionedTableChangeApplyOptions{
		ApplyIdentity: uuid.NewString(), SourceIdentity: "addp://engine/22/path/BUSINESS/SPATIAL_FEATURES?type=table",
		Fields: []datatype.FieldInfo{
			{Name: "ID", Type: datatype.FieldTypeBigInt, Nullable: false},
			{Name: "SHAPE", Type: datatype.FieldTypeGeometry, Nullable: false},
		},
		Keys: []string{"ID"},
		SpatialInfo: &datatype.SpatialInfo{GeometryColumns: []datatype.GeometryColumnInfo{{
			Name: "SHAPE", GeometryType: string(datatype.GeometryTypeGeometry), SRID: &srid, Dimension: &dimension,
		}}, PrimaryGeometryColumn: "SHAPE"},
	}
	if err := p.PreparePartitionedTableChangeApply(context.Background(), connInfo, path, opts); err != nil {
		t.Fatal(err)
	}
	point := geom.NewPointFlat(geom.XY, []float64{116.4, 39.9}).SetSRID(srid)
	polygon := geom.NewPolygonFlat(geom.XY, []float64{
		0, 0, 10, 0, 10, 10, 0, 10, 0, 0,
	}, []int{10}).SetSRID(srid)
	multi := geom.NewMultiPolygon(geom.XY).SetSRID(srid)
	if err := multi.Push(polygon); err != nil {
		t.Fatal(err)
	}
	secondPolygon := geom.NewPolygonFlat(geom.XY, []float64{
		20, 20, 30, 20, 30, 30, 20, 30, 20, 20,
	}, []int{10}).SetSRID(srid)
	if err := multi.Push(secondPolygon); err != nil {
		t.Fatal(err)
	}
	geometries := []geom.T{point, polygon, multi}
	changes := make([]plugin.PartitionedTableChange, 0, len(geometries))
	for i, geometry := range geometries {
		encoded, err := ewkb.Marshal(geometry, ewkb.NDR)
		if err != nil {
			t.Fatal(err)
		}
		changes = append(changes, plugin.PartitionedTableChange{
			Operation: plugin.TableChangeOperationUpsert,
			Position:  pluginshared.KafkaOffsetPosition("0", int64(i+1)),
			Row:       map[string]interface{}{"ID": int64(i + 1), "SHAPE": encoded},
		})
	}
	if _, err := p.ApplyPartitionedTableChanges(context.Background(), connInfo, path, oracleApplyBatch("0", 0, 3, changes...), opts); err != nil {
		t.Fatal(err)
	}
	qualified := commonquery.ForEngine("oracle").QualifiedTable(schema, table)
	rows, err := db.Query("SELECT target_row.\"ID\", target_row.\"SHAPE\".SDO_GTYPE, target_row.\"SHAPE\".SDO_SRID FROM " + qualified + " target_row ORDER BY target_row.\"ID\"")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantGTypes := []int{2001, 2003, 2007}
	index := 0
	for rows.Next() {
		var id int64
		var gtype, gotSRID int
		if err := rows.Scan(&id, &gtype, &gotSRID); err != nil {
			t.Fatal(err)
		}
		if gtype != wantGTypes[index] || gotSRID != srid {
			t.Fatalf("row %d gtype=%d srid=%d", id, gtype, gotSRID)
		}
		index++
	}
	if index != len(wantGTypes) {
		t.Fatalf("spatial rows=%d", index)
	}
}

func TestIntegrationOraclePartitionedApplyScalarTypeMatrix(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_INTEGRATION=1 to run Oracle integration test")
	}
	connInfo := oracleIntegrationConnInfo(t)
	p := &OraclePlugin{}
	dsn, _ := p.BuildDSN(connInfo)
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := strings.ToUpper(connInfo["user"].(string))
	table := "ADDP_TYPES_TARGET_" + strings.ToUpper(uuid.NewString()[:8])
	defer dropOracleApplyIntegrationTable(db, schema, table)
	path := oracleApplyIntegrationPath(schema, table)
	opts := plugin.PartitionedTableChangeApplyOptions{
		ApplyIdentity: uuid.NewString(), SourceIdentity: "addp://engine/12/path/public/type_matrix?type=table",
		Fields: []datatype.FieldInfo{
			{Name: "ID", Type: datatype.FieldTypeBigInt, Nullable: false},
			{Name: "TEXT_VALUE", Type: datatype.FieldTypeString, Nullable: false},
			{Name: "INT_VALUE", Type: datatype.FieldTypeInt, Nullable: false},
			{Name: "FLOAT_VALUE", Type: datatype.FieldTypeFloat, Nullable: false},
			{Name: "DOUBLE_VALUE", Type: datatype.FieldTypeDouble, Nullable: false},
			{Name: "DECIMAL_VALUE", Type: datatype.FieldTypeDecimal, Precision: 28, Scale: 9, Nullable: false},
			{Name: "BOOL_VALUE", Type: datatype.FieldTypeBool, Nullable: false},
			{Name: "DATE_VALUE", Type: datatype.FieldTypeDate, Nullable: false},
			{Name: "TIMESTAMP_VALUE", Type: datatype.FieldTypeTimestamp, Nullable: false},
			{Name: "JSON_VALUE", Type: datatype.FieldTypeJSON, Nullable: false},
			{Name: "UUID_VALUE", Type: datatype.FieldTypeUUID, Nullable: false},
			{Name: "BYTES_VALUE", Type: datatype.FieldTypeBytes, Nullable: false},
		},
		Keys: []string{"ID"},
	}
	if err := p.PreparePartitionedTableChangeApply(context.Background(), connInfo, path, opts); err != nil {
		t.Fatal(err)
	}
	wantTimestamp := time.Date(2026, 8, 13, 11, 22, 33, 444_000_000, time.UTC)
	change := plugin.PartitionedTableChange{
		Operation: plugin.TableChangeOperationUpsert,
		Position:  pluginshared.KafkaOffsetPosition("0", 1),
		Row: map[string]interface{}{
			"ID": int64(1), "TEXT_VALUE": "oracle target", "INT_VALUE": int32(42),
			"FLOAT_VALUE": float64(1.25), "DOUBLE_VALUE": float64(9.5),
			"DECIMAL_VALUE": "1234567890123456789.123456789", "BOOL_VALUE": true,
			"DATE_VALUE": "2026-08-13", "TIMESTAMP_VALUE": wantTimestamp,
			"JSON_VALUE": `{"enabled":true,"name":"oracle"}`, "UUID_VALUE": "eef1d731-ea11-449d-8a55-c930d75db0c2",
			"BYTES_VALUE": []byte{0, 1, 2, 255},
		},
	}
	if _, err := p.ApplyPartitionedTableChanges(context.Background(), connInfo, path, oracleApplyBatch("0", 0, 1, change), opts); err != nil {
		t.Fatal(err)
	}
	qualified := commonquery.ForEngine("oracle").QualifiedTable(schema, table)
	query := "SELECT \"TEXT_VALUE\", \"INT_VALUE\", \"FLOAT_VALUE\", \"DOUBLE_VALUE\", " +
		"TO_CHAR(\"DECIMAL_VALUE\", 'FM99999999999999999999999999999999999999D999999999', 'NLS_NUMERIC_CHARACTERS=''.,'''), " +
		"\"BOOL_VALUE\", TO_CHAR(\"DATE_VALUE\", 'YYYY-MM-DD'), " +
		"TO_CHAR(\"TIMESTAMP_VALUE\", 'YYYY-MM-DD HH24:MI:SS.FF3'), JSON_SERIALIZE(\"JSON_VALUE\" RETURNING VARCHAR2), " +
		"\"UUID_VALUE\", DBMS_LOB.GETLENGTH(\"BYTES_VALUE\") FROM " + qualified + " WHERE \"ID\" = :1"
	var textValue, decimalValue, dateValue, timestampValue, jsonValue, uuidValue string
	var intValue int32
	var floatValue, doubleValue float64
	var boolValue bool
	var bytesLength int
	if err := db.QueryRow(query, 1).Scan(
		&textValue, &intValue, &floatValue, &doubleValue, &decimalValue, &boolValue,
		&dateValue, &timestampValue, &jsonValue, &uuidValue, &bytesLength,
	); err != nil {
		t.Fatal(err)
	}
	if textValue != "oracle target" || intValue != 42 || floatValue != 1.25 || doubleValue != 9.5 ||
		decimalValue != "1234567890123456789.123456789" || !boolValue || dateValue != "2026-08-13" ||
		timestampValue != "2026-08-13 11:22:33.444" || jsonValue != `{"enabled":true,"name":"oracle"}` ||
		uuidValue != "eef1d731-ea11-449d-8a55-c930d75db0c2" || bytesLength != 4 {
		t.Fatalf("Oracle type matrix row mismatch: text=%q int=%d float=%v double=%v decimal=%q bool=%v date=%q timestamp=%q json=%q uuid=%q bytes=%d",
			textValue, intValue, floatValue, doubleValue, decimalValue, boolValue, dateValue, timestampValue, jsonValue, uuidValue, bytesLength)
	}
}

func oracleApplyIntegrationOptions() plugin.PartitionedTableChangeApplyOptions {
	return plugin.PartitionedTableChangeApplyOptions{
		ApplyIdentity: uuid.NewString(), SourceIdentity: "addp://engine/22/path/BUSINESS/ORDERS?type=table",
		Fields: []datatype.FieldInfo{
			{Name: "ID", Type: datatype.FieldTypeBigInt, Nullable: false},
			{Name: "NAME", Type: datatype.FieldTypeString, Nullable: false},
		},
		Keys: []string{"ID"},
	}
}

func oracleApplyIntegrationPath(schema, table string) plugin.CatalogPath {
	return plugin.CatalogPath{
		Version: plugin.CatalogPathVersion,
		Segments: []plugin.CatalogSegment{
			{Term: plugin.CatalogTermSchema, Kind: plugin.CatalogKindNamespace, Name: schema},
			{Term: plugin.CatalogTermTable, Kind: plugin.CatalogKindTable, Name: table},
		},
	}
}

func oracleApplyBatch(partition string, start, end int64, changes ...plugin.PartitionedTableChange) *plugin.PartitionedTableChangeBatch {
	return &plugin.PartitionedTableChangeBatch{
		Partition: partition, StartPosition: pluginshared.KafkaOffsetPosition(partition, start),
		EndPosition: pluginshared.KafkaOffsetPosition(partition, end), Changes: changes,
	}
}

func oracleApplyUpsert(nextOffset, id int64, name string) plugin.PartitionedTableChange {
	return plugin.PartitionedTableChange{
		Operation: plugin.TableChangeOperationUpsert,
		Position:  pluginshared.KafkaOffsetPosition("0", nextOffset),
		Row:       map[string]interface{}{"ID": id, "NAME": name},
	}
}

func assertOracleApplyRow(t *testing.T, db *sql.DB, schema, table string, id int64, wantName string) {
	t.Helper()
	var name string
	query := "SELECT \"NAME\" FROM " + commonquery.ForEngine("oracle").QualifiedTable(schema, table) + " WHERE \"ID\" = :1"
	if err := db.QueryRow(query, id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != wantName {
		t.Fatalf("row %d name=%q want=%q", id, name, wantName)
	}
}

func dropOracleApplyIntegrationTable(db *sql.DB, schema, table string) {
	_, _ = db.Exec("DROP TABLE " + commonquery.ForEngine("oracle").QualifiedTable(schema, table) + " PURGE")
}
