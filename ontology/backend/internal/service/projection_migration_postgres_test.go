package service

import (
	"fmt"
	"os"
	"testing"

	"github.com/addp/common/schema"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"gorm.io/gorm"
)

func TestPostgresMigrationUpgrade(t *testing.T) {
	for _, from := range []int64{1, 2} {
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
			if from == 2 {
				projection, err := os.ReadFile("../repository/migrations/002_projection_runtime.sql")
				if err != nil {
					return err
				}
				return tx.Exec(string(projection)).Error
			}
			return nil
		})
	})
	// A pre-runtime head must survive a forward migration with no inferred
	// active graph or guessed publication baseline.
	if err := db.Exec("INSERT INTO ontology.ontologies (tenant_id,ontology_id,last_revision) VALUES (101,'before_runtime',1)").Error; err != nil {
		t.Fatal(err)
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
}
