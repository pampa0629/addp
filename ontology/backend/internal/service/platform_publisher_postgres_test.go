package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/addp/ontology/internal/falkor"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func testPlatformPublication(t *testing.T, db *gorm.DB) {
	ctx, actor := context.Background(), platformMachineActor()
	repo := repository.NewPlatformRevisionRepository(db)
	auth := testPlatformAuthorizer(func(ctx context.Context, _ *platform.Snapshot) (models.PlatformActor, error) { return actor, ctx.Err() })
	publisher := func(graph ProjectionGraph, a PlatformPublicationAuthorizer) *PlatformPublisher {
		t.Helper()
		p, err := NewPlatformPublisher(repo, graph, a)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	head := func(capability string) models.PlatformCapability {
		t.Helper()
		h, err := repo.Head(ctx, actor, capability)
		if err != nil {
			t.Fatal(err)
		}
		return *h
	}
	assertActive := func(capability string, p *models.PlatformProjection, version uint64) {
		t.Helper()
		h := head(capability)
		if h.ActiveRevision == nil || *h.ActiveRevision != p.Revision || h.ActiveGeneration == nil || *h.ActiveGeneration != p.Generation || h.ActivationVersion != version {
			t.Fatalf("unsafe active pointer: %+v", h)
		}
	}
	var executionsBefore, tenantProjectionsBefore int64
	if err := db.Table("common.task_executions").Count(&executionsBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Projection{}).Count(&tenantProjectionsBefore).Error; err != nil {
		t.Fatal(err)
	}
	t.Run("runtime_reads_only_active_pg_snapshot", func(t *testing.T) {
		capability := "transfer.task.runtime_read"
		reader := NewPlatformRevisionService(repo)
		if _, err := reader.PlatformCapabilityContext(ctx, capability); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal(err)
		}
		snapshot := platformTestSnapshot(t, capability, 7)
		if _, err := repo.Store(ctx, actor, snapshot, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.PlatformCapabilityContext(ctx, capability); !errors.Is(err, repository.ErrNotActive) {
			t.Fatal("record alone became active", err)
		}
		catalog, err := reader.PlatformCapabilities(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range catalog.Capabilities {
			if item.Capability == capability {
				t.Fatal("inactive definition entered catalog")
			}
		}
		if err := publisher(testPlatformGraph{}, auth).PublishOnReady(ctx, func() bool { return true }, func() (*platform.Snapshot, error) { return snapshot, nil }); err != nil {
			t.Fatal(err)
		}
		want, _ := snapshot.Context()
		got, err := reader.PlatformCapabilityContext(ctx, capability)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("wrong PG snapshot", got, err)
		}
		catalog, err = reader.PlatformCapabilities(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for i, item := range catalog.Capabilities {
			if i > 0 && catalog.Capabilities[i-1].Capability >= item.Capability {
				t.Fatal("catalog not deterministically ordered")
			}
			if item.Capability == capability {
				found = reflect.DeepEqual(item, want)
			}
		}
		if !found {
			t.Fatal("active definition not consumed by catalog")
		}
		got.Concepts[0].Name["en"] = "mutated"
		got, err = reader.PlatformCapabilityContext(ctx, capability)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("shared mutable read", err)
		}
		next := platformTestSnapshot(t, capability, 8)
		if _, err := repo.Store(ctx, actor, next, 7); err != nil {
			t.Fatal(err)
		}
		got, err = reader.PlatformCapabilityContext(ctx, capability)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("latest record replaced active", err)
		}
		g := testPlatformGraph{verify: func(context.Context, *falkor.Projection) error { return falkor.ErrProtocol }}
		if _, err := publisher(g, auth).Publish(ctx, next); err == nil {
			t.Fatal("verification failure published")
		}
		got, err = reader.PlatformCapabilityContext(ctx, capability)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("failed candidate replaced active", err)
		}
		if _, err := publisher(testPlatformGraph{}, auth).Publish(ctx, next); err != nil {
			t.Fatal(err)
		}
		got, err = reader.PlatformCapabilityContext(ctx, capability)
		if err != nil || got.Revision != 8 || got.Digest != next.Digest() {
			t.Fatal("new active invisible", got, err)
		}
	})
	t.Run("runtime_rejects_corrupt_active_snapshot_without_source_fallback", func(t *testing.T) {
		// A privileged fixture installs a valid-hash but noncanonical record.
		// No triggers or integrity constraints are bypassed.
		capability := "transfer.task.corrupt_runtime"
		snapshot := platformTestSnapshot(t, capability, 2)
		payload := " " + string(snapshot.CanonicalJSON())
		hash := sha256.Sum256([]byte(payload))
		digest := hex.EncodeToString(hash[:])
		if err := db.Create(&models.PlatformCapability{Capability: capability, LastRevision: 2}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.PlatformRevision{Capability: capability, Revision: 2, Payload: payload, Digest: digest}).Error; err != nil {
			t.Fatal(err)
		}
		generation := uuid.NewString()
		if err := db.Create(&models.PlatformProjection{Capability: capability, Revision: 2, Digest: digest, Generation: generation, Status: "building"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&models.PlatformCapability{}).Where("capability=?", capability).Update("publish_generation", generation).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&models.PlatformProjection{}).Where("generation=?", generation).Update("status", "ready").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&models.PlatformCapability{}).Where("capability=?", capability).Updates(map[string]any{"active_revision": 2, "active_generation": generation, "activation_version": 1}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := NewPlatformRevisionService(repo).PlatformCapabilityContext(ctx, capability); !errors.Is(err, repository.ErrIntegrity) {
			t.Fatal("corrupt active exposed", err)
		}
		if _, err := NewPlatformRevisionService(repo).PlatformCapabilities(ctx); !errors.Is(err, repository.ErrIntegrity) {
			t.Fatal("corrupt active skipped in catalog", err)
		}
	})
	t.Run("exact_release_reuse_and_three_fresh_checks", func(t *testing.T) {
		snapshot := platformTestSnapshot(t, "transfer.task.publish", 2)
		stages := []string{}
		checks := 0
		a := testPlatformAuthorizer(func(ctx context.Context, _ *platform.Snapshot) (models.PlatformActor, error) {
			stages = append(stages, "auth")
			checks++
			current := actor
			current.AuthorizationVersion = int64(checks)
			return current, ctx.Err()
		})
		g := testPlatformGraph{build: func(context.Context, *falkor.Projection) error { stages = append(stages, "build"); return nil }, verify: func(context.Context, *falkor.Projection) error { stages = append(stages, "verify"); return nil }}
		first, err := publisher(g, a).Publish(ctx, snapshot)
		if err != nil || first.Status != "ready" || !reflect.DeepEqual(stages, []string{"auth", "auth", "build", "verify", "auth"}) {
			t.Fatal(first, stages, err)
		}
		assertActive(first.Capability, first, 1)
		var event models.PlatformProjectionEvent
		if err := db.Where("generation=? AND action='ready'", first.Generation).First(&event).Error; err != nil || event.AuthorizationVersion != 3 || event.ActorPrincipalID != actor.PrincipalID {
			t.Fatal(event, err)
		}
		second, err := publisher(g, auth).Publish(ctx, snapshot)
		if err != nil || second.Generation == first.Generation {
			t.Fatal(second, err)
		}
		assertActive(second.Capability, second, 2)
		for _, model := range []any{&models.PlatformRevision{}, &models.PlatformRevisionEvent{}} {
			var count int64
			if err := db.Model(model).Where("capability=?", first.Capability).Count(&count).Error; err != nil || count != 1 {
				t.Fatal("release duplicated", count, err)
			}
		}
		// A different artifact with the same release identity must never build.
		d, _ := snapshot.Context()
		d.Digest = ""
		d.Operation.Effects = append(d.Operation.Effects, "another.effect")
		data, _ := json.Marshal(d)
		different, err := platform.Compile(data)
		if err != nil {
			t.Fatal(err)
		}
		if p, err := publisher(g, auth).Publish(ctx, different); p != nil || !errors.Is(err, repository.ErrIntegrity) {
			t.Fatal("identity overwritten", p, err)
		}
		assertActive(second.Capability, second, 2)
	})
	t.Run("failure_revocation_cancellation_preserve_old", func(t *testing.T) {
		capability := "transfer.task.failures"
		old, err := publisher(testPlatformGraph{}, auth).Publish(ctx, platformTestSnapshot(t, capability, 2))
		if err != nil {
			t.Fatal(err)
		}
		next := platformTestSnapshot(t, capability, 3)
		denied := errors.New("permission revoked")
		for _, stage := range []string{"build", "verify", "authorize", "cancel", "principal_changed"} {
			t.Run(stage, func(t *testing.T) {
				callCtx, cancel := context.WithCancel(ctx)
				defer cancel()
				checks := 0
				a := testPlatformAuthorizer(func(c context.Context, _ *platform.Snapshot) (models.PlatformActor, error) {
					checks++
					if stage == "authorize" && checks == 3 {
						return models.PlatformActor{}, denied
					}
					current := actor
					if stage == "principal_changed" && checks == 3 {
						current.PrincipalID++
					}
					return current, c.Err()
				})
				g := testPlatformGraph{build: func(context.Context, *falkor.Projection) error {
					if stage == "build" {
						return falkor.ErrRejected
					}
					return nil
				}, verify: func(context.Context, *falkor.Projection) error {
					if stage == "verify" {
						return falkor.ErrProtocol
					}
					if stage == "cancel" {
						cancel()
					}
					return nil
				}}
				attempt, err := publisher(g, a).Publish(callCtx, next)
				if err == nil || attempt == nil || attempt.Status == "ready" {
					t.Fatal("failed publication accepted", attempt, err)
				}
				assertActive(capability, old, 1)
				if stage == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			})
		}
		recovered, err := publisher(testPlatformGraph{}, auth).Publish(ctx, next)
		if err != nil {
			t.Fatal(err)
		}
		assertActive(capability, recovered, 2)
		if _, err := publisher(testPlatformGraph{}, auth).Publish(ctx, platformTestSnapshot(t, capability, 2)); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("older deployment rolled back", err)
		}
		assertActive(capability, recovered, 2)
	})
	t.Run("authorization_denial_before_record_or_graph", func(t *testing.T) {
		for _, deniedStage := range []int{1, 2} {
			capability := fmt.Sprintf("transfer.task.auth_stage_%d", deniedStage)
			snapshot := platformTestSnapshot(t, capability, 2)
			checks := 0
			denial := errors.New("machine permission denied")
			a := testPlatformAuthorizer(func(context.Context, *platform.Snapshot) (models.PlatformActor, error) {
				checks++
				if checks == deniedStage {
					return models.PlatformActor{}, denial
				}
				return actor, nil
			})
			graphCalled := false
			g := testPlatformGraph{build: func(context.Context, *falkor.Projection) error { graphCalled = true; return nil }}
			if attempt, err := publisher(g, a).Publish(ctx, snapshot); attempt != nil || !errors.Is(err, denial) || graphCalled {
				t.Fatal("denied machine published", attempt, err)
			}
			var count int64
			if err := db.Model(&models.PlatformProjection{}).Where("capability=?", capability).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("denied graph admitted", count, err)
			}
			if err := db.Model(&models.PlatformRevision{}).Where("capability=?", capability).Count(&count).Error; err != nil || count != int64(deniedStage-1) {
				t.Fatal("wrong recording boundary", count, err)
			}
		}
	})
	t.Run("generation_fence_rejects_late_writer", func(t *testing.T) {
		snapshot := platformTestSnapshot(t, "transfer.task.fencing", 2)
		if _, err := repo.Store(ctx, actor, snapshot, 0); err != nil {
			t.Fatal(err)
		}
		first, err := repo.BeginPlatformProjection(ctx, actor, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		second, err := repo.BeginPlatformProjection(ctx, actor, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.FinishPlatformProjection(ctx, actor, snapshot, first, "ready"); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("late writer admitted", err)
		}
		if err := repo.FinishPlatformProjection(ctx, actor, snapshot, second, "ready"); err != nil {
			t.Fatal(err)
		}
		assertActive(second.Capability, second, 1)
		var stale models.PlatformProjection
		if err := db.Where("generation=?", first.Generation).First(&stale).Error; err != nil || stale.Status != "superseded" {
			t.Fatal(stale, err)
		}
		if err := repo.FinishPlatformProjection(ctx, actor, snapshot, second, "ready"); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("duplicate activation", err)
		}
		third, err := repo.BeginPlatformProjection(ctx, actor, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		hijacked := *third
		hijacked.BaselineActivationVersion = 0
		if err := repo.FinishPlatformProjection(ctx, actor, snapshot, &hijacked, "ready"); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("ABA baseline", err)
		}
		if err := repo.FinishPlatformProjection(ctx, actor, snapshot, third, "ready"); err != nil {
			t.Fatal(err)
		}
		assertActive(third.Capability, third, 2)
	})
	t.Run("concurrent_activation_exactly_one", func(t *testing.T) {
		snapshot := platformTestSnapshot(t, "transfer.task.activation_race", 2)
		if _, err := repo.Store(ctx, actor, snapshot, 0); err != nil {
			t.Fatal(err)
		}
		attempt, err := repo.BeginPlatformProjection(ctx, actor, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		outcomes := make(chan error, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				outcomes <- repo.FinishPlatformProjection(ctx, actor, snapshot, attempt, "ready")
			}()
		}
		close(start)
		wg.Wait()
		close(outcomes)
		wins, conflicts := 0, 0
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
			t.Fatal(wins, conflicts)
		}
		assertActive(attempt.Capability, attempt, 1)
	})
	t.Run("new_publisher_fences_running_predecessor", func(t *testing.T) {
		bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		snapshot := platformTestSnapshot(t, "transfer.task.publisher_race", 2)
		entered, release := make(chan struct{}), make(chan struct{})
		oldDone := make(chan error, 1)
		workerDone := make(chan struct{})
		g := testPlatformGraph{build: func(ctx context.Context, _ *falkor.Projection) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}}
		go func() {
			defer close(workerDone)
			_, err := publisher(g, auth).Publish(bounded, snapshot)
			oldDone <- err
		}()
		t.Cleanup(func() {
			cancel()
			select {
			case <-workerDone:
			case <-time.After(2 * time.Second):
				t.Error("publisher fixture did not stop")
			}
		})
		select {
		case <-entered:
		case <-bounded.Done():
			t.Fatal("first publisher did not build")
		}
		second, err := publisher(testPlatformGraph{}, auth).Publish(bounded, snapshot)
		close(release)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-oldDone:
			if !errors.Is(err, repository.ErrConflict) {
				t.Fatal("late publisher succeeded", err)
			}
		case <-bounded.Done():
			t.Fatal("old publisher did not finish")
		}
		assertActive(second.Capability, second, 1)
	})
	t.Run("new_record_blocks_old_build_activation", func(t *testing.T) {
		snapshot := platformTestSnapshot(t, "transfer.task.changed_head", 2)
		if _, err := repo.Store(ctx, actor, snapshot, 0); err != nil {
			t.Fatal(err)
		}
		attempt, err := repo.BeginPlatformProjection(ctx, actor, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Store(ctx, actor, platformTestSnapshot(t, attempt.Capability, 3), 2); err != nil {
			t.Fatal(err)
		}
		if err := repo.FinishPlatformProjection(ctx, actor, snapshot, attempt, "ready"); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("old record activated", err)
		}
		h := head(attempt.Capability)
		if h.ActiveRevision != nil || h.ActiveGeneration != nil || h.ActivationVersion != 0 {
			t.Fatal("stale build active", h)
		}
	})
	t.Run("ready_audit_failure_is_atomic", func(t *testing.T) {
		capability := "transfer.task.audit_failure"
		old, err := publisher(testPlatformGraph{}, auth).Publish(ctx, platformTestSnapshot(t, capability, 2))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`CREATE FUNCTION ontology.test_platform_ready_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END; $$;
CREATE TRIGGER test_platform_ready_audit BEFORE INSERT ON ontology.platform_projection_events FOR EACH ROW WHEN (NEW.action='ready') EXECUTE FUNCTION ontology.test_platform_ready_audit();`).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Exec("DROP TRIGGER test_platform_ready_audit ON ontology.platform_projection_events; DROP FUNCTION ontology.test_platform_ready_audit()").Error; err != nil {
				t.Error(err)
			}
		})
		attempt, err := publisher(testPlatformGraph{}, auth).Publish(ctx, platformTestSnapshot(t, capability, 3))
		if err == nil || attempt == nil || attempt.Status != "failed" {
			t.Fatal(attempt, err)
		}
		assertActive(capability, old, 1)
		var count int64
		if err := db.Model(&models.PlatformProjectionEvent{}).Where("generation=? AND action='ready'", attempt.Generation).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("partial audit", count, err)
		}
		for _, query := range []string{"UPDATE ontology.platform_projections SET digest=digest", "DELETE FROM ontology.platform_projections", "UPDATE ontology.platform_projection_events SET action=action", "DELETE FROM ontology.platform_projection_events"} {
			if err := db.Exec(query).Error; err == nil {
				t.Fatal("immutable terminal history changed", query)
			}
		}
		for _, query := range []string{
			"UPDATE ontology.platform_capabilities SET activation_version=0 WHERE capability='transfer.task.audit_failure'",
			"UPDATE ontology.platform_capabilities SET activation_version=activation_version+1 WHERE capability='transfer.task.audit_failure'",
			"UPDATE ontology.platform_capabilities SET active_generation=publish_generation, active_revision=last_revision, activation_version=activation_version+1 WHERE capability='transfer.task.audit_failure'",
			"UPDATE ontology.platform_capabilities SET last_revision=1 WHERE capability='transfer.task.audit_failure'",
			"UPDATE ontology.platform_capabilities SET publish_generation=NULL WHERE capability='transfer.task.audit_failure'",
		} {
			if err := db.Exec(query).Error; err == nil {
				t.Fatal("unsafe head mutation", query)
			}
		}
		assertActive(capability, old, 1)
	})
	var executionsAfter, tenantProjectionsAfter int64
	if err := db.Table("common.task_executions").Count(&executionsAfter).Error; err != nil || executionsAfter != executionsBefore {
		t.Fatal("platform created Tenant task", err)
	}
	if err := db.Model(&models.Projection{}).Count(&tenantProjectionsAfter).Error; err != nil || tenantProjectionsAfter != tenantProjectionsBefore {
		t.Fatal("platform touched Tenant projection", err)
	}
}
