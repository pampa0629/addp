package mysql

import (
	"context"
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestIntegrationMySQLDataProtectionReadContracts(t *testing.T) {
	db, mysqlPlugin, serverConnInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)

	ctx := context.Background()
	qualifiedTable := mysqlDialect().QualifiedTable(database, "customers")
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualifiedTable+" (id BIGINT NOT NULL, email VARCHAR(255) NOT NULL, PRIMARY KEY (id)) ENGINE=InnoDB"); err != nil {
		t.Fatalf("create protected source: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+qualifiedTable+" (id, email) VALUES (1, 'customer@example.com')"); err != nil {
		t.Fatalf("seed protected source: %v", err)
	}

	const engineID = uint(93)
	connInfo := make(plugin.ConnectionInfo, len(serverConnInfo)+1)
	for key, value := range serverConnInfo {
		connInfo[key] = value
	}
	connInfo["database"] = database
	path := plugin.EngineCatalogBranchLeafPath(
		mysqlPlugin.EngineCatalogModel(), engineID,
		plugin.EngineCatalogTermDatabase, database,
		plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, "customers",
	)

	facts, err := mysqlPlugin.DescribeEngineCatalogFacts(ctx, connInfo, path, plugin.EngineCatalogFactsOptions{})
	if err != nil || facts == nil || facts.Table == nil || len(facts.Table.Fields) != 2 {
		t.Fatalf("describe protected source facts = %#v, error = %v", facts, err)
	}
	batch, err := mysqlPlugin.ReadBatch(ctx, connInfo, path, plugin.BatchReadOptions{Limit: 10})
	if err != nil {
		t.Fatalf("read protected source batch: %v", err)
	}
	if len(batch.Rows) != 1 || batch.Rows[0]["email"] != "customer@example.com" {
		t.Fatalf("source batch = %#v", batch.Rows)
	}
	sampleQuery, sampleLanguage := mysqlPlugin.GenerateSampleQuery(ctx, connInfo, plugin.SampleQueryOptions{Path: path})
	if sampleLanguage != "sql" || sampleQuery != "SELECT `id`, `email` FROM `"+database+"`.`customers` LIMIT 10" {
		t.Fatalf("protected sample query = %q, %q", sampleQuery, sampleLanguage)
	}
	samplePrepared, err := mysqlPlugin.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: engineID, Language: sampleLanguage, Query: sampleQuery, TargetPath: &path,
		Options: plugin.QueryOptions{EngineID: engineID, EngineType: mysqlPlugin.Type(), ReadOnly: true, Limit: 10},
	})
	if err != nil {
		t.Fatalf("prepare protected sample query: %v", err)
	}
	if _, err := samplePrepared.OutputLineage(ctx); err != nil {
		t.Fatalf("resolve protected sample query output lineage: %v", err)
	}

	prepared, err := mysqlPlugin.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: engineID,
		Language: "sql",
		Query:    "SELECT id, email AS contact_email FROM " + qualifiedTable + " ORDER BY id",
		Options: plugin.QueryOptions{
			EngineID: engineID, EngineType: mysqlPlugin.Type(), ReadOnly: true,
		},
	})
	if err != nil {
		t.Fatalf("prepare protected query: %v", err)
	}
	readSet, err := prepared.ReadSet(ctx)
	if err != nil {
		t.Fatalf("resolve protected query read set: %v", err)
	}
	if len(readSet.Paths) != 1 || !reflect.DeepEqual(readSet.Paths[0], path) {
		t.Fatalf("read set paths = %#v, want %#v", readSet.Paths, path)
	}
	lineage, err := prepared.OutputLineage(ctx)
	if err != nil {
		t.Fatalf("resolve protected query output lineage: %v", err)
	}
	if len(lineage.Sources) != 1 || len(lineage.Sources[0].Bindings) != 2 {
		t.Fatalf("output lineage = %#v", lineage)
	}
	bindings := lineage.Sources[0].Bindings
	if len(bindings[0].SourcePath) != 1 || bindings[0].SourcePath[0] != "id" ||
		len(bindings[0].OutputPath) != 1 || bindings[0].OutputPath[0] != "id" ||
		len(bindings[1].SourcePath) != 1 || bindings[1].SourcePath[0] != "email" ||
		len(bindings[1].OutputPath) != 1 || bindings[1].OutputPath[0] != "contact_email" {
		t.Fatalf("output bindings = %#v", bindings)
	}
	result, err := prepared.Execute(ctx)
	if err != nil {
		t.Fatalf("execute protected query plan: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["contact_email"] != "customer@example.com" {
		t.Fatalf("query result = %#v", result.Rows)
	}

	servicePlan, err := mysqlPlugin.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: engineID,
		Language: "sql",
		Query:    "SELECT addp_source.id, addp_source.email FROM (SELECT id, email FROM " + qualifiedTable + ") AS addp_source ORDER BY addp_source.id LIMIT 2",
		Options: plugin.QueryOptions{
			EngineID: engineID, EngineType: mysqlPlugin.Type(), ReadOnly: true, Limit: 2,
		},
	})
	if err != nil {
		t.Fatalf("prepare Service-shaped protected query plan: %v", err)
	}
	serviceLineage, err := servicePlan.OutputLineage(ctx)
	if err != nil {
		t.Fatalf("resolve Service-shaped protected query output lineage: %v", err)
	}
	if len(serviceLineage.Sources) != 1 || len(serviceLineage.Sources[0].Bindings) != 2 {
		t.Fatalf("Service-shaped output lineage = %#v", serviceLineage)
	}
}
