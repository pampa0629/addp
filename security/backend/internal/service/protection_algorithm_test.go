package service

import (
	"context"
	"os"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/execution"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestFieldAlgorithmPermissionsRetentionAndBaselineRevocation(t *testing.T) {
	verifyFieldAlgorithmPermissions(t, openSecurityTestDB(t))
}
func TestFieldAlgorithmsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SECURITY_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("SECURITY_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("DROP SCHEMA IF EXISTS security CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(tx); err != nil {
		t.Fatal(err)
	}
	if err := execution.EnsureStore(tx); err != nil {
		t.Fatal(err)
	}
	// Discovery execution ownership uses the existing common startup schema.
	verifyFieldAlgorithmPermissions(t, tx)
}
func verifyFieldAlgorithmPermissions(t *testing.T, db *gorm.DB) {
	t.Helper()
	db, enrollments, finding, kind, grade := prepareReviewablePhoneFindingOnDB(t, db)
	definitions := newTestDefinitionService(db)
	baselines, err := definitions.ListBaselines(7)
	if err != nil {
		t.Fatal(err)
	}
	baseline := baselines[0]
	request := models.ProtectionBaselineRequest{SensitiveDataTypeID: kind.ID, SecurityGradeID: grade.ID, Effect: baseline.Effect, Algorithm: baseline.Algorithm, Parameters: baseline.Parameters, AllowedAlgorithms: []string{baseline.Algorithm, dataprotection.AlgorithmConstantV1, dataprotection.AlgorithmSM3V1}, InvalidValueEffect: baseline.InvalidValueEffect, Version: baseline.Version}
	updatedBaseline, err := definitions.UpdateBaseline(baseline.ID, 7, 11, request)
	if err != nil {
		t.Fatal(err)
	}
	baseline = *updatedBaseline
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认字段"})
	if err != nil {
		t.Fatal(err)
	}
	policies := NewPolicyService(db)
	tooMuch := models.CreateProtectionPolicyRequest{AssessmentID: reviewed.Assessment.ID, ConsumerOwner: "manager", Action: "preview", Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]any{"prefix_runes": 4, "suffix_runes": 4, "mask_rune": "*"}, Rationale: "超出前缀上限"}
	if _, err := policies.Create(context.Background(), 7, 31, tooMuch); err != commonapi.ErrBadRequest {
		t.Fatalf("retention limit rejection: %v", err)
	}
	tooMuch.Algorithm = dataprotection.AlgorithmSM3V1
	tooMuch.Parameters = map[string]any{}
	created, err := policies.Create(context.Background(), 7, 31, tooMuch)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := enrollments.ListChanges(context.Background(), 7, "manager", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	latest := changes.Changes[len(changes.Changes)-1].Projection
	if latest.Rules[0].Decision.Algorithm != dataprotection.AlgorithmSM3V1 {
		t.Fatal("independent hash did not reach preview projection")
	}
	document := map[string]any{"userInfo": map[string]any{"phone": "abc"}}
	if err := dataprotection.ProtectDocument(document, "preview", latest.Rules, dataprotection.SubjectReference{}); err != nil {
		t.Fatal(err)
	}
	if document["userInfo"].(map[string]any)["phone"] != "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0" {
		t.Fatal("hash was not executed")
	}
	request.Version = baseline.Version
	request.AllowedAlgorithms = []string{baseline.Algorithm}
	if _, err := definitions.UpdateBaseline(baseline.ID, 7, 11, request); err != nil {
		t.Fatal(err)
	}
	changes, err = enrollments.ListChanges(context.Background(), 7, "manager", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if changes.Changes[len(changes.Changes)-1].Projection.Rules[0].Decision.Algorithm != dataprotection.AlgorithmKeepPrefixSuffixV2 {
		t.Fatal("permission withdrawal did not restore baseline")
	}
	if _, err := policies.Update(context.Background(), 7, 31, created.ID, models.UpdateProtectionPolicyRequest{Version: created.Version, Effect: "mask", Algorithm: dataprotection.AlgorithmSM3V1, Parameters: map[string]any{}, Rationale: "不再许可"}); err != commonapi.ErrBadRequest {
		t.Fatalf("withdrawn algorithm accepted: %v", err)
	}
	if _, err := policies.Revoke(context.Background(), 7, 31, created.ID, models.RevokeProtectionPolicyRequest{Version: created.Version, Rationale: "撤销字段规则"}); err != nil {
		t.Fatal(err)
	}
}

func TestBaselineAlgorithmConfigurationRejectsImplicitPermissionAndGeometry(t *testing.T) {
	request := models.ProtectionBaselineRequest{Effect: "mask", Algorithm: dataprotection.AlgorithmSM3V1, Parameters: map[string]any{}, AllowedAlgorithms: []string{dataprotection.AlgorithmSM3V1}}
	if err := validateBaselineConfiguration(request); err != nil {
		t.Fatal(err)
	}
	request.AllowedAlgorithms = nil
	if validateBaselineConfiguration(request) == nil {
		t.Fatal("default algorithm must be explicitly permitted")
	}
	request.AllowedAlgorithms = []string{dataprotection.AlgorithmSM3V1, dataprotection.AlgorithmKeepPrefixSuffixV2}
	if validateBaselineConfiguration(request) == nil {
		t.Fatal("hash baseline cannot permit prefix masking without retention bounds")
	}
	baseline := models.ProtectionBaseline{Effect: "mask", Algorithm: dataprotection.AlgorithmSM3V1, Parameters: map[string]any{}, AllowedAlgorithms: []string{dataprotection.AlgorithmSM3V1}}
	if validatePolicyDecision(baseline, "geometry", policyDecision("mask", dataprotection.AlgorithmSM3V1, nil, "suppress")) == nil {
		t.Fatal("geometry must not be hashed")
	}
	baseline.Effect = "suppress"
	if validatePolicyDecision(baseline, "string", policyDecision("mask", dataprotection.AlgorithmSM3V1, nil, "suppress")) == nil {
		t.Fatal("suppression baseline must not be weakened")
	}
}

func TestDenyBaselineDefaultsToDenyOnInvalidValues(t *testing.T) {
	db := openSecurityTestDB(t)
	definitions := newTestDefinitionService(db)
	classification, err := definitions.CreateClassification(models.DefinitionRequest{Code: "private", Name: "Private"}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	grade, err := definitions.CreateGrade(models.DefinitionRequest{Code: "l1", Name: "L1", RiskOrder: 1}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	kind, err := definitions.CreateType(models.CreateSensitiveDataTypeRequest{Code: "private", Name: "Private", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID, DefaultProtection: &models.DefaultProtectionRequest{Effect: "deny"}}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	baselines, err := definitions.ListBaselines(7)
	if err != nil || len(baselines) != 1 || baselines[0].SensitiveDataTypeID != kind.ID || baselines[0].InvalidValueEffect != "deny" {
		t.Fatal("deny baseline must not default to suppression")
	}
}
