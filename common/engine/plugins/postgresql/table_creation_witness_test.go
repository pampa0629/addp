package postgresql

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

// T1 simulates a failed native catalog query using the already available SQLite
// transaction driver. Real PG creation/COPY/error behavior belongs to T2.
func TestPostgresOptionalCreationObservationCannotPoisonWriter(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), "CREATE TABLE actual_result (id bigint)"); err != nil {
		t.Fatal(err)
	}
	if witness := observePostgresCreationWitness(t.Context(), tx, "main", "actual_result", false); witness != nil {
		t.Fatal("failed optional native query yielded a witness")
	}
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO actual_result VALUES (1)"); err != nil {
		t.Fatal("optional query poisoned the actual write", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresCreationWitnessRequiresBoundedSameCreation(t *testing.T) {
	created := postgresCreationWitness{SchemaVersion: postgresCreationWitnessSchema, EngineID: 2,
		Cluster: "123456", ServerStart: time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC),
		Database: 7, Namespace: 8, Relation: 9, CatalogXmin: 100, Transaction: 100, Schema: "a.b", Table: "c"}
	for _, scenario := range []string{"same", "cluster", "restart", "engine", "database", "namespace", "relation", "catalog_version", "schema", "table", "xmin_wrap", "reverse_time", "not_creation", "missing", "unknown_cluster"} {
		t.Run(scenario, func(t *testing.T) {
			a, b := created, created
			b.Transaction++
			switch scenario {
			case "cluster":
				b.Cluster = "999"
			case "restart":
				b.ServerStart = b.ServerStart.Add(time.Second)
			case "engine":
				b.EngineID++
			case "database":
				b.Database++
			case "namespace":
				b.Namespace++
			case "relation":
				b.Relation++
			case "catalog_version":
				b.CatalogXmin++
			case "schema":
				b.Schema = "a"
			case "table":
				b.Table = "b.c"
			case "xmin_wrap":
				b.Transaction += uint64(1) << 32
			case "reverse_time":
				b.Transaction = 99
			case "not_creation":
				a.CatalogXmin++
			case "missing":
				a.EngineID = 0
			case "unknown_cluster":
				a.Cluster = ""
			}
			if got := samePostgresCreatedTable(&a, &b); got != (scenario == "same") {
				t.Fatalf("same-created-table=%v for %s", got, scenario)
			}
		})
	}
	if samePostgresCreatedTable(nil, &created) || samePostgresCreatedTable(&created, nil) {
		t.Fatal("missing observations became evidence")
	}
	encoded := postgresCreationWitnessJSON(&created)
	if got := decodePostgresCreationWitness(encoded); got == nil || *got != created {
		t.Fatal("private handoff round trip failed")
	}
	for _, invalid := range []string{"", "{}", encoded + " {}", strings.Repeat(" ", 4097), strings.Replace(encoded, postgresCreationWitnessSchema, "retired/v0", 1), strings.Replace(encoded, `"transaction":100`, `"transaction":101`, 1)} {
		if decodePostgresCreationWitness(invalid) != nil {
			t.Fatal("invalid private handoff accepted")
		}
	}
	public, err := json.Marshal(plugin.TableTargetPreparation{Disposition: plugin.TableTargetCreated, CreationWitness: encoded})
	if err != nil || strings.Contains(string(public), "cluster") || strings.Contains(string(public), encoded) {
		t.Fatal("private native evidence leaked into public serialization")
	}
}
