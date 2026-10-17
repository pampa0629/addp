package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

func TestOneApprovalPublishesOnlyExplicitManagerAndDevelopGrants(t *testing.T) {
	assertOneApprovalPublishesExplicitOutlets(t, openSecurityTestDB(t))
}

func assertOneApprovalPublishesExplicitOutlets(t *testing.T, db *gorm.DB) {
	t.Helper()
	db, _, finding, _, _ := prepareReviewablePhoneFindingOnDB(t, db)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(t.Context(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认字段"})
	if err != nil {
		t.Fatal(err)
	}
	requests := NewAccessRequestService(db, testAccessActorResolver)
	until := time.Now().UTC().Add(time.Hour)
	body := models.CreateProtectionAccessRequest{AssessmentID: reviewed.Assessment.ID, Scopes: []models.ProtectionAccessScope{{ConsumerOwner: "manager", Action: "preview"}, {ConsumerOwner: "develop", Action: "query"}}, RequestedExpiresAt: until, Rationale: "核验预览和查询"}
	for _, scope := range []models.ProtectionAccessScope{{ConsumerOwner: "transfer", Action: "export"}, {ConsumerOwner: "service", Action: "service_execute"}, {ConsumerOwner: "graph", Action: "graph_read"}, {ConsumerOwner: "manager", Action: "profile"}} {
		invalid := body
		invalid.Scopes = []models.ProtectionAccessScope{scope}
		if _, err := requests.Create(t.Context(), 7, 41, invalid); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("unexpected scope permitted: %v", err)
		}
	}
	created, err := requests.Create(t.Context(), 7, 41, body)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := requests.Decide(t.Context(), 7, 42, created.ID, models.DecideProtectionAccessRequest{Version: created.Version, Decision: "approve", ExpiresAt: until, Rationale: "两个范围分别批准"})
	if err != nil {
		t.Fatal(err)
	}
	if len(approved.Scopes) != 2 || len(approved.Authorizations) != 2 {
		t.Fatalf("missing precise grants: %+v", approved)
	}
	if _, err := requests.Create(t.Context(), 7, 41, body); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("active grants could be silently renewed: %v", err)
	}
	for owner, action := range map[string]string{"manager": "preview", "develop": "query", "service": "service_execute", "transfer": "export"} {
		var record models.ProtectionProjectionRecord
		if err := db.Where("tenant_id = ? AND enrollment_id = ? AND consumer_owner = ?", 7, reviewed.Assessment.EnrollmentID, owner).First(&record).Error; err != nil {
			t.Fatal(err)
		}
		var projection dataprotection.Projection
		if err := json.Unmarshal([]byte(record.ProjectionPayload), &projection); err != nil {
			t.Fatal(err)
		}
		rule := projectionRule(t, &projection, action)
		want := owner == "manager" || owner == "develop"
		if got := rule.EffectiveDecision(dataprotection.SubjectReference{Type: "user", ID: "41"}, time.Now().UTC()).Effect == dataprotection.EffectAllow; got != want {
			t.Fatalf("%s plaintext=%v want=%v", owner, got, want)
		}
		if rule.EffectiveDecision(dataprotection.SubjectReference{Type: "user", ID: "43"}, time.Now().UTC()).Effect == dataprotection.EffectAllow {
			t.Fatal("grant leaked to another user")
		}
	}
	developGrant := approved.Authorizations[0]
	if developGrant.ConsumerOwner != "develop" {
		t.Fatal("scopes not canonical")
	}
	exemptions := NewExemptionService(db)
	grant, err := exemptions.Get(t.Context(), 7, developGrant.ExemptionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exemptions.Revoke(t.Context(), 7, 42, grant.ID, models.RevokeProtectionExemptionRequest{Version: grant.Version, Rationale: "结束查询用途"}); err != nil {
		t.Fatal(err)
	}
	history, err := requests.GetForReview(t.Context(), 7, 42, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if history.Authorizations[0].AuthorizationState != "revoked" || history.Authorizations[1].AuthorizationState != "active" {
		t.Fatalf("outlet statuses not independent: %+v", history.Authorizations)
	}
	for _, state := range []string{"active", "revoked"} {
		page, err := requests.ListReviewQueue(t.Context(), 7, 42, models.ProtectionAccessRequestReviewFilter{Scope: "history", AuthorizationState: state}, 1, 20)
		if err != nil || page.Total != 1 || len(page.Data) != 1 || page.Data[0].ID != created.ID {
			t.Fatalf("per-outlet history filtering: state=%s page=%+v err=%v", state, page, err)
		}
	}
}

func TestMultiOutletApprovalRollsBackEveryGrantOnFailure(t *testing.T) {
	db, _, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(t.Context(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认字段"})
	if err != nil {
		t.Fatal(err)
	}
	requests := NewAccessRequestService(db, testAccessActorResolver)
	until := time.Now().UTC().Add(time.Hour)
	created, err := requests.Create(t.Context(), 7, 41, models.CreateProtectionAccessRequest{AssessmentID: reviewed.Assessment.ID, Scopes: []models.ProtectionAccessScope{{ConsumerOwner: "manager", Action: "preview"}, {ConsumerOwner: "develop", Action: "query"}}, RequestedExpiresAt: until, Rationale: "双出口"})
	if err != nil {
		t.Fatal(err)
	}
	creates := 0
	if err := db.Callback().Create().Before("gorm:create").Register("test_fail_second_grant", func(tx *gorm.DB) {
		if tx.Statement.Table == "protection_exemption_revisions" {
			creates++
			if creates == 2 {
				tx.AddError(errors.New("second grant failed"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Create().Remove("test_fail_second_grant") })
	if _, err := requests.Decide(t.Context(), 7, 42, created.ID, models.DecideProtectionAccessRequest{Version: created.Version, Decision: "approve", ExpiresAt: until, Rationale: "批准"}); err == nil {
		t.Fatal("partial approval unexpectedly succeeded")
	}
	var count int64
	if err := db.Model(&models.ProtectionExemption{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("partial grants persisted: %d %v", count, err)
	}
	var row models.ProtectionAccessRequest
	if err := db.First(&row, "id = ?", created.ID).Error; err != nil || row.State != "pending" {
		t.Fatalf("partial request decision persisted: %+v %v", row, err)
	}
}

func TestAddingDevelopScopeDoesNotWidenOrRenewExistingManagerGrant(t *testing.T) {
	db, _, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(t.Context(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认字段"})
	if err != nil {
		t.Fatal(err)
	}
	requests := NewAccessRequestService(db, testAccessActorResolver)
	until := time.Now().UTC().Add(time.Hour)
	approve := func(scope models.ProtectionAccessScope) *models.ProtectionAccessRequestResponse {
		t.Helper()
		created, err := requests.Create(t.Context(), 7, 41, models.CreateProtectionAccessRequest{AssessmentID: reviewed.Assessment.ID, Scopes: []models.ProtectionAccessScope{scope}, RequestedExpiresAt: until, Rationale: "显式选择单一范围"})
		if err != nil {
			t.Fatal(err)
		}
		approved, err := requests.Decide(t.Context(), 7, 42, created.ID, models.DecideProtectionAccessRequest{Version: created.Version, Decision: "approve", ExpiresAt: until, Rationale: "独立批准"})
		if err != nil {
			t.Fatal(err)
		}
		return approved
	}
	manager := approve(models.ProtectionAccessScope{ConsumerOwner: "manager", Action: "preview"})
	var before models.ProtectionProjectionRecord
	if err := db.First(&before, "tenant_id = ? AND enrollment_id = ? AND consumer_owner = ?", 7, reviewed.Assessment.EnrollmentID, "manager").Error; err != nil {
		t.Fatal(err)
	}
	develop := approve(models.ProtectionAccessScope{ConsumerOwner: "develop", Action: "query"})
	var after models.ProtectionProjectionRecord
	if err := db.First(&after, "tenant_id = ? AND enrollment_id = ? AND consumer_owner = ?", 7, reviewed.Assessment.EnrollmentID, "manager").Error; err != nil {
		t.Fatal(err)
	}
	if before.Revision != after.Revision || before.ProjectionPayload != after.ProjectionPayload {
		t.Fatal("Develop approval changed existing Manager grant")
	}
	if len(develop.Authorizations) != 1 || develop.Authorizations[0].ConsumerOwner != "develop" || manager.Authorizations[0].ExemptionID == develop.Authorizations[0].ExemptionID {
		t.Fatal("single-outlet approval widened its scope")
	}
}
