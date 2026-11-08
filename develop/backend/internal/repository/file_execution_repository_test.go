package repository

import (
	"bytes"
	"strings"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonexecution "github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func filePendingFixture(t *testing.T) (*gorm.DB, *FileExecutionRepository, *commonexecution.TaskExecution, []commonexecution.DevelopFileSourceIntent) {
	t.Helper()
	db := newQueryExecutionTestDB(t)
	item, sources := newFilePendingValues()
	return db, NewFileExecutionRepository(db), item, sources
}

func newFilePendingValues() (*commonexecution.TaskExecution, []commonexecution.DevelopFileSourceIntent) {
	principal, membership, version, triggered := int64(31), int64(41), int64(5), 31
	item := &commonexecution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7,
		Module: commonexecution.ModuleDevelop, Source: commonexecution.ModuleDevelop, TaskType: commonexecution.TaskTypeWorkflow,
		ExecutionBoundary: commonexecution.ExecutionBoundaryBounded, TriggerType: commonexecution.TriggerTypeManual,
		Status: commonexecution.ExecutionStatusPending, MaxAttempts: 1, TriggeredBy: &triggered,
		ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version,
		ExecutionConfig: commonmodels.JSONMap{"timeout": 60, "content": map[string]any{"workflow_definition": map[string]any{"tasks": []any{}}}}}
	sources := []commonexecution.DevelopFileSourceIntent{{NodeID: "source", Target: plugin.FileItemPath(11, "source/A.gpkg")}}
	return item, sources
}

func fileIssuedFixture(t *testing.T, facts *commonclient.DevelopFileExecutionFacts) commonclient.IssuedDevelopFileAuthorization {
	t.Helper()
	scope, err := commonexecution.NewDevelopBoundFileScope(facts.Intent, []commonexecution.DevelopFileSourceReference{{
		Target: facts.Intent.Sources[0].Target, ItemFingerprint: strings.Repeat("a", 64), BoundaryHash: strings.Repeat("b", 64), EngineVersion: "2"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := scope.Digest()
	return commonclient.IssuedDevelopFileAuthorization{Scope: *scope, ScopeDigest: digest, Authorization: commonclient.IssuedExecutionAuthorization{
		ID: "101", ExecutionID: facts.ExecutionID, Audience: "develop", TenantID: facts.TenantID, SourceType: "user",
		ActorPrincipalID: facts.Actor.PrincipalID, TenantMembershipID: facts.Actor.TenantMembershipID,
		IssuedAuthorizationVersion: facts.Actor.AuthorizationVersion, ExpiresAt: time.Now().Add(10 * time.Minute),
		Accesses: []commonclient.ExecutionEngineAccessScope{{EngineID: "11", Effects: []string{"read"}}}}}
}

func fileAdmissionFixture(facts *commonclient.DevelopFileExecutionFacts) commonclient.DevelopFileAdmissionObservation {
	return commonclient.DevelopFileAdmissionObservation{AuthorizationID: "101", ExecutionID: facts.ExecutionID,
		ExpectedAttempt: 1, ScopeDigest: facts.ScopeDigest, State: "ready", ObservedAt: time.Now().Add(-time.Second), ExpiresAt: time.Now().Add(time.Minute)}
}

func TestFileExecutionPendingPrivateFactsAndOriginalActorBinding(t *testing.T) {
	db, repo, item, sources := filePendingFixture(t)
	facts, err := repo.CreatePending(t.Context(), item, sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := db.Model(&commonexecution.TaskExecution{}).Select("execution_config").Where("execution_id = ?", item.ExecutionID).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, facts.ConfigJSON) || facts.AdmissionState != "pending" || facts.AuthorizationID != "" || facts.BoundScope != nil {
		t.Fatal("configuration or unissued state changed")
	}
	sources[0].NodeID = "changed"
	facts.ConfigJSON[0] = '!'
	fresh, err := repo.Read(t.Context(), 7, item.ExecutionID)
	if err != nil || fresh.Intent.Sources[0].NodeID != "source" || !bytes.Equal(fresh.ConfigJSON, raw) {
		t.Fatalf("mutable projection reached private store: %v", err)
	}
	if _, err := repo.Read(t.Context(), 8, item.ExecutionID); err != gorm.ErrRecordNotFound {
		t.Fatalf("cross-tenant facts visible: %v", err)
	}
	issued := fileIssuedFixture(t, fresh)
	if err := repo.BindAuthorization(t.Context(), 7, item.ExecutionID, issued); err != nil {
		t.Fatal(err)
	}
	bound, err := repo.Read(t.Context(), 7, item.ExecutionID)
	if err != nil || bound.AuthorizationID != "101" || bound.ScopeDigest != issued.ScopeDigest {
		t.Fatalf("sealed binding missing: %v", err)
	}
	if err := repo.BindAuthorization(t.Context(), 7, item.ExecutionID, issued); err == nil {
		t.Fatal("binding silently replayed")
	}
	if err := repo.ApplyAdmission(t.Context(), 7, item.ExecutionID, fileAdmissionFixture(bound)); err != nil {
		t.Fatal(err)
	}
	ready, err := repo.Read(t.Context(), 7, item.ExecutionID)
	if err != nil || ready.AdmissionState != "ready" || ready.Intent.Target != nil {
		t.Fatalf("read-only admission failed: %v", err)
	}
}

func TestFileExecutionPendingFailureRollsBackCommonAndPrivateRows(t *testing.T) {
	db, repo, item, sources := filePendingFixture(t)
	item.ExecutionConfig["timeout"] = 1.5
	if facts, err := repo.CreatePending(t.Context(), item, sources, nil); err == nil || facts != nil {
		t.Fatal("invalid frozen timeout produced a pending scope")
	}
	for _, model := range []any{&commonexecution.TaskExecution{}, &models.FileExecution{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("non-atomic pending/intent: %d %v", count, err)
		}
	}
}

func TestFileExecutionBindingRejectsWrongIdentityAndChangedConfig(t *testing.T) {
	for _, kind := range []string{"tenant", "actor", "member", "version", "execution", "config"} {
		t.Run(kind, func(t *testing.T) {
			db, repo, item, sources := filePendingFixture(t)
			facts, err := repo.CreatePending(t.Context(), item, sources, nil)
			if err != nil {
				t.Fatal(err)
			}
			issued := fileIssuedFixture(t, facts)
			switch kind {
			case "tenant":
				issued.Authorization.TenantID = "8"
			case "actor":
				issued.Authorization.ActorPrincipalID = "32"
			case "member":
				issued.Authorization.TenantMembershipID = "42"
			case "version":
				issued.Authorization.IssuedAuthorizationVersion = "6"
			case "execution":
				issued.Authorization.ExecutionID = uuid.NewString()
			case "config":
				if err := db.Model(&commonexecution.TaskExecution{}).Where("execution_id = ?", item.ExecutionID).
					Update("execution_config", `{"timeout":61,"content":{}}`).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.BindAuthorization(t.Context(), 7, item.ExecutionID, issued); err == nil {
				t.Fatal("different original binding accepted")
			}
			var stored commonexecution.TaskExecution
			if err := db.Where("execution_id = ?", item.ExecutionID).First(&stored).Error; err != nil || stored.ExecutionAuthorizationID != nil {
				t.Fatalf("failed binding left partial EA: %v", err)
			}
		})
	}
}

func TestFileExecutionAdmissionRejectsLateOrCancelledAndCanCloseUnstarted(t *testing.T) {
	for _, kind := range []string{"expired", "cancelled", "config", "approval", "rejected"} {
		t.Run(kind, func(t *testing.T) {
			db, repo, item, sources := filePendingFixture(t)
			facts, err := repo.CreatePending(t.Context(), item, sources, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.BindAuthorization(t.Context(), 7, item.ExecutionID, fileIssuedFixture(t, facts)); err != nil {
				t.Fatal(err)
			}
			bound, _ := repo.Read(t.Context(), 7, item.ExecutionID)
			observation := fileAdmissionFixture(bound)
			switch kind {
			case "expired":
				observation.ExpiresAt = time.Now().Add(-time.Second)
			case "cancelled":
				_ = db.Model(&commonexecution.TaskExecution{}).Where("execution_id = ?", item.ExecutionID).Update("status", commonexecution.ExecutionStatusCancelled).Error
			case "config":
				_ = db.Model(&commonexecution.TaskExecution{}).Where("execution_id = ?", item.ExecutionID).Update("execution_config", `{"timeout":62}`).Error
			case "approval":
				observation.ApprovalID = uuid.NewString()
			case "rejected":
				observation.State = "rejected"
			}
			err = repo.ApplyAdmission(t.Context(), 7, item.ExecutionID, observation)
			if kind == "rejected" {
				if err != nil {
					t.Fatal(err)
				}
				var stored commonexecution.TaskExecution
				if err := db.Where("execution_id = ?", item.ExecutionID).First(&stored).Error; err != nil || stored.Status != "failed" || stored.Attempt != 0 || stored.StartedAt != nil {
					t.Fatalf("rejection forged a claim: %#v %v", stored, err)
				}
			} else if err == nil {
				t.Fatal("stale or expanded admission activated")
			}
		})
	}
}

func TestFileExecutionPendingRequiresOriginalHumanWorkflow(t *testing.T) {
	for _, kind := range []string{"principal", "member", "version", "source", "task-type", "attempts", "parent", "retry", "boundary"} {
		t.Run(kind, func(t *testing.T) {
			db, repo, item, sources := filePendingFixture(t)
			switch kind {
			case "principal":
				item.ActorPrincipalID = nil
			case "member":
				item.ActorTenantMembershipID = nil
			case "version":
				item.IssuedAuthorizationVersion = nil
			case "source":
				item.Source = commonexecution.ModuleOrchestrator
			case "task-type":
				item.TaskType = commonexecution.TaskTypeQuery
			case "attempts":
				item.MaxAttempts = 2
			case "parent":
				value := uuid.NewString()
				item.ParentExecutionID = &value
			case "retry":
				value := uuid.NewString()
				item.RetryOfExecutionID = &value
			case "boundary":
				item.ExecutionBoundary = "unbounded"
			}
			if _, err := repo.CreatePending(t.Context(), item, sources, nil); err == nil {
				t.Fatal("unsupported execution origin entered private purpose")
			}
			var count int64
			if err := db.Model(&commonexecution.TaskExecution{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("rejected origin created an execution")
			}
		})
	}
}

func TestFileExecutionQueueExcludesOnlyUnadmittedPurpose(t *testing.T) {
	db, repo, item, sources := filePendingFixture(t)
	runFileQueueCase(t, db, repo, item, sources)
}

func runFileQueueCase(t *testing.T, db *gorm.DB, repo *FileExecutionRepository, item *commonexecution.TaskExecution, sources []commonexecution.DevelopFileSourceIntent) {
	t.Helper()
	facts, err := repo.CreatePending(t.Context(), item, sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := repo.PendingAdmissions(t.Context(), 100); err != nil || len(rows) != 0 {
		t.Fatal("unbound execution observed", rows, err)
	}
	ordinary := commonexecution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: "develop", Source: "develop",
		TaskType: "workflow", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", MaxAttempts: 1,
		ExecutionConfig: commonmodels.JSONMap{"timeout": 60}}
	if err := commonexecution.CreatePending(t.Context(), db, &ordinary); err != nil {
		t.Fatal(err)
	}
	queue := NewExecutionRepository(db)
	claimed, _, err := queue.ClaimNext("workflow", t.Context(), "file-worker", time.Now(), time.Minute, nil)
	if err != nil || claimed == nil || claimed.ExecutionID != ordinary.ExecutionID {
		t.Fatalf("private purpose escaped pending or ordinary workflow changed: %#v %v", claimed, err)
	}
	if candidate, _, err := queue.ClaimNext("workflow", t.Context(), "file-worker", time.Now(), time.Minute, nil); err != nil || candidate != nil {
		t.Fatalf("unissued purpose claimed: %#v %v", candidate, err)
	}
	if err := repo.BindAuthorization(t.Context(), 7, item.ExecutionID, fileIssuedFixture(t, facts)); err != nil {
		t.Fatal(err)
	}
	if rows, err := repo.PendingAdmissions(t.Context(), 100); err != nil || len(rows) != 1 || rows[0].Facts.ExecutionID != item.ExecutionID {
		t.Fatal("original bound pending missing", rows, err)
	}
	if candidate, _, err := queue.ClaimNext("workflow", t.Context(), "file-worker", time.Now(), time.Minute, nil); err != nil || candidate != nil {
		t.Fatalf("bound but unadmitted purpose claimed: %#v %v", candidate, err)
	}
	bound, _ := repo.Read(t.Context(), 7, item.ExecutionID)
	if err := repo.ApplyAdmission(t.Context(), 7, item.ExecutionID, fileAdmissionFixture(bound)); err != nil {
		t.Fatal(err)
	}
	if rows, err := repo.PendingAdmissions(t.Context(), 100); err != nil || len(rows) != 0 {
		t.Fatal("fresh ready unnecessarily reobserved", rows, err)
	}
	if err := db.Model(&models.FileExecution{}).Where("execution_id = ?", item.ExecutionID).
		Update("admission_expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if candidate, _, err := queue.ClaimNext("workflow", t.Context(), "file-worker", time.Now(), time.Minute, nil); err != nil || candidate != nil {
		t.Fatalf("expired admission claimed: %#v %v", candidate, err)
	}
	if rows, err := repo.PendingAdmissions(t.Context(), 100); err != nil || len(rows) != 1 {
		t.Fatal("expired ready not scheduled for fresh observation", rows, err)
	}
	// An expired observation is not an Allow. A fresh System observation may
	// refresh pending eligibility; the original attempt and EA do not change.
	if err := repo.ApplyAdmission(t.Context(), 7, item.ExecutionID, fileAdmissionFixture(bound)); err != nil {
		t.Fatal(err)
	}
	claimed, lease, err := queue.ClaimNext("workflow", t.Context(), "file-worker", time.Now(), time.Minute, nil)
	if err != nil || claimed == nil || lease == nil || claimed.ExecutionID != item.ExecutionID || claimed.Attempt != 1 || claimed.StartedAt == nil {
		t.Fatalf("original admitted purpose was not claimed exactly once: %#v %v", claimed, err)
	}
	if err := db.Model(&models.FileExecution{}).Where("execution_id = ?", item.ExecutionID).Update("admission_expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if rows, err := repo.PendingAdmissions(t.Context(), 100); err != nil || len(rows) != 0 {
		t.Fatal("running execution entered pending observer", rows, err)
	}
	runPrivateRuntimeRecordCase(t, db, repo, claimed, *lease, bound)
}

func TestFileExecutionKnownUnissuedRejectionIsAtomicAndNeverStarts(t *testing.T) {
	for _, state := range []string{"unissued", "bound", "cancelled", "other-tenant"} {
		t.Run(state, func(t *testing.T) {
			db, repo, item, sources := filePendingFixture(t)
			facts, err := repo.CreatePending(t.Context(), item, sources, nil)
			if err != nil {
				t.Fatal(err)
			}
			tenant := uint(7)
			if state == "bound" {
				if err := repo.BindAuthorization(t.Context(), tenant, item.ExecutionID, fileIssuedFixture(t, facts)); err != nil {
					t.Fatal(err)
				}
			} else if state == "cancelled" {
				if err := db.Model(item).Update("status", "cancelled").Error; err != nil {
					t.Fatal(err)
				}
			} else if state == "other-tenant" {
				tenant = 8
			}
			err = repo.RejectUnissued(t.Context(), tenant, item.ExecutionID)
			if state != "unissued" {
				if err == nil {
					t.Fatal("non-rejectable execution changed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := repo.Read(t.Context(), 7, item.ExecutionID)
			if err != nil || fresh.AdmissionState != "rejected" || fresh.AuthorizationID != "" || fresh.BoundScope != nil {
				t.Fatalf("unissued fact changed: %v", err)
			}
			var stored commonexecution.TaskExecution
			if err := db.First(&stored, item.ID).Error; err != nil || stored.Status != "failed" || stored.Attempt != 0 || stored.StartedAt != nil || stored.LeaseToken != nil {
				t.Fatalf("rejection fabricated a claim: %v", err)
			}
			if err := repo.RejectUnissued(t.Context(), 7, item.ExecutionID); err == nil {
				t.Fatal("rejection silently replayed")
			}
		})
	}
}
