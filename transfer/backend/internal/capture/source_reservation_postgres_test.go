package capture

import (
	"context"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegrationPostgresCaptureReservation(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard PostgreSQL gate")
	}
	info := testpg.ConnInfoFromEnv(t)
	db, err := openPostgreSQL(info)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	schema, pub, slot := "cdc_reservation_"+suffix, "cdc_pub_"+suffix, "cdc_slot_"+suffix
	exec := func(query string) {
		t.Helper()
		if _, e := db.ExecContext(ctx, query); e != nil {
			t.Fatal(e)
		}
	}
	exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema))
	t.Cleanup(func() {
		if _, e := db.ExecContext(ctx, "DROP PUBLICATION IF EXISTS "+pq.QuoteIdentifier(pub)); e != nil {
			t.Error(e)
		}
		if _, e := db.ExecContext(ctx, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE"); e != nil {
			t.Error(e)
		}
	})
	source := pq.QuoteIdentifier(schema) + ".source"
	foreign := pq.QuoteIdentifier(schema) + ".foreign_table"
	exec("CREATE TABLE " + source + " (id integer PRIMARY KEY)")
	exec("CREATE TABLE " + foreign + " (id integer PRIMARY KEY)")
	resource := &models.CaptureResource{SourceType: models.CaptureSourcePostgreSQL, SourceDatabase: info["database"].(string), SourceSchema: schema, SourceTable: "source", SourceConnectionFingerprint: "fixture", PostgreSQL: &models.PostgreSQLCaptureResource{SlotName: slot, PublicationName: pub}}
	plan := &CapturePlan{SourceType: models.CaptureSourcePostgreSQL, SourceConnInfo: info, SourceConnectionFingerprint: "fixture"}
	resource.PostgreSQL.SlotOwned, resource.PostgreSQL.PublicationOwned = true, true
	value := readPostgreSQLObservation(ctx, db, resource, time.Now(), time.Minute)
	if value.Status != "missing" || value.RetainedWALBytes != nil {
		t.Fatalf("missing task slot observation %+v", value)
	}
	// A real, inactive physical fixture slot must never be associated as a task logical slot.
	if _, err = db.ExecContext(ctx, "SELECT pg_create_physical_replication_slot($1)", slot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, e := db.ExecContext(ctx, "SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_name=$1", slot); e != nil {
			t.Error(e)
		}
	})
	if value = readPostgreSQLObservation(ctx, db, resource, time.Now(), time.Minute); value.Status != "identity_mismatch" {
		t.Fatalf("foreign slot matched %+v", value)
	}
	if _, err = db.ExecContext(ctx, "SELECT pg_drop_replication_slot($1)", slot); err != nil {
		t.Fatal(err)
	}
	// Cleanup below checks absence; don't drop an already-removed slot twice.
	resource.PostgreSQL.SlotOwned, resource.PostgreSQL.PublicationOwned = false, false
	if err = verifyPostgreSQLResourceReservation(ctx, plan, resource); err != nil {
		t.Fatalf("empty namespace refused: %v", err)
	}
	exec("CREATE PUBLICATION " + pq.QuoteIdentifier(pub) + " FOR TABLE " + foreign)
	if err = verifyPostgreSQLResourceReservation(ctx, plan, resource); err == nil {
		t.Fatal("foreign publication adopted")
	}
	if err = (DatabaseSourceResources{}).DropOwnedResources(ctx, plan, resource); err != nil {
		t.Fatal(err)
	}
	var table string
	if err = db.QueryRowContext(ctx, "SELECT tablename FROM pg_publication_tables WHERE pubname=$1", pub).Scan(&table); err != nil || table != "foreign_table" {
		t.Fatal("rejected resource was changed or deleted")
	}
	exec("ALTER PUBLICATION " + pq.QuoteIdentifier(pub) + " SET TABLE " + source)
	if err = verifyPostgreSQLResourceReservation(ctx, plan, resource); err == nil {
		t.Fatal("same-table stranger adopted without reservation")
	}
	resource.PostgreSQL.SlotOwned, resource.PostgreSQL.PublicationOwned = true, true
	if err = verifyPostgreSQLResourceReservation(ctx, plan, resource); err != nil {
		t.Fatalf("owned reentry failed: %v", err)
	}
	exec("ALTER PUBLICATION " + pq.QuoteIdentifier(pub) + " SET TABLE " + foreign)
	if err = verifyPostgreSQLResourceReservation(ctx, plan, resource); err == nil {
		t.Fatal("changed publication identity accepted")
	}
}
