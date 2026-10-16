package migration

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"testing"
)

func TestSourceIdentityMigration208RetainsAppliedChecksum(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000208_source_dataitem_boundary_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	const applied = "8b840e5392ae0681bd23eb934d077ec172d13e41ef5f6fd5776780df009765b5"
	if actual := fmt.Sprintf("%x", sha256.Sum256(data)); actual != applied {
		t.Fatalf("already-applied migration 208 changed: got %s, want %s; use a new forward migration", actual, applied)
	}
}
