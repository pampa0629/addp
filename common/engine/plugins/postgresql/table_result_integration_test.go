package postgresql

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestIntegrationPostgresTableResultPersonDimensionUnionAggregation(t *testing.T) {
	db, pg, conn := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	ctx := t.Context()
	schema := fmt.Sprintf("table_result_person_%d", time.Now().UnixNano())
	must := func(query string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE SCHEMA "` + schema + `"`)
	defer db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
	prefix := `"` + schema + `".`
	must("CREATE TABLE " + prefix + "persons (person_id text,person_nickname text)")
	must("CREATE TABLE " + prefix + "activities (leader_person_id text,leader_nickname_snapshot text)")
	must("CREATE TABLE " + prefix + "members (person_id text,member_nickname_snapshot text)")
	must("CREATE TABLE " + prefix + "target (person_id text PRIMARY KEY,person_nickname text,person_record_source text)")
	must("INSERT INTO " + prefix + "persons VALUES ('1','甲')")
	must("INSERT INTO " + prefix + "activities VALUES ('1','活动快照'),('2','乙')")
	must("INSERT INTO " + prefix + "members VALUES ('2','成员快照')")
	query := strings.ReplaceAll(postgresPersonDimensionLineageQuery, "outdoor.", prefix)
	plan, err := pg.PrepareTableResult(ctx, conn, plugin.TableResultRequest{Query: plugin.QueryRequest{EngineID: 91, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true}}, Target: plugin.TabularItemPath(91, plugin.EngineCatalogTermSchema, schema, "target"), WriteMode: "overwrite"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := plan.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !result.FieldLineageComplete || result.RowsAffected != 2 || len(result.Sources) != 3 || len(result.FieldMappings) != 7 {
		t.Fatalf("person dimension result = %+v", result)
	}
	got := map[string]string{}
	for _, mapping := range result.FieldMappings {
		if mapping.Transformation == "generated" {
			if mapping.TargetField != "person_record_source" || mapping.SourceField != "" {
				t.Fatalf("generated mapping = %+v", mapping)
			}
			continue
		}
		parts := plugin.EngineCatalogPathWithoutRoot(mapping.SourcePath).Segments
		if len(parts) != 2 {
			t.Fatalf("unexpected source path: %+v", mapping.SourcePath)
		}
		got[parts[1].Name+"."+mapping.SourceField+"->"+mapping.TargetField] = mapping.Transformation
	}
	for _, expected := range []string{"persons.person_id->person_id", "members.person_id->person_id", "activities.leader_person_id->person_id", "persons.person_nickname->person_nickname", "activities.leader_nickname_snapshot->person_nickname", "members.member_nickname_snapshot->person_nickname"} {
		if got[expected] != "derived" {
			t.Fatalf("missing derived mapping %s in %#v", expected, got)
		}
	}
	rows, err := db.QueryContext(ctx, "SELECT person_id,person_nickname,person_record_source FROM "+prefix+"target ORDER BY person_id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for _, expected := range [][3]string{{"1", "甲", "persons"}, {"2", "乙", "activity_member_snapshot"}} {
		var actual [3]string
		if !rows.Next() {
			t.Fatal("missing person dimension row")
		}
		if err := rows.Scan(&actual[0], &actual[1], &actual[2]); err != nil || actual != expected {
			t.Fatalf("row = %v, want %v; error = %v", actual, expected, err)
		}
	}
	if rows.Next() || rows.Err() != nil {
		t.Fatalf("unexpected extra row or error: %v", rows.Err())
	}
}

func TestIntegrationPostgresTableResultPositionParametersAndConservativeProof(t *testing.T) {
	db, pg, conn := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	ctx := t.Context()
	schema := fmt.Sprintf("table_result_%d", time.Now().UnixNano())
	must := func(q string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE SCHEMA "` + schema + `"`)
	defer db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
	source := `"` + schema + `"."source"`
	target := `"` + schema + `"."target"`
	must("CREATE TABLE " + source + " (id integer,name text,amount numeric(8,2))")
	must("INSERT INTO " + source + " VALUES (1,'甲',10.25),(2,'乙',20.50)")
	must("CREATE TABLE " + target + " (key integer PRIMARY KEY,nickname text,widened bigint,constant text)")
	must("INSERT INTO " + target + " VALUES(99,'old',99,'old')")
	path := plugin.TabularItemPath(91, plugin.EngineCatalogTermSchema, schema, "target")
	params := map[string]interface{}{"minimum": 2, "label": "frozen"}
	req := plugin.TableResultRequest{Query: plugin.QueryRequest{EngineID: 91, Language: "sql", Query: "SELECT id AS nickname,name AS key,id,:label FROM " + source + " WHERE id>=:minimum", Options: plugin.QueryOptions{ReadOnly: true, Parameters: params}}, Target: path, WriteMode: "overwrite"}
	plan, err := pg.PrepareTableResult(ctx, conn, req)
	if err != nil {
		t.Fatal(err)
	}
	params["minimum"] = 99
	params["label"] = "mutated"
	req.Target.Segments[len(req.Target.Segments)-1].Name = "wrong_target"
	set, err := plan.ReadSet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Paths) != 1 {
		t.Fatalf("read set=%+v", set)
	}
	set.Paths[0].Segments[len(set.Paths[0].Segments)-1].Name = "wrong_source"
	result, err := plan.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsAffected != 1 || !result.FieldLineageComplete || len(result.FieldMappings) != 4 || len(result.TargetFields) != 4 || len(result.Sources) != 1 || len(result.Sources[0].Fields) != 3 {
		t.Fatalf("result=%+v", result)
	}
	expected := []struct{ source, target, kind string }{{"id", "key", "direct"}, {"name", "nickname", "direct"}, {"id", "widened", "derived"}, {"", "constant", "generated"}}
	for i, want := range expected {
		got := result.FieldMappings[i]
		if got.SourceField != want.source || got.TargetField != want.target || got.Transformation != want.kind {
			t.Fatalf("mapping=%+v want=%+v", got, want)
		}
	}
	var key int
	var name, label string
	if err := db.QueryRowContext(ctx, "SELECT key,nickname,constant FROM "+target).Scan(&key, &name, &label); err != nil {
		t.Fatal(err)
	}
	if key != 2 || name != "乙" || label != "frozen" {
		t.Fatalf("stored=%d %s %s", key, name, label)
	}
	if _, err := plan.Execute(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
		t.Fatalf("reuse error=%v", err)
	}
	if _, err := plan.ReadSet(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
		t.Fatalf("read after consume=%v", err)
	}
	// A valid native window query executes but cannot pretend to have complete proof.
	plan, err = pg.PrepareTableResult(ctx, conn, plugin.TableResultRequest{Query: plugin.QueryRequest{EngineID: 91, Language: "sql", Query: "SELECT id,name,id,(row_number() OVER (ORDER BY id))::text FROM " + source, Options: plugin.QueryOptions{ReadOnly: true}}, Target: plugin.TabularItemPath(91, plugin.EngineCatalogTermSchema, schema, "target"), WriteMode: "overwrite"})
	if err != nil {
		t.Fatal(err)
	}
	result, err = plan.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsAffected != 2 || result.FieldLineageComplete || len(result.FieldMappings) != 0 || len(result.Sources) != 1 {
		t.Fatalf("opaque result=%+v", result)
	}
}

func TestIntegrationPostgresTableResultDWD(t *testing.T) {
	db, pg, conn := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	schema := fmt.Sprintf("dwd_write_%d", time.Now().UnixNano())
	ctx := t.Context()
	must := func(q string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE SCHEMA "` + schema + `"`)
	defer db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
	prefix := `"` + schema + `".`
	for name, columns := range map[string]string{"members": "person_id text,activity_id text,member_status text", "activities": "activity_id text,current_leader_person_id text,is_effective_activity boolean,activity_date date,activity_intensity numeric", "persons": "person_id text,person_nickname text", "participation": "person_id text,activity_id text,member_status text,activity_date date,activity_intensity numeric,is_current_leader boolean,person_nickname text"} {
		must("CREATE TABLE " + prefix + name + " (" + columns + ")")
	}
	must("INSERT INTO " + prefix + "members VALUES('p1','a1','报名中')")
	must("INSERT INTO " + prefix + "activities VALUES('a1','p1',true,'2026-10-04',3.5)")
	must("INSERT INTO " + prefix + "persons VALUES('p1','测试昵称')")
	query := strings.ReplaceAll(postgresDWDLineageQuery, "outdoor.", prefix)
	plan, err := pg.PrepareTableResult(ctx, conn, plugin.TableResultRequest{Query: plugin.QueryRequest{EngineID: 91, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true}}, Target: plugin.TabularItemPath(91, plugin.EngineCatalogTermSchema, schema, "participation"), WriteMode: "overwrite"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := plan.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !result.FieldLineageComplete || result.RowsAffected != 1 || len(result.Sources) != 3 || len(result.FieldMappings) != 12 {
		t.Fatalf("DWD result=%+v", result)
	}
	found := false
	for _, mapping := range result.FieldMappings {
		if mapping.TargetField == "person_nickname" {
			if mapping.SourceField != "person_nickname" || mapping.Transformation != "direct" {
				t.Fatalf("nickname mapping=%+v", mapping)
			}
			found = true
		}
	}
	var nickname string
	if err := db.QueryRowContext(ctx, "SELECT person_nickname FROM "+prefix+"participation").Scan(&nickname); err != nil {
		t.Fatal(err)
	}
	if !found || nickname != "测试昵称" {
		t.Fatalf("nickname=%s proof=%v", nickname, found)
	}
}

func TestIntegrationPostgresTableResultRejectsChangedReadSetAndUnsafeTarget(t *testing.T) {
	db, pg, conn := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	ctx := t.Context()
	schema := fmt.Sprintf("write_guard_%d", time.Now().UnixNano())
	must := func(q string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE SCHEMA "` + schema + `"`)
	defer db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
	prefix := `"` + schema + `".`
	must("CREATE TABLE " + prefix + "a(id int)")
	must("CREATE TABLE " + prefix + "b(id int)")
	must("CREATE TABLE " + prefix + "target(id int)")
	must("INSERT INTO " + prefix + "target VALUES(99)")
	must("CREATE VIEW " + prefix + "v AS SELECT id FROM " + prefix + "a")
	prepare := func(query string) plugin.PreparedTableResult {
		t.Helper()
		plan, err := pg.PrepareTableResult(ctx, conn, plugin.TableResultRequest{Query: plugin.QueryRequest{EngineID: 91, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true}}, Target: plugin.TabularItemPath(91, plugin.EngineCatalogTermSchema, schema, "target"), WriteMode: "overwrite"})
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	plan := prepare("SELECT id FROM " + prefix + "v")
	if _, err := plan.ReadSet(ctx); err != nil {
		t.Fatal(err)
	}
	must("CREATE OR REPLACE VIEW " + prefix + "v AS SELECT id FROM " + prefix + "b")
	if result, err := plan.Execute(ctx); err == nil || result != nil {
		t.Fatalf("read-set drift result=%+v error=%v", result, err)
	}

	must("ALTER TABLE " + prefix + "target ENABLE ROW LEVEL SECURITY")
	plan = prepare("SELECT id FROM " + prefix + "a")
	if result, err := plan.Execute(ctx); err == nil || result != nil {
		t.Fatalf("target RLS result=%+v error=%v", result, err)
	}
	must("ALTER TABLE " + prefix + "target DISABLE ROW LEVEL SECURITY")
	must("CREATE FUNCTION " + prefix + "check_external(integer) RETURNS boolean LANGUAGE sql STABLE AS 'SELECT EXISTS(SELECT 1 FROM " + prefix + "b)'")
	must("ALTER TABLE " + prefix + "target ADD CONSTRAINT unproven_check CHECK (" + prefix + "check_external(id)) NOT VALID")
	plan = prepare("SELECT id FROM " + prefix + "a")
	if result, err := plan.Execute(ctx); err == nil || result != nil {
		t.Fatalf("target check read result=%+v error=%v", result, err)
	}
	must("ALTER TABLE " + prefix + "target DROP CONSTRAINT unproven_check")
	must("ALTER TABLE " + prefix + "b ADD PRIMARY KEY(id)")
	must("ALTER TABLE " + prefix + "target ADD CONSTRAINT external_fk FOREIGN KEY(id) REFERENCES " + prefix + "b(id) NOT VALID")
	plan = prepare("SELECT id FROM " + prefix + "a")
	if result, err := plan.Execute(ctx); result != nil || err == nil || !strings.Contains(err.Error(), "unresolved write-side effects") {
		t.Fatalf("target foreign key result=%+v error=%v", result, err)
	}
	must("ALTER TABLE " + prefix + "target DROP CONSTRAINT external_fk")
	must("CREATE FUNCTION " + prefix + "index_external(integer) RETURNS integer LANGUAGE sql IMMUTABLE AS 'SELECT count(*)::integer FROM " + prefix + "b'")
	must("CREATE INDEX external_index ON " + prefix + "target ((" + prefix + "index_external(id)))")
	plan = prepare("SELECT id FROM " + prefix + "a")
	if result, err := plan.Execute(ctx); result != nil || err == nil || !strings.Contains(err.Error(), "unresolved write-side effects") {
		t.Fatalf("target index function result=%+v error=%v", result, err)
	}
	must("DROP INDEX " + prefix + "external_index")
	must("ALTER TABLE " + prefix + "target ADD COLUMN calculated integer GENERATED ALWAYS AS (id+1) STORED")
	plan = prepare("SELECT id,id FROM " + prefix + "a")
	if result, err := plan.Execute(ctx); result != nil || err == nil || !strings.Contains(err.Error(), "unresolved write-side effects") {
		t.Fatalf("target generated column result=%+v error=%v", result, err)
	}
	must("ALTER TABLE " + prefix + "target DROP COLUMN calculated")
	must("ALTER TABLE " + prefix + "target ADD COLUMN omitted text DEFAULT 'must-not-be-used'")
	plan = prepare("SELECT id FROM " + prefix + "a")
	if result, err := plan.Execute(ctx); err == nil || result != nil {
		t.Fatalf("omitted target column result=%+v error=%v", result, err)
	}
	must("ALTER TABLE " + prefix + "target DROP COLUMN omitted")
	for _, query := range []string{"SELECT id FROM " + prefix + "target", "SELECT id,id FROM " + prefix + "a", "SELECT * FROM " + prefix + "a WHERE false"} {
		plan = prepare(query)
		if strings.Contains(query, "WHERE false") {
			must("CREATE FUNCTION " + prefix + "change_value() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN NEW.id := 7; RETURN NEW; END'")
			must("CREATE TRIGGER rewrite BEFORE INSERT ON " + prefix + "target FOR EACH ROW EXECUTE FUNCTION " + prefix + "change_value()")
		}
		if result, err := plan.Execute(ctx); err == nil || result != nil {
			t.Fatalf("unsafe write result=%+v error=%v", result, err)
		}
	}
	var id int
	if err := db.QueryRowContext(ctx, "SELECT id FROM "+prefix+"target").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != 99 {
		t.Fatalf("failed overwrite lost previous value=%d", id)
	}
}

var _ plugin.TableResultProvider = (*PostgreSQLPlugin)(nil)
