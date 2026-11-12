package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/testsupport"
)

func TestTIFFMaskForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable System PostgreSQL gate")
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
	run := func(runner *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	_, applied285 := migrationFilesBeforeAndThrough(t, "000285_tiff_dynamic_file_composition.up.sql")
	run(&Runner{DSN: dsn, FS: applied285, Root: DefaultMigrationsRoot})
	verifyState := func(wantVersion int) {
		t.Helper()
		var version int
		var dirty bool
		if err := db.QueryRow("SELECT version,dirty FROM system.schema_migrations").Scan(&version, &dirty); err != nil || version != wantVersion || dirty {
			t.Fatalf("migration state=%d/%t, want %d/false: %v", version, dirty, wantVersion, err)
		}
		var filename, checksum string
		if err := db.QueryRow("SELECT filename,sha256 FROM system.schema_migration_checksums WHERE version=285").Scan(&filename, &checksum); err != nil ||
			filename != "000285_tiff_dynamic_file_composition.up.sql" || checksum != appliedTIFFComposition285Checksum {
			t.Fatal("applied migration 285 identity or checksum changed", filename, checksum, err)
		}
	}
	checkComposition := func(wantMask bool) {
		t.Helper()
		for _, object := range []bool{false, true} {
			target := func(name string) plugin.EngineCatalogPath {
				if object {
					return plugin.ObjectItemPath(91, "images", name)
				}
				return plugin.FileItemPath(91, "images/"+name)
			}
			primary := target("a.tif")
			refs := []authorizationfacts.FileCompositionRef{
				{Target: primary, Role: "image", Primary: true, Required: true},
				{Target: target("a.tif.aux.xml"), Role: "auxiliary_metadata"},
			}
			for _, mask := range []bool{false, true} {
				if mask {
					refs = append(refs, authorizationfacts.FileCompositionRef{Target: target("a.tif.msk"), Role: "mask"})
				}
				composition, err := authorizationfacts.NewScannedFileCompositionBoundary("tiff", "multi", refs)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(composition)
				path, _ := json.Marshal(primary)
				var valid bool
				if err := db.QueryRow("SELECT system.valid_source_file_composition($1::jsonb,$2::jsonb,91)", string(raw), string(path)).Scan(&valid); err != nil || valid != (!mask || wantMask) {
					t.Fatalf("object=%v mask=%v valid=%v, want mask support=%v: %v", object, mask, valid, wantMask, err)
				}
			}
		}
	}
	verifyState(285)
	checkComposition(false)
	// Start at the exact persisted development version, including its real
	// checksum; let the normal Runner apply 286 and the new forward extension.
	latest := &Runner{DSN: dsn, FS: EmbeddedSQL, Root: DefaultMigrationsRoot}
	for range 2 {
		run(latest)
		verifyState(int(latestMigrationVersion(t)))
		checkComposition(true)
		var count int
		if err := db.QueryRow("SELECT count(*) FROM system.schema_migration_checksums WHERE version=287").Scan(&count); err != nil || count != 1 {
			t.Fatal("forward migration checksum was not recorded exactly once", count, err)
		}
	}
}
