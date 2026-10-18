package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/resourcetree"
	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

func TestGraphOwnerProjectionReconciliation(t *testing.T) {
	assertGraphOwnerProjectionReconciliation(t, openSecurityTestDB(t))
}
func assertGraphOwnerProjectionReconciliation(t *testing.T, db *gorm.DB) {
	t.Helper()
	svc := NewEnrollmentService(db)
	now := time.Now().UTC()
	for i, state := range []string{models.EnrollmentStateActivating, models.EnrollmentStateEnrolling, models.EnrollmentStateActive, models.EnrollmentStateReleasing, models.EnrollmentStateReleased} {
		created, err := svc.Create(t.Context(), 73, 11, testDataItemEnrollmentRequest(uint(800+i), resourcetree.TypeGraph, "graph.backfill"))
		if err != nil {
			t.Fatal(err)
		}
		initialState := state
		if state == models.EnrollmentStateReleasing {
			initialState = models.EnrollmentStateActivating
		}
		if err = db.Model(&models.ProtectionEnrollment{}).Where("id = ?", created.ID).Updates(map[string]any{"state": initialState, "created_at": now.Add(-2 * time.Hour), "protection_expires_at": now.Add(-time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
		if state == models.EnrollmentStateReleasing {
			if _, err = svc.Release(t.Context(), 73, 11, created.ID, models.ReleaseProtectionEnrollmentRequest{Version: created.Version, Basis: models.ReleaseBasisManual, Reason: "existing release"}); err != nil {
				t.Fatal(err)
			}
		}
		// Model the previous release's four-owner records and feeds.
		if err = db.Where("enrollment_id = ? AND consumer_owner = ?", created.ID, graphProtectionOwner).Delete(&models.ProtectionProjectionRecord{}).Error; err != nil {
			t.Fatal(err)
		}
		if err = db.Where("enrollment_id = ? AND consumer_owner = ?", created.ID, graphProtectionOwner).Delete(&models.ProtectionProjectionChange{}).Error; err != nil {
			t.Fatal(err)
		}
		var before []models.ProtectionProjectionRecord
		if err = db.Where("enrollment_id = ?", created.ID).Order("consumer_owner").Find(&before).Error; err != nil {
			t.Fatal(err)
		}
		if err = ReconcileGraphOwnerProjections(t.Context(), db, now); err != nil {
			t.Fatal(err)
		}
		var after []models.ProtectionProjectionRecord
		if err = db.Where("enrollment_id = ? AND consumer_owner <> ?", created.ID, graphProtectionOwner).Order("consumer_owner").Find(&after).Error; err != nil {
			t.Fatal(err)
		}
		for j, row := range after {
			if row.ProjectionPayload != before[j].ProjectionPayload || row.Revision != before[j].Revision || row.PublishedSequence != before[j].PublishedSequence {
				t.Fatal("existing owner changed")
			}
		}
		var graph []models.ProtectionProjectionRecord
		if err = db.Where("enrollment_id = ? AND consumer_owner = ?", created.ID, graphProtectionOwner).Find(&graph).Error; err != nil {
			t.Fatal(err)
		}
		if state == models.EnrollmentStateReleased {
			if len(graph) != 0 {
				t.Fatal("released resource revived")
			}
			continue
		}
		if len(graph) != 1 {
			t.Fatalf("Graph records=%d", len(graph))
		}
		var projection dataprotection.Projection
		if err = json.Unmarshal([]byte(graph[0].ProjectionPayload), &projection); err != nil {
			t.Fatal(err)
		}
		if projection.ExpiresAt == nil || !projection.ExpiresAt.Before(now) || !projection.ValidFrom.Before(*projection.ExpiresAt) {
			t.Fatal("expired protection was renewed")
		}
		if state == models.EnrollmentStateReleasing && graph[0].ReleaseSequence == nil {
			t.Fatal("release feed not installed")
		}
		if state == models.EnrollmentStateReleasing {
			for _, owner := range allRequiredProtectionOwners() {
				changes, err := svc.ListChanges(t.Context(), 73, owner, "", 500)
				if err != nil {
					t.Fatal(err)
				}
				if err = svc.Acknowledge(t.Context(), 73, owner, changes.NextCursor); err != nil {
					t.Fatal(err)
				}
			}
			current, err := svc.Get(t.Context(), 73, created.ID)
			if err != nil || current.State != models.EnrollmentStateReleased {
				t.Fatal("existing release stalled after Graph acknowledgement", err)
			}
		}
		seq := graph[0].PublishedSequence
		if err = ReconcileGraphOwnerProjections(t.Context(), db, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if err = db.Where("id = ?", graph[0].ID).First(&graph[0]).Error; err != nil || graph[0].PublishedSequence != seq {
			t.Fatal("reconciliation is not idempotent", err)
		}
	}
}

func TestGraphProjectionDoesNotAdvertiseFieldMaskingOrExemption(t *testing.T) {
	db, _, finding, _, _ := prepareReviewablePhoneFinding(t)
	if _, err := NewAssessmentService(db, nil).ReviewFinding(t.Context(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认保护"}); err != nil {
		t.Fatal(err)
	}
	changes, err := NewEnrollmentService(db).ListChanges(t.Context(), 7, graphProtectionOwner, "", 20)
	if err != nil || len(changes.Changes) < 2 {
		t.Fatal("Graph feed missing", err)
	}
	projection := changes.Changes[len(changes.Changes)-1].Projection
	if projection == nil || projection.State != dataprotection.ProjectionStateActive || len(projection.Rules) != 1 {
		t.Fatalf("Graph projection=%+v", projection)
	}
	rule := projection.Rules[0]
	if rule.Action != graphReadAction || rule.Decision.Effect != dataprotection.EffectDeny || len(rule.Authorizations) != 0 {
		t.Fatalf("Graph overstates its protection capability: %+v", rule)
	}
	if validPolicyBinding(graphProtectionOwner, graphReadAction) {
		t.Fatal("Graph raw-value exemption admitted")
	}
}
