package service

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/addp/common/schema"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"gorm.io/gorm"
)

func TestPostgresMigrationUpgrade(t *testing.T) {
	for _, from := range []int64{1, 2, 3, 4} {
		t.Run(fmt.Sprintf("from_%d", from), func(t *testing.T) { testProjectionMigrationUpgrade(t, from) })
	}
}

func testProjectionMigrationUpgrade(t *testing.T, from int64) {
	db := postgresFixtureMigration(t, func(db *gorm.DB) error {
		initial, err := os.ReadFile("../repository/migrations/001_revisions.sql")
		if err != nil {
			return err
		}
		return schema.Migrate(db, "ontology", from, func(tx *gorm.DB) error {
			if err := tx.Exec(string(initial)).Error; err != nil {
				return err
			}
			if from >= 2 {
				projection, err := os.ReadFile("../repository/migrations/002_projection_runtime.sql")
				if err != nil {
					return err
				}
				if err := tx.Exec(string(projection)).Error; err != nil {
					return err
				}
			}
			if from >= 3 {
				rebuild, err := os.ReadFile("../repository/migrations/003_projection_rebuild.sql")
				if err != nil {
					return err
				}
				if err := tx.Exec(string(rebuild)).Error; err != nil {
					return err
				}
			}
			if from >= 4 {
				platformSQL, err := os.ReadFile("../repository/migrations/004_platform_revisions.sql")
				if err != nil {
					return err
				}
				return tx.Exec(string(platformSQL)).Error
			}
			return nil
		})
	})
	// A pre-runtime head must survive a forward migration with no inferred
	// active graph or guessed publication baseline.
	if err := db.Exec("INSERT INTO ontology.ontologies (tenant_id,ontology_id,last_revision) VALUES (101,'before_runtime',1)").Error; err != nil {
		t.Fatal(err)
	}
	if from == 4 {
		snapshot := platformTestSnapshot(t, "transfer.task.migration", 2)
		// Seed the actual v4 schema, not the current head model with v5 columns.
		if err := db.Exec("INSERT INTO ontology.platform_capabilities (capability,last_revision) VALUES ('transfer.task.migration',2)").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.PlatformRevision{Capability: "transfer.task.migration", Revision: 2, Payload: string(snapshot.CanonicalJSON()), Digest: snapshot.Digest()}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.PlatformRevisionEvent{Capability: "transfer.task.migration", Revision: 2, Action: "record", Digest: snapshot.Digest(), ActorPrincipalID: 11, ActorPrincipalType: "user", AuthorizationVersion: 7}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	var head models.Ontology
	if err := db.Where("tenant_id=101 AND ontology_id='before_runtime'").First(&head).Error; err != nil {
		t.Fatal(err)
	}
	if head.LastRevision != 1 || head.ActivationVersion != 1 || head.ActiveRevision != nil || head.ActiveGeneration != nil {
		t.Fatalf("migration altered existing identity: %#v", head)
	}
	if err := schema.Require(db, "ontology", repository.SchemaVersion); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&models.PlatformCapability{}, &models.PlatformRevision{}, &models.PlatformRevisionEvent{}, &models.PlatformProjection{}, &models.PlatformProjectionEvent{}} {
		var count int64
		var expected int64
		if from == 4 {
			switch model.(type) {
			case *models.PlatformCapability, *models.PlatformRevision, *models.PlatformRevisionEvent:
				expected = 1
			}
		}
		if err := db.Model(model).Count(&count).Error; err != nil || count != expected {
			t.Fatal("migration invented platform records", count, err)
		}
	}
	if from == 4 {
		s := NewPlatformRevisionService(repository.NewPlatformRevisionRepository(db))
		head, err := s.Head(context.Background(), platformTestActor(), "transfer.task.migration")
		if err != nil || head.LastRevision != 2 || head.ActivationVersion != 0 || head.ActiveRevision != nil || head.ActiveGeneration != nil || head.PublishGeneration != nil {
			t.Fatal("migration inferred activation", head, err)
		}
		if _, err := s.Get(context.Background(), platformTestActor(), head.Capability, 2); err != nil {
			t.Fatal("migration damaged immutable record", err)
		}
	}
}
