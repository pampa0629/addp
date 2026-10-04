package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

type tableResultProtectionRecorder struct {
	allowDevelopProtectionGate
	denied string
	paths  []string
	active int
}

func (g *tableResultProtectionRecorder) BeginCatalogPath(_ context.Context, _ uint, _ plugin.EnginePlugin, path plugin.EngineCatalogPath) (func(), error) {
	parts := plugin.EngineCatalogPathWithoutRoot(path).Segments
	name := parts[len(parts)-1].Name
	g.paths = append(g.paths, name)
	if name == g.denied {
		return nil, errors.New("protected source denied")
	}
	g.active++
	return func() { g.active-- }, nil
}

func TestTableResultOwnerProtectionAndPersistedFieldFactsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("DEVELOP_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("DEVELOP_POSTGRES_TEST_DSN is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	schema := fmt.Sprintf("develop_write_%d", time.Now().UnixNano())
	must := func(q string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE SCHEMA "` + schema + `"`)
	defer db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
	prefix := `"` + schema + `".`
	must("CREATE TABLE " + prefix + "a(id integer,name text)")
	must("INSERT INTO " + prefix + "a VALUES(1,'甲')")
	must("CREATE TABLE " + prefix + "b(id integer)")
	must("INSERT INTO " + prefix + "b VALUES(1)")
	must("CREATE TABLE " + prefix + "target(person_id integer,person_nickname text,generated text)")
	must("INSERT INTO " + prefix + "target VALUES(99,'old','old')")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	engine := &commonModels.Engine{ID: 91, EngineType: "postgresql", ConnectionInfo: commonModels.ConnectionInfo{"host": u.Hostname(), "port": u.Port(), "user": u.User.Username(), "password": password, "database": strings.TrimPrefix(u.Path, "/"), "sslmode": "disable"}}
	target := &resourcetree.ResourceLocator{EngineID: 91, Path: []string{schema, "target"}, Type: resourcetree.TypeTable}
	query := "SELECT a.id AS wrong_name,a.name,:label FROM " + prefix + "a JOIN " + prefix + "b USING(id)"
	gate := &tableResultProtectionRecorder{denied: "b"}
	service := &SQLEngineService{protectionGate: gate}
	result, err := service.executeTableResult(ctx, 7, engine, target, query, map[string]interface{}{"label": "created"}, "overwrite")
	if err == nil || result != nil || gate.active != 0 || strings.Join(gate.paths, ",") != "a,b" {
		t.Fatalf("denied result=%+v err=%v gate=%+v", result, err, gate)
	}
	var id int
	if err := db.QueryRowContext(ctx, "SELECT person_id FROM "+prefix+"target").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != 99 {
		t.Fatal("protected read changed target")
	}
	gate.denied = ""
	gate.paths = nil
	result, err = service.executeTableResult(ctx, 7, engine, target, query, map[string]interface{}{"label": "created"}, "overwrite")
	if err != nil {
		t.Fatal(err)
	}
	if gate.active != 0 || len(gate.paths) != 2 || result.RowsAffected != 1 {
		t.Fatalf("gate=%+v result=%+v", gate, result)
	}
	inputs := map[string]string{"people": (&resourcetree.ResourceLocator{EngineID: 91, Path: []string{schema, "a"}, Type: resourcetree.TypeTable}).ToURI(), "membership": (&resourcetree.ResourceLocator{EngineID: 91, Path: []string{schema, "b"}, Type: resourcetree.TypeTable}).ToURI()}
	metadata, err := tableResultExecutionMetadata("verified", inputs, target.ToURI(), "overwrite", result)
	if err != nil {
		t.Fatal(err)
	}
	facts := metadata["lineage_facts"].(*commonExecution.LineageFacts)
	if len(facts.Inputs) != 2 || len(facts.Outputs) != 1 || facts.Operations[0].FieldLineageStatus != "complete" || len(facts.Operations[0].FieldMappings) != 3 {
		t.Fatalf("facts=%+v", facts)
	}
	for _, ref := range append(facts.Inputs, facts.Outputs...) {
		if err := ref.SchemaSnapshot.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	mapping := facts.Operations[0].FieldMappings[1]
	if mapping.InputPort != "input.people" || mapping.SourceField != "name" || mapping.TargetField != "person_nickname" || mapping.Transformation != "direct" {
		t.Fatalf("mapping=%+v", mapping)
	}
	if facts.Operations[0].FieldMappings[2].Transformation != "generated" {
		t.Fatalf("generated evidence=%+v", facts.Operations[0])
	}
	var nickname string
	if err := db.QueryRowContext(ctx, "SELECT person_nickname FROM "+prefix+"target").Scan(&nickname); err != nil {
		t.Fatal(err)
	}
	if nickname != "甲" {
		t.Fatalf("stored nickname=%s", nickname)
	}
}
