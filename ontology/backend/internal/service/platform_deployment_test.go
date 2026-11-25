package service

import (
	"os"
	"testing"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/ontology/internal/repository"
	"gorm.io/gorm"
)

func TestInstalledOntologyRevision(t *testing.T) {
	if os.Getenv("ADDP_TEST_ONTOLOGY_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Ontology deployment")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "ontology"}, "ontology", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := repository.NewPlatformRevisionRepository(db)
	actor := platformTestActor()
	snapshot := platformTestSnapshot(t, "transfer.task.create", 2)
	row, err := repo.Store(t.Context(), actor, snapshot, 0)
	if err != nil {
		t.Fatal(err)
	}
	read, err := repo.Get(t.Context(), actor, row.Capability, row.Revision)
	if err != nil || read.Payload != string(snapshot.CanonicalJSON()) || read.Digest != snapshot.Digest() {
		t.Fatalf("frozen revision read error: %v", err)
	}
	if err := db.Model(row).Update("payload", "{}").Error; err == nil {
		t.Fatal("immutable revision was rewritten")
	}
	if err := db.Delete(row).Error; err == nil {
		t.Fatal("immutable revision was deleted")
	}
	t.Logf("capability %s revision %d retained its exact payload and digest %s", row.Capability, row.Revision, read.Digest)
}
