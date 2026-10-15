package oracle

import (
	"context"
	"database/sql"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func testOracleNonSpatialDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	info := oracleIntegrationConnInfo(t)
	p := &OraclePlugin{}
	dsn, err := p.BuildDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := strings.ToUpper(plugin.GetString(info, "user"))
	suffix := strings.ToUpper(uuid.NewString()[:8])
	// Distinct quoted names must never be folded or interpolated into catalog SQL.
	for _, table := range []string{"ADDP_DEL_" + suffix, "addp_del_" + strings.ToLower(suffix), "ADDP_Del_'_" + suffix} {
		path := plugin.TabularItemPath(93001, plugin.EngineCatalogTermSchema, schema, table)
		qualified := commonquery.ForDialect("oracle").QualifiedTable(schema, table)
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME=:1", table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("occupied fixture %q: %d %v", table, count, err)
		}
		// Cleanup owns only this non-spatial synthetic table even when the
		// regression intentionally fails under the old DeleteResource path.
		t.Cleanup(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			connection, err := sql.Open("oracle", dsn)
			if err != nil {
				t.Error(err)
				return
			}
			defer connection.Close()
			var residue int
			if err := connection.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME=:1", table).Scan(&residue); err != nil {
				t.Error(err)
				return
			}
			if residue != 0 {
				if _, err := connection.ExecContext(cleanup, "DROP TABLE "+qualified+" PURGE"); err != nil {
					t.Error(err)
				}
			}
			if err := connection.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME=:1", table).Scan(&residue); err != nil || residue != 0 {
				t.Errorf("delete fixture residue %q: %d %v", table, residue, err)
			}
		})
		fields := []datatype.FieldInfo{{Name: "ID", Type: datatype.FieldTypeBigInt, PrimaryKey: true}, {Name: "DOCUMENT", Type: datatype.FieldTypeJSON, Nullable: true}, {Name: "PAYLOAD", Type: datatype.FieldTypeBytes, Nullable: true}}
		if err := p.PrepareTableWrite(ctx, info, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
			t.Fatal(err)
		}
		session, err := p.OpenTableWriteSession(ctx, info, path, plugin.TableWriteSessionOptions{Fields: fields})
		if err != nil {
			t.Fatal(err)
		}
		if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{{"ID": int64(1), "DOCUMENT": map[string]interface{}{"sentinel": true}, "PAYLOAD": []byte{0, 255}}}}); err != nil {
			_ = session.Abort(ctx)
			t.Fatal(err)
		}
		if err := session.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if err := p.DeleteResource(ctx, info, path); err != nil {
			t.Fatalf("non-spatial delete %q: %v", table, err)
		}
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME=:1", table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("target was not removed %q: %d %v", table, count, err)
		}
	}
}

// A native object column conservatively needs metadata. Use an owned ordinary
// object type and an owned metadata view to exercise error ordering and scoped
// cleanup on real Oracle without claiming native Spatial certification.
func testOracleDeleteMetadataGuard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p := &OraclePlugin{}
	info := oracleIntegrationConnInfo(t)
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
	typeName, target, scalar, source := "ADDP_DT_"+suffix, "ADDP_DO_"+suffix, "ADDP_DS_"+suffix, "ADDP_DM_"+suffix
	dialect := commonquery.ForDialect("oracle")
	qualified := func(name string) string { return dialect.QualifiedTable(schema, name) }
	for _, name := range []string{typeName, target, scalar, source, "USER_SDO_GEOM_METADATA"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME=:1", name).Scan(&count); err != nil || count != 0 {
			t.Fatalf("occupied metadata fixture %q: %d %v", name, count, err)
		}
	}
	type ownedObject struct{ kind, name string }
	var created []ownedObject
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for i := len(created) - 1; i >= 0; i-- {
			object := created[i]
			var count int
			if err := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME=:1 AND OBJECT_TYPE=:2", object.name, object.kind).Scan(&count); err != nil {
				t.Error(err)
				continue
			}
			if count > 0 {
				q := "DROP " + object.kind + " " + qualified(object.name)
				if object.kind == "TABLE" {
					q += " PURGE"
				}
				if _, err := db.ExecContext(cleanup, q); err != nil {
					t.Error(err)
				}
			}
			if err := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME=:1", object.name).Scan(&count); err != nil || count != 0 {
				t.Errorf("metadata fixture residue %q: %d %v", object.name, count, err)
			}
		}
	})
	create := func(kind, name, ddl string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
		created = append(created, ownedObject{kind, name})
	}
	create("TYPE", typeName, "CREATE TYPE "+qualified(typeName)+" AS OBJECT (VALUE_NUMBER NUMBER)")
	create("TABLE", target, "CREATE TABLE "+qualified(target)+" (ID NUMBER(18,0) PRIMARY KEY, PAYLOAD "+qualified(typeName)+")")
	create("TABLE", scalar, "CREATE TABLE "+qualified(scalar)+" (ID NUMBER(18,0))")
	metadataDDL := "CREATE TABLE " + qualified(source) + " (TABLE_NAME VARCHAR2(128), COLUMN_NAME VARCHAR2(128))"
	create("TABLE", source, metadataDDL)
	viewDDL := "VIEW " + qualified("USER_SDO_GEOM_METADATA") + " AS SELECT TABLE_NAME, COLUMN_NAME FROM " + qualified(source)
	create("VIEW", "USER_SDO_GEOM_METADATA", "CREATE "+viewDDL)
	if _, err := db.ExecContext(ctx, "INSERT INTO "+qualified(target)+" VALUES (1,"+qualified(typeName)+"(7))"); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := db.QueryRowContext(ctx, "SELECT OBJECT_ID FROM USER_OBJECTS WHERE OBJECT_NAME=:1 AND OBJECT_TYPE='TABLE'", target).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE "+qualified(source)+" PURGE"); err != nil {
		t.Fatal(err)
	}
	path := func(name string) plugin.EngineCatalogPath {
		return plugin.TabularItemPath(93001, plugin.EngineCatalogTermSchema, schema, name)
	}
	// A broken metadata view is irrelevant for a proven scalar table.
	if err := p.DeleteResource(ctx, info, path(scalar)); err != nil {
		t.Fatalf("scalar delete touched invalid metadata: %v", err)
	}
	var absent int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME=:1", scalar).Scan(&absent); err != nil || absent != 0 {
		t.Fatalf("scalar delete left a target: %d %v", absent, err)
	}
	if err := p.DeleteResource(ctx, info, path(target)); err == nil || !strings.Contains(err.Error(), "check Oracle delete spatial metadata") {
		t.Fatalf("object delete did not fail before DROP: %v", err)
	}
	var after int64
	if err := db.QueryRowContext(ctx, "SELECT OBJECT_ID FROM USER_OBJECTS WHERE OBJECT_NAME=:1 AND OBJECT_TYPE='TABLE'", target).Scan(&after); err != nil || after != before {
		t.Fatalf("failed preflight changed target: %d %v", after, err)
	}
	var rows int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified(target)+" WHERE ID=1").Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("failed preflight changed sentinel: %d %v", rows, err)
	}
	// Repair only the owned view, then prove exact metadata cleanup succeeds.
	if _, err := db.ExecContext(ctx, metadataDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE OR REPLACE "+viewDDL); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{target, "OTHER_" + suffix} {
		if _, err := db.ExecContext(ctx, "INSERT INTO "+qualified(source)+" VALUES (:1,'PAYLOAD')", name); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.DeleteResource(ctx, info, path(target)); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME=:1", target).Scan(&absent); err != nil || absent != 0 {
		t.Fatalf("object delete left a target: %d %v", absent, err)
	}
	for _, tc := range []struct {
		name string
		want int
	}{{target, 0}, {"OTHER_" + suffix, 1}} {
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified(source)+" WHERE TABLE_NAME=:1", tc.name).Scan(&rows); err != nil || rows != tc.want {
			t.Fatalf("metadata scope %q: %d %v", tc.name, rows, err)
		}
	}
}
