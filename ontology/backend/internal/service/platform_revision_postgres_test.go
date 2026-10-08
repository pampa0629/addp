package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
	"gorm.io/gorm"
)

func testPlatformRevisionStorage(t *testing.T, db *gorm.DB) {
	s := NewPlatformRevisionService(repository.NewPlatformRevisionRepository(db))
	ctx, actor := context.Background(), platformTestActor()
	var executionsBefore, projectionsBefore int64
	if err := db.Table("common.task_executions").Count(&executionsBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Projection{}).Count(&projectionsBefore).Error; err != nil {
		t.Fatal(err)
	}
	t.Run("record_exact_release_without_tenant_or_activation", func(t *testing.T) {
		snapshot := platformTestSnapshot(t, "transfer.task.create", 2)
		if _, err := s.Head(ctx, actor, "transfer.task.create"); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal(err)
		}
		record, err := s.Store(ctx, actor, snapshot, 0)
		if err != nil {
			t.Fatal(err)
		}
		read, err := s.Get(ctx, actor, record.Capability, record.Revision)
		if err != nil || read.Payload != string(snapshot.CanonicalJSON()) || read.Digest != snapshot.Digest() ||
			!read.CreatedAt.Equal(record.CreatedAt) {
			t.Fatalf("stored release mismatch: %+v %v", read, err)
		}
		head, err := s.Head(ctx, actor, record.Capability)
		if err != nil || head.LastRevision != 2 {
			t.Fatalf("head %+v %v", head, err)
		}
		var event models.PlatformRevisionEvent
		if err := db.Where("capability = ?", record.Capability).First(&event).Error; err != nil {
			t.Fatal(err)
		}
		if event.Action != "record" || event.Revision != 2 || event.Digest != record.Digest ||
			event.ActorPrincipalID != actor.PrincipalID || event.ActorPrincipalType != "user" ||
			event.AuthorizationVersion != actor.AuthorizationVersion || !event.CreatedAt.Equal(read.CreatedAt) {
			t.Fatalf("invalid platform provenance: %+v", event)
		}
		if _, err := s.Get(ctx, actor, record.Capability, 1); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("invented skipped revision", err)
		}
		tenant := NewRevisionService(repository.NewRevisionRepository(db))
		if _, err := tenant.Get(ctx, testActor(101), semantic.Scope{TenantID: 101, OntologyID: record.Capability, Revision: 2}); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("platform leaked through tenant lookup", err)
		}
		for _, baseline := range []uint64{0, 2} {
			if _, err := s.Store(ctx, actor, snapshot, baseline); !errors.Is(err, repository.ErrConflict) {
				t.Fatal("duplicate release accepted", err)
			}
		}
		if _, err := s.Store(ctx, actor, platformTestSnapshot(t, record.Capability, 4), 1); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("stale baseline accepted", err)
		}
		serviceActor := actor
		serviceActor.PrincipalID, serviceActor.PrincipalType = 22, "service_principal"
		if _, err := s.Store(ctx, serviceActor, platformTestSnapshot(t, record.Capability, 4), 2); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Store(ctx, actor, platformTestSnapshot(t, record.Capability, 3), 4); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("backward release accepted", err)
		}
		if _, err := s.Get(ctx, actor, record.Capability, 2); err != nil {
			t.Fatal("old history unavailable", err)
		}
		var count int64
		if err := db.Model(&models.Ontology{}).Where("tenant_id = 0").Count(&count).Error; err != nil || count != 0 {
			t.Fatal("synthetic platform tenant", count, err)
		}
	})
	t.Run("concurrent_baseline_has_one_winner", func(t *testing.T) {
		const capability = "transfer.task.concurrent"
		first, second := platformTestSnapshot(t, capability, 5), platformTestSnapshot(t, capability, 6)
		start, outcomes := make(chan struct{}), make(chan error, 2)
		var wg sync.WaitGroup
		for _, snapshot := range []*platform.Snapshot{first, second} {
			wg.Add(1)
			go func(snapshot *platform.Snapshot) {
				defer wg.Done()
				<-start
				_, err := s.Store(ctx, actor, snapshot, 0)
				outcomes <- err
			}(snapshot)
		}
		close(start)
		wg.Wait()
		close(outcomes)
		var wins, conflicts int
		for err := range outcomes {
			if err == nil {
				wins++
			} else if errors.Is(err, repository.ErrConflict) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if wins != 1 || conflicts != 1 {
			t.Fatal("unsafe concurrency", wins, conflicts)
		}
		for _, model := range []any{&models.PlatformRevision{}, &models.PlatformRevisionEvent{}} {
			var count int64
			if err := db.Model(model).Where("capability = ?", capability).Count(&count).Error; err != nil || count != 1 {
				t.Fatal(count, err)
			}
		}
		head, err := s.Head(ctx, actor, capability)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get(ctx, actor, capability, head.LastRevision); err != nil {
			t.Fatal("head does not identify winning record", err)
		}
	})
	t.Run("audit_failure_rolls_back_all_records", func(t *testing.T) {
		if _, err := s.Store(ctx, actor, platformTestSnapshot(t, "transfer.task.rollback_existing", 2), 0); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`CREATE FUNCTION ontology.test_reject_platform_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'injected audit failure'; END; $$;
CREATE TRIGGER test_reject_platform_audit BEFORE INSERT ON ontology.platform_revision_events
FOR EACH ROW WHEN (NEW.capability IN ('transfer.task.rollback', 'transfer.task.rollback_existing')) EXECUTE FUNCTION ontology.test_reject_platform_audit();`).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Exec("DROP TRIGGER test_reject_platform_audit ON ontology.platform_revision_events; DROP FUNCTION ontology.test_reject_platform_audit()").Error; err != nil {
				t.Error(err)
			}
		})
		if _, err := s.Store(ctx, actor, platformTestSnapshot(t, "transfer.task.rollback", 2), 0); err == nil {
			t.Fatal("audit failure ignored")
		}
		for _, model := range []any{&models.PlatformCapability{}, &models.PlatformRevision{}, &models.PlatformRevisionEvent{}} {
			var count int64
			if err := db.Model(model).Where("capability = 'transfer.task.rollback'").Count(&count).Error; err != nil || count != 0 {
				t.Fatal("partial transaction", count, err)
			}
		}
		if _, err := s.Store(ctx, actor, platformTestSnapshot(t, "transfer.task.rollback_existing", 4), 2); err == nil {
			t.Fatal("audit failure ignored for existing head")
		}
		head, err := s.Head(ctx, actor, "transfer.task.rollback_existing")
		if err != nil || head.LastRevision != 2 {
			t.Fatal("audit failure advanced head", head, err)
		}
		if _, err := s.Get(ctx, actor, head.Capability, 2); err != nil {
			t.Fatal("audit failure damaged old history", err)
		}
		if _, err := s.Get(ctx, actor, head.Capability, 4); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("audit failure left new revision", err)
		}
	})
	t.Run("database_immutability_and_integrity", func(t *testing.T) {
		before, err := s.Get(ctx, actor, "transfer.task.create", 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, query := range []string{
			"UPDATE ontology.platform_revisions SET payload = payload WHERE capability='transfer.task.create' AND revision=2",
			"DELETE FROM ontology.platform_revisions WHERE capability='transfer.task.create' AND revision=2",
			"UPDATE ontology.platform_revision_events SET digest = digest WHERE capability='transfer.task.create' AND revision=2",
			"DELETE FROM ontology.platform_revision_events WHERE capability='transfer.task.create' AND revision=2",
		} {
			if err := db.Exec(query).Error; err == nil {
				t.Fatal("immutable guard missing", query)
			}
		}
		after, err := s.Get(ctx, actor, "transfer.task.create", 2)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("history changed", err)
		}
		base := platformTestSnapshot(t, "transfer.task.create", 8)
		for _, record := range []models.PlatformRevision{
			{Capability: "transfer.task.create", Revision: 8, Payload: string(base.CanonicalJSON()), Digest: strings.Repeat("0", 64)},
			{Capability: "transfer.task.create", Revision: 9, Payload: string(base.CanonicalJSON()), Digest: base.Digest()},
			{Capability: "transfer.task.concurrent", Revision: 8, Payload: string(base.CanonicalJSON()), Digest: base.Digest()},
		} {
			if err := db.Create(&record).Error; err == nil {
				t.Fatal("invalid stored release accepted")
			}
		}
		// Privileged raw SQL can insert noncanonical JSON despite its valid
		// digest; exact service reads must still refuse to expose it.
		payload := " " + string(base.CanonicalJSON())
		hash := sha256.Sum256([]byte(payload))
		raw := models.PlatformRevision{Capability: "transfer.task.create", Revision: 8, Payload: payload, Digest: hex.EncodeToString(hash[:])}
		if err := db.Create(&raw).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get(ctx, actor, raw.Capability, 8); !errors.Is(err, repository.ErrIntegrity) {
			t.Fatal("noncanonical stored release exposed", err)
		}
	})
	var executionsAfter, projectionsAfter int64
	if err := db.Table("common.task_executions").Count(&executionsAfter).Error; err != nil || executionsAfter != executionsBefore {
		t.Fatal("recording created executions", executionsBefore, executionsAfter, err)
	}
	if err := db.Model(&models.Projection{}).Count(&projectionsAfter).Error; err != nil || projectionsAfter != projectionsBefore {
		t.Fatal("recording created tenant projections", projectionsBefore, projectionsAfter, err)
	}
}
