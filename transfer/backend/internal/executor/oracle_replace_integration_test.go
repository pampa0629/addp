package executor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/oracle"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resume"
	"github.com/google/uuid"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
)

// Only the standard disposable Oracle gate enables this test. The source is
// synthetic; the real Transfer executor and Oracle target execute all effects.
func TestIntegrationTransferOracleReplace(t *testing.T) {
	if os.Getenv("ADDP_TRANSFER_ORACLE_REPLACE_INTEGRATION") != "1" {
		t.Skip("run make test-common-oracle-decimal to verify Oracle replace")
	}
	info := plugin.ConnectionInfo{}
	for key, variable := range map[string]string{
		"host": "ADDP_TEST_ORACLE_HOST", "service_name": "ADDP_TEST_ORACLE_SERVICE_NAME",
		"user": "ADDP_TEST_ORACLE_USER", "password": "ADDP_TEST_ORACLE_PASSWORD",
	} {
		value := os.Getenv(variable)
		if value == "" {
			t.Fatalf("%s is required by the disposable Oracle gate", variable)
		}
		info[key] = value
	}
	port, err := strconv.Atoi(os.Getenv("ADDP_TEST_ORACLE_PORT"))
	if err != nil || port <= 0 {
		t.Fatal("ADDP_TEST_ORACLE_PORT must be explicit and positive")
	}
	info["port"] = port
	kinds := []string{"scalar"}
	if os.Getenv("ADDP_ORACLE_SPATIAL_INTEGRATION") == "1" {
		kinds = append(kinds, "spatial")
	}
	for _, kind := range kinds {
		for _, scenario := range []string{"commit", "empty", "definition_rejected", "drop_rejected", "invalid_json", "duplicate_key", "abort_after_batch"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				testOracleReplace(t, info, kind == "spatial", scenario)
			})
		}
	}
}

func testOracleReplace(t *testing.T, info plugin.ConnectionInfo, spatial bool, scenario string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	p := &oracle.OraclePlugin{}
	dsn, err := p.BuildDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	schema := strings.ToUpper(plugin.GetString(info, "user"))
	suffix := strings.ToUpper(uuid.NewString()[:8])
	target, sibling, child := "ADDP_RT_"+suffix, "ADDP_RS_"+suffix, "ADDP_RC_"+suffix
	dialect := commonquery.ForDialect("oracle")
	qualified := func(name string) string { return dialect.QualifiedTable(schema, name) }
	path := func(name string) plugin.EngineCatalogPath {
		return plugin.TabularItemPath(93002, plugin.EngineCatalogTermSchema, schema, name)
	}
	var owned []string
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Reverse order drops the owned referencing child before either parent.
		for i := len(owned) - 1; i >= 0; i-- {
			name := owned[i]
			var count int
			if err := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME=:1", name).Scan(&count); err != nil {
				t.Error(err)
				continue
			}
			if count != 0 {
				if _, err := db.ExecContext(cleanup, "DROP TABLE "+qualified(name)+" PURGE"); err != nil {
					t.Error(err)
				}
			}
			if os.Getenv("ADDP_ORACLE_SPATIAL_INTEGRATION") == "1" {
				if _, err := db.ExecContext(cleanup, "DELETE FROM USER_SDO_GEOM_METADATA WHERE TABLE_NAME=:1", name); err != nil {
					t.Error(err)
				}
				if err := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_SDO_GEOM_METADATA WHERE TABLE_NAME=:1", name).Scan(&count); err != nil || count != 0 {
					t.Errorf("metadata residue %s: %d %v", name, count, err)
				}
			}
			if err := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME=:1", name).Scan(&count); err != nil || count != 0 {
				t.Errorf("object residue %s: %d %v", name, count, err)
			}
		}
	})
	oldFields := []datatype.FieldInfo{{Name: "ID", Type: datatype.FieldTypeBigInt, PrimaryKey: true}, {Name: "LEGACY", Type: datatype.FieldTypeString}}
	fields := []datatype.FieldInfo{{Name: "ID", Type: datatype.FieldTypeBigInt, PrimaryKey: true}, {Name: "DOCUMENT", Type: datatype.FieldTypeJSON}, {Name: "PAYLOAD", Type: datatype.FieldTypeBytes}}
	var oldSpatial, newSpatial *datatype.SpatialInfo
	var point []byte
	if spatial {
		oldFields = append(oldFields, datatype.FieldInfo{Name: "OLD_SHAPE", Type: datatype.FieldTypeGeometry})
		fields = append(fields, datatype.FieldInfo{Name: "LOCATION", Type: datatype.FieldTypeGeometry})
		oldSpatial = datatype.NewSingleGeometrySpatialInfo("OLD_SHAPE", "Point", 4326, 2)
		newSpatial = datatype.NewSingleGeometrySpatialInfo("LOCATION", "Point", 4326, 2)
		extent := datatype.NewBoundingBox(-180, -90, 180, 90)
		oldSpatial.Extent, newSpatial.Extent = &extent, &extent
		point, err = ewkb.Marshal(geom.NewPointFlat(geom.XY, []float64{116, 39}).SetSRID(4326), ewkb.NDR)
		if err != nil {
			t.Fatal(err)
		}
	}
	objectID := func(name string) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, "SELECT OBJECT_ID FROM USER_OBJECTS WHERE OBJECT_NAME=:1 AND OBJECT_TYPE='TABLE'", name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	for _, name := range []string{target, sibling, child} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME=:1", name).Scan(&count); err != nil || count != 0 {
			t.Fatalf("fixture occupied %s: %d %v", name, count, err)
		}
		if os.Getenv("ADDP_ORACLE_SPATIAL_INTEGRATION") == "1" {
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_SDO_GEOM_METADATA WHERE TABLE_NAME=:1", name).Scan(&count); err != nil || count != 0 {
				t.Fatalf("metadata fixture occupied %s: %d %v", name, count, err)
			}
		}
	}
	for _, name := range []string{target, sibling} {
		// Ownership was checked above; register cleanup before multi-step DDL.
		owned = append(owned, name)
		if err := p.PrepareTableWrite(ctx, info, path(name), plugin.TableWriteOptions{Fields: oldFields, SpatialInfo: oldSpatial}); err != nil {
			t.Fatal(err)
		}
		session, err := p.OpenTableWriteSession(ctx, info, path(name), plugin.TableWriteSessionOptions{Fields: oldFields, SpatialInfo: oldSpatial})
		if err != nil {
			t.Fatal(err)
		}
		row := map[string]interface{}{"ID": int64(99), "LEGACY": "old sentinel"}
		if spatial {
			row["OLD_SHAPE"] = point
		}
		if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{row}}); err != nil {
			_ = session.Abort(ctx)
			t.Fatal(err)
		}
		if err := session.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
	before, siblingBefore := objectID(target), objectID(sibling)
	if scenario == "drop_rejected" {
		owned = append(owned, child)
		if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified(child)+" (PARENT_ID NUMBER(19,0) REFERENCES "+qualified(target)+"(ID))"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO "+qualified(child)+" VALUES (99)"); err != nil {
			t.Fatal(err)
		}
	}
	row := func(id int64) map[string]interface{} {
		value := map[string]interface{}{"ID": id, "DOCUMENT": map[string]interface{}{"members": []interface{}{true, "合成", nil}}, "PAYLOAD": []byte{0, 255, 65}}
		if spatial {
			value["LOCATION"] = point
		}
		return value
	}
	rows := []map[string]interface{}{row(1), row(2)}
	if scenario == "invalid_json" {
		rows[1]["DOCUMENT"] = json.RawMessage(`{`)
	}
	if scenario == "duplicate_key" {
		rows[1]["ID"] = int64(1)
	}
	if scenario == "empty" {
		rows = nil
	}
	if scenario == "definition_rejected" {
		fields = append(fields, datatype.FieldInfo{Name: "BAD_ARRAY", Type: datatype.FieldTypeArray})
	}
	reader := &fakeBatchReader{}
	for index, value := range rows {
		reader.batches = append(reader.batches, &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{value}, Offset: int64(index)})
	}
	executor := &TableTransferExecutor{SourceNativeReader: reader, SourceTableSessionProvider: reader,
		TargetDeleteProvider: p, TargetNativePreparer: p, TargetTableSessionProvider: p}
	var marker *resume.Marker
	final := false
	metrics, executeErr := executor.Execute(ctx, TableTransferPlan{
		Source:    TableSourcePlan{Kind: TableEndpointNative, TableInfo: &datatype.TableInfo{Fields: fields}, SpatialInfo: newSpatial},
		Target:    TableTargetPlan{Kind: TableEndpointNative, ConnInfo: info, Path: path(target), DeleteBeforeWrite: true, TableWrite: plugin.BatchWriteOptions{Method: "copy"}},
		BatchSize: 1,
		ProgressCallback: func(_ context.Context, event TableProgressEvent) error {
			if event.Final {
				final, marker = true, event.CommitMarker
			}
			if scenario == "abort_after_batch" && !event.Final {
				return errors.New("synthetic abort after first batch")
			}
			return nil
		},
	})
	wantError := map[string]string{"definition_rejected": "before target effects", "drop_rejected": "delete native table target", "invalid_json": "DOCUMENT", "duplicate_key": "ORA-00001", "abort_after_batch": "synthetic abort"}[scenario]
	if (executeErr != nil) != (wantError != "") || (wantError != "" && !strings.Contains(executeErr.Error(), wantError)) {
		t.Fatalf("unexpected replace result: metrics=%+v error=%v expected=%q", metrics, executeErr, wantError)
	}
	if !reader.sessionClosed {
		t.Fatal("source session was not closed")
	}
	preserved := scenario == "definition_rejected" || scenario == "drop_rejected"
	if objectID(sibling) != siblingBefore {
		t.Fatal("replace changed sibling identity")
	}
	if (objectID(target) == before) != preserved {
		t.Fatal("replace target identity does not match DDL outcome")
	}
	assertTable := func(name string, old bool, wantRows int) {
		t.Helper()
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified(name)).Scan(&count); err != nil || count != wantRows {
			t.Fatalf("actual committed rows %s: %d %v, expected %d", name, count, err, wantRows)
		}
		column, absent := "DOCUMENT", "LEGACY"
		if old {
			column, absent = absent, column
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified(name)+" WHERE ID=99 AND LEGACY='old sentinel'").Scan(&count); err != nil || count != 1 {
				t.Fatalf("old sentinel changed: %d %v", count, err)
			}
		}
		for _, check := range []struct {
			column string
			want   int
		}{{column, 1}, {absent, 0}} {
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_TAB_COLUMNS WHERE TABLE_NAME=:1 AND COLUMN_NAME=:2", name, check.column).Scan(&count); err != nil || count != check.want {
				t.Fatalf("target structure %s.%s: %d %v", name, check.column, count, err)
			}
		}
		if spatial {
			geometry, absent := "LOCATION", "OLD_SHAPE"
			if old {
				geometry, absent = absent, geometry
			}
			for _, check := range []struct {
				column string
				want   int
			}{{geometry, 1}, {absent, 0}} {
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_SDO_GEOM_METADATA WHERE TABLE_NAME=:1 AND COLUMN_NAME=:2 AND SRID=4326", name, check.column).Scan(&count); err != nil || count != check.want {
					t.Fatalf("spatial registration %s.%s: %d %v", name, check.column, count, err)
				}
			}
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_INDEXES WHERE TABLE_NAME=:1 AND INDEX_TYPE='DOMAIN' AND STATUS='VALID'", name).Scan(&count); err != nil || count != 1 {
				t.Fatalf("spatial index %s: %d %v", name, count, err)
			}
		}
	}
	assertTable(sibling, true, 1)
	if scenario == "drop_rejected" {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified(child)+" WHERE PARENT_ID=99").Scan(&count); err != nil || count != 1 {
			t.Fatalf("referencing child changed: %d %v", count, err)
		}
	}
	wantRows := 0
	if scenario == "commit" {
		wantRows = 2
	}
	if preserved {
		wantRows = 1
	}
	assertTable(target, preserved, wantRows)
	if executeErr != nil {
		if final || marker != nil {
			t.Fatal("failed replace emitted a successful final marker")
		}
		if !preserved && metrics.RecordsWritten != 1 {
			t.Fatalf("expected attempted progress before rollback, got %+v", metrics)
		}
		t.Logf("failed replace: old_table_preserved=%v committed_rows=%d progress=%+v", preserved, wantRows, metrics)
		return
	}
	if !final || marker == nil || marker.Provider != "oracle.table_write_session" || metrics.RecordsWritten != int64(wantRows) {
		t.Fatalf("successful replace lacks commit evidence: final=%v marker=%+v metrics=%+v", final, marker, metrics)
	}
	if wantRows == 0 {
		return
	}
	if spatial {
		var minType, maxType, minSRID, maxSRID int
		if err := db.QueryRowContext(ctx, `SELECT MIN(r."LOCATION".SDO_GTYPE), MAX(r."LOCATION".SDO_GTYPE), MIN(r."LOCATION".SDO_SRID), MAX(r."LOCATION".SDO_SRID) FROM `+qualified(target)+" r").Scan(&minType, &maxType, &minSRID, &maxSRID); err != nil || minType != 2001 || maxType != 2001 || minSRID != 4326 || maxSRID != 4326 {
			t.Fatalf("geometry values: type=%d/%d SRID=%d/%d error=%v", minType, maxType, minSRID, maxSRID, err)
		}
	}
	result, err := db.QueryContext(ctx, "SELECT ID, JSON_SERIALIZE(DOCUMENT RETURNING VARCHAR2(4000)), RAWTOHEX(DBMS_LOB.SUBSTR(PAYLOAD,3,1)) FROM "+qualified(target)+" ORDER BY ID")
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	index := 0
	for result.Next() {
		var id int64
		var document, payload string
		if err := result.Scan(&id, &document, &payload); err != nil {
			t.Fatal(err)
		}
		var actual interface{}
		if err := json.Unmarshal([]byte(document), &actual); err != nil {
			t.Fatal(err)
		}
		if id != int64(index+1) || payload != "00FF41" || !reflect.DeepEqual(actual, row(id)["DOCUMENT"]) {
			t.Fatalf("typed round trip: %d %s %s", id, document, payload)
		}
		index++
	}
	if err := result.Err(); err != nil || index != 2 {
		t.Fatalf("round trip rows=%d error=%v", index, err)
	}
}
