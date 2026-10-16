package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestEngineServerVersionForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reset := func() {
		t.Helper()
		if _, err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	before, through := migrationFilesBeforeAndThrough(t, "000213_engine_server_version.up.sql")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.QueryRow(`INSERT INTO system.engines(name,engine_type,connection_info,identity_key,created_at,updated_at) VALUES('version migration fixture','postgresql','{"host":"localhost","port":5432,"database":"test"}','{"host":"localhost","port":5432,"database":"test"}',now(),now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	var revision int64
	var updated time.Time
	if err := db.QueryRow("SELECT version,updated_at FROM system.engines WHERE id=$1", id).Scan(&revision, &updated); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	for i := 0; i < 2; i++ {
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var version string
	var observed sql.NullTime
	var currentRevision int64
	var currentUpdated time.Time
	if err := db.QueryRow("SELECT server_version,server_version_checked_at,version,updated_at FROM system.engines WHERE id=$1", id).Scan(&version, &observed, &currentRevision, &currentUpdated); err != nil {
		t.Fatal(err)
	}
	if version != "" || observed.Valid || currentRevision != revision || !currentUpdated.Equal(updated) {
		t.Fatalf("migration fabricated facts or changed edit baseline: version=%q observed=%+v revision=%d updated=%s", version, observed, currentRevision, currentUpdated)
	}
}
