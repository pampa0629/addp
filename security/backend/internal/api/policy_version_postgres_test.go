package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/dataprotection"
	authmiddleware "github.com/addp/common/middleware/auth"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Invoked inside the existing HTTP PostgreSQL gate's rollback transaction.
func verifyPolicyHTTPVersions(t *testing.T, db *gorm.DB, kind models.SensitiveDataType, baseline models.ProtectionBaseline) {
	t.Helper()
	updatedBaseline, err := service.NewDefinitionService(db).UpdateBaseline(baseline.ID, 7, 11, models.ProtectionBaselineRequest{Version: baseline.Version, SensitiveDataTypeID: kind.ID, SecurityGradeID: baseline.SecurityGradeID, Effect: baseline.Effect, Algorithm: baseline.Algorithm, Parameters: baseline.Parameters, AllowedAlgorithms: []string{baseline.Algorithm, dataprotection.AlgorithmConstantV1}, InvalidValueEffect: baseline.InvalidValueEffect})
	if err != nil {
		t.Fatal(err)
	}
	baseline = *updatedBaseline
	enrollment := models.ProtectionEnrollment{ID: uuid.NewString(), TenantID: 7, TargetOwner: "meta", TargetType: "data_item", TargetIdentity: "policy-http-fixture", State: models.EnrollmentStateActive, Version: 1, LatestSourceSnapshotHash: "sha256:policy-http", CreatedBy: 11}
	assessment := models.ResourceSecurityAssessment{ID: uuid.NewString(), TenantID: 7, EnrollmentID: enrollment.ID, ComponentKey: "userInfo.phone", Version: 1, CurrentRevision: 1, CreatedBy: 11}
	revision := models.ResourceSecurityAssessmentRevision{ID: uuid.NewString(), TenantID: 7, AssessmentID: assessment.ID, Revision: 1, Conclusion: models.AssessmentConclusionSensitive, SourceKind: models.AssessmentRevisionSourceManual, SensitiveDataTypeID: kind.ID, SecurityClassificationID: kind.SecurityClassificationID, SecurityGradeID: kind.DefaultSecurityGradeID, SourceSnapshotHash: enrollment.LatestSourceSnapshotHash, Component: dataprotection.Component{Key: "userInfo.phone", Path: []dataprotection.PathSegment{{Name: "userInfo", Container: "object"}, {Name: "phone", Container: "scalar"}}, ValueType: "string", SchemaFingerprint: "sha256:policy-http-component"}, Rationale: "fixture", CreatedBy: 11}
	projection := models.ProtectionProjectionRecord{ID: uuid.NewString(), TenantID: 7, EnrollmentID: enrollment.ID, ConsumerOwner: "manager", Revision: "00000000000000000001", State: dataprotection.ProjectionStateActive, ProjectionPayload: `{}`}
	for _, row := range []any{&enrollment, &assessment, &revision, &projection} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	handler := NewPolicyHandler(service.NewPolicyService(db))
	call := func(method, id string, body []byte, tenant string, handle gin.HandlerFunc, status int) []byte {
		t.Helper()
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(method, "/", bytes.NewReader(body))
		context.Request.Header.Set("Content-Type", "application/json")
		context.Params = gin.Params{{Key: "id", Value: id}}
		actor := authtest.NewTenantUserAuthContext(tenant, "11", []string{"security.policy.create", "security.policy.read", "security.policy.update", "security.policy.delete"})
		if err := authmiddleware.SetAuthContextForGin(context, actor); err != nil {
			t.Fatal(err)
		}
		handle(context)
		if recorder.Code != status {
			t.Fatalf("%s status=%d, want %d: %s", method, recorder.Code, status, recorder.Body.String())
		}
		return recorder.Body.Bytes()
	}
	create, err := json.Marshal(models.CreateProtectionPolicyRequest{AssessmentID: assessment.ID, ConsumerOwner: "manager", Action: "preview", Effect: "mask", Algorithm: baseline.Algorithm, Parameters: baseline.Parameters, Rationale: "initial mask"})
	if err != nil {
		t.Fatal(err)
	}
	created := decodeDefinitionResponse[models.ProtectionPolicyResponse](t, call(http.MethodPost, "", create, "7", handler.Create, http.StatusCreated))
	// Reproduce the frontend's literal numeric version and change its algorithm.
	update := []byte(`{"version":1,"effect":"mask","algorithm":"addp.mask.constant/v1","parameters":{"value":"validation"},"invalid_value_effect":"suppress","rationale":"constant replacement"}`)
	response := call(http.MethodPut, created.ID, update, "7", handler.Update, http.StatusOK)
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(response, &wire); err != nil {
		t.Fatal(err)
	}
	updated := decodeDefinitionResponse[models.ProtectionPolicyResponse](t, response)
	if string(wire["version"]) != "2" || updated.Current.Algorithm != dataprotection.AlgorithmConstantV1 || updated.Current.Parameters["value"] != "validation" || len(updated.History) != 2 {
		t.Fatalf("update must persist replacement and return numeric new version: %s", response)
	}
	call(http.MethodPut, created.ID, []byte(`{"version":"2","effect":"deny","rationale":"string version"}`), "7", handler.Update, http.StatusBadRequest)
	conflict := decodeDefinitionResponse[map[string]string](t, call(http.MethodPut, created.ID, update, "7", handler.Update, http.StatusConflict))
	if conflict["error_code"] != "resource_version_conflict" {
		t.Fatalf("conflict=%v", conflict)
	}
	call(http.MethodPut, created.ID, update, "8", handler.Update, http.StatusNotFound)
	var policy models.ProtectionPolicy
	var count int64
	if err := db.First(&policy, "id = ?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ProtectionPolicyRevision{}).Where("policy_id = ?", created.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if policy.Version != 2 || count != 2 {
		t.Fatal("invalid, stale or cross-tenant request changed policy or immutable history")
	}
	revoked := decodeDefinitionResponse[models.ProtectionPolicyResponse](t, call(http.MethodDelete, created.ID, []byte(`{"version":2,"rationale":"restore baseline"}`), "7", handler.Revoke, http.StatusOK))
	if revoked.Version != 3 || revoked.State != models.ProtectionPolicyStateRevoked || len(revoked.History) != 3 {
		t.Fatalf("revoke=%#v", revoked)
	}
	if err := db.First(&projection, "id = ?", projection.ID).Error; err != nil {
		t.Fatal(err)
	}
	var compiled dataprotection.Projection
	if err := json.Unmarshal([]byte(projection.ProjectionPayload), &compiled); err != nil {
		t.Fatal(err)
	}
	for _, rule := range compiled.Rules {
		if rule.Action == "preview" && rule.Decision.Algorithm == baseline.Algorithm {
			return
		}
	}
	t.Fatal("revoke did not restore baseline preview projection")
}
