package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/addp/common/schema"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"gorm.io/gorm"
)

func TestPostgresMigrationUpgrade(t *testing.T) {
	for _, from := range []int64{1, 2, 3, 4, 5} {
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
				if err := tx.Exec(string(platformSQL)).Error; err != nil {
					return err
				}
			}
			if from >= 5 {
				publicationSQL, err := os.ReadFile("../repository/migrations/005_platform_publication.sql")
				if err != nil {
					return err
				}
				return tx.Exec(string(publicationSQL)).Error
			}
			return nil
		})
	})
	// A pre-runtime head must survive a forward migration with no inferred
	// active graph or guessed publication baseline.
	if err := db.Exec("INSERT INTO ontology.ontologies (tenant_id,ontology_id,last_revision) VALUES (101,'before_runtime',1)").Error; err != nil {
		t.Fatal(err)
	}
	var historicalPayload, historicalDigest string
	if from >= 4 {
		snapshot := platformTestSnapshot(t, "transfer.task.migration", 2)
		// Historical bytes are created in the historical format, never by a
		// runtime fallback compiler. The new Restore must refuse them.
		var old map[string]json.RawMessage
		if err := json.Unmarshal(snapshot.CanonicalJSON(), &old); err != nil {
			t.Fatal(err)
		}
		delete(old, "review")
		old["contract"] = json.RawMessage(`"addp.platform-definition/v1"`)
		old["compiler"] = json.RawMessage(`"addp.platform-compiler/v1"`)
		payload, err := json.Marshal(old)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(payload)
		historicalPayload, historicalDigest = string(payload), hex.EncodeToString(hash[:])
		// Seed the actual v4 schema, not the current head model with v5 columns.
		if err := db.Exec("INSERT INTO ontology.platform_capabilities (capability,last_revision) VALUES ('transfer.task.migration',2)").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.PlatformRevision{Capability: "transfer.task.migration", Revision: 2, Payload: historicalPayload, Digest: historicalDigest}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.PlatformRevisionEvent{Capability: "transfer.task.migration", Revision: 2, Action: "record", Digest: historicalDigest, ActorPrincipalID: 11, ActorPrincipalType: "user", AuthorizationVersion: 7}).Error; err != nil {
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
		if from >= 4 {
			switch model.(type) {
			case *models.PlatformCapability, *models.PlatformRevision, *models.PlatformRevisionEvent:
				expected = 1
			}
		}
		if err := db.Model(model).Count(&count).Error; err != nil || count != expected {
			t.Fatal("migration invented platform records", count, err)
		}
	}
	if from >= 4 {
		s := NewPlatformRevisionService(repository.NewPlatformRevisionRepository(db))
		head, err := s.Head(context.Background(), platformTestActor(), "transfer.task.migration")
		if err != nil || head.LastRevision != 2 || head.ActivationVersion != 0 || head.ActiveRevision != nil || head.ActiveGeneration != nil || head.PublishGeneration != nil {
			t.Fatal("migration inferred activation", head, err)
		}
		if _, err := s.Get(context.Background(), platformTestActor(), head.Capability, 2); !errors.Is(err, repository.ErrIntegrity) {
			t.Fatal("old snapshot was accepted or normalized", err)
		}
		var record models.PlatformRevision
		if err := db.Where("capability=? AND revision=2", head.Capability).First(&record).Error; err != nil || record.Payload != historicalPayload || record.Digest != historicalDigest {
			t.Fatal("migration changed historical evidence", record, err)
		}
		// NOT VALID protects historical audit evidence but still rejects every
		// attempted new v1 write. It is not dual-format write support.
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("INSERT INTO ontology.platform_capabilities (capability) VALUES ('transfer.task.legacy')").Error; err != nil {
				return err
			}
			return tx.Exec("INSERT INTO ontology.platform_revisions (capability,revision,payload,digest) VALUES ('transfer.task.migration',2,?,?) ON CONFLICT DO NOTHING", historicalPayload, historicalDigest).Error
		}); err == nil {
			t.Fatal("historical-format write accepted after upgrade")
		}
	}
}
