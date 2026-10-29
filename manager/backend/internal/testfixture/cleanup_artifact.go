package testfixture

import (
	"gorm.io/gorm"
	"testing"
)

// CleanupArtifactSQLite supplies the domain control facts in process-local
// fixtures. Production schema and locking are validated by the PG gate.
func CleanupArtifactSQLite(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, sql := range []string{
		`CREATE TABLE IF NOT EXISTS manager.cleanup_targets(key TEXT PRIMARY KEY,tenant_id INTEGER NOT NULL,kind TEXT NOT NULL,identity TEXT NOT NULL,generation INTEGER NOT NULL DEFAULT 0,operation_id TEXT NOT NULL DEFAULT '',updated_at DATETIME)`,
		`CREATE TABLE IF NOT EXISTS manager.cleanup_artifact_operations(id TEXT PRIMARY KEY,tenant_id INTEGER NOT NULL,origin TEXT NOT NULL,execution_id TEXT NOT NULL,attempt INTEGER NOT NULL,lease_token TEXT NOT NULL,lease_owner TEXT NOT NULL,action TEXT NOT NULL,snapshot TEXT NOT NULL,target_keys TEXT NOT NULL,binding_digest TEXT NOT NULL,state TEXT NOT NULL,created_at DATETIME,effect_at DATETIME,finalized_at DATETIME)`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func ArtifactReferenceSQLite(t *testing.T, db *gorm.DB) {
	t.Helper()
	CleanupArtifactSQLite(t, db)
	for _, table := range []string{"raster_cog", "model_3d_glb", "model3d_tiles", "gaussian_splat_ksplat", "point_cloud_copc", "pptx_pdf", "vector_tile_cache"} {
		if err := db.Exec("CREATE TABLE IF NOT EXISTS manager." + table + " (id INTEGER PRIMARY KEY,tenant_id INTEGER,storage_ref TEXT,deleted_at DATETIME)").Error; err != nil {
			t.Fatal(err)
		}
	}
}
