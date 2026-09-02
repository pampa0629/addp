package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	commonexecution "github.com/addp/common/execution"
	"github.com/addp/common/resourcetree"
	"github.com/addp/security/internal/models"
)

type staticSecurityFactsReader struct {
	facts  *dataprotection.DataItemSecurityFacts
	sample *dataprotection.DataItemSecuritySample
}

func (r staticSecurityFactsReader) GetDataItemSecurityFacts(context.Context, string) (*dataprotection.DataItemSecurityFacts, error) {
	return r.facts, nil
}

func (r staticSecurityFactsReader) GetDataItemSecuritySample(context.Context, string) (*dataprotection.DataItemSecuritySample, error) {
	if r.sample == nil {
		return nil, errors.New("security sample is unavailable")
	}
	return r.sample, nil
}

func TestDiscoveryCreatesValueFreeFindingAndManagerActiveProjection(t *testing.T) {
	db := openSecurityTestDB(t)
	definitions := newTestDefinitionService(db)
	classification, err := definitions.CreateClassification(models.DefinitionRequest{Code: "personal", Name: "个人信息"}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	grade, err := definitions.CreateGrade(models.DefinitionRequest{Code: "l3", Name: "三级", RiskOrder: 3}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	dataType, err := definitions.CreateType(models.SensitiveDataTypeRequest{Code: "phone", Name: "手机号", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID, ProtectionThreshold: 0.9}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	_, err = definitions.CreateBaseline(models.ProtectionBaselineRequest{SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID, Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1, KeepPrefix: 3, KeepSuffix: 4, InvalidValueEffect: dataprotection.EffectSuppress}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}

	enrollments := NewEnrollmentService(db)
	created, err := enrollments.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeCollection, "Outdoor.Persons"))
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range requiredProtectionOwners {
		changes, err := enrollments.ListChanges(context.Background(), 7, owner, "", 20)
		if err != nil {
			t.Fatal(err)
		}
		if err := enrollments.Acknowledge(context.Background(), 7, owner, changes.NextCursor); err != nil {
			t.Fatal(err)
		}
	}

	fields := []datatype.FieldInfo{
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
		{Name: "userInfo.nickName", Path: []string{"userInfo", "nickName"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	hash, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	facts := &dataprotection.DataItemSecurityFacts{SchemaVersion: dataprotection.DataItemSecurityFactsSchemaV1, ItemFingerprint: created.Target.ResourceIdentity, ItemType: "collection", Fields: fields, SourceSnapshotHash: hash, ObservedAt: time.Now().UTC()}
	discoveries := NewDiscoveryService(db, func(uint) SecurityFactsReader { return staticSecurityFactsReader{facts: facts} })
	execution, lease, err := discoveries.ClaimNext(context.Background(), "security-test", time.Now().UTC(), time.Minute)
	if err != nil || execution == nil || lease == nil {
		var queued []commonexecution.TaskExecution
		_ = db.Find(&queued).Error
		t.Fatalf("claim = %#v/%#v, err=%v, queued=%#v", execution, lease, err, queued)
	}
	if err := discoveries.Execute(context.Background(), execution, *lease); err != nil {
		t.Fatal(err)
	}

	listed, err := discoveries.ListFindings(context.Background(), 7, created.ID, hash, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || listed.Data[0].ComponentKey != "userInfo.phone" || listed.Data[0].Confidence != 1 {
		t.Fatalf("findings = %#v", listed)
	}
	evidence, _ := json.Marshal(listed.Data[0].Evidence)
	if strings.Contains(string(evidence), "13661384499") {
		t.Fatal("finding evidence leaked a raw phone value")
	}
	managerChanges, err := enrollments.ListChanges(context.Background(), 7, "manager", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(managerChanges.Changes) != 2 || managerChanges.Changes[1].Projection == nil || managerChanges.Changes[1].Projection.State != dataprotection.ProjectionStateActive {
		t.Fatalf("manager changes = %#v", managerChanges.Changes)
	}
	projection := managerChanges.Changes[1].Projection
	requireManagerProjectionEffects(t, projection, dataprotection.EffectMask)
	previewRule := managerProjectionRule(t, projection, managerPreviewAction)
	if previewRule.Component.Key != "userInfo.phone" || previewRule.Decision.Parameters["exact_runes"] != float64(11) && previewRule.Decision.Parameters["exact_runes"] != 11 {
		t.Fatalf("active projection = %#v", projection)
	}
	developChanges, err := enrollments.ListChanges(context.Background(), 7, "develop", "", 20)
	if err != nil || len(developChanges.Changes) != 2 {
		t.Fatalf("develop changes = %#v, err=%v", developChanges, err)
	}
	requireDevelopQueryProjection(t, developChanges.Changes[1].Projection, dataprotection.EffectMask)
	serviceChanges, err := enrollments.ListChanges(context.Background(), 7, "service", "", 20)
	if err != nil || len(serviceChanges.Changes) != 2 {
		t.Fatalf("service changes = %#v, err=%v", serviceChanges, err)
	}
	requireServiceExecuteProjection(t, serviceChanges.Changes[1].Projection, dataprotection.EffectMask)
	transferChanges, err := enrollments.ListChanges(context.Background(), 7, "transfer", "", 20)
	if err != nil || len(transferChanges.Changes) != 2 {
		t.Fatalf("transfer changes = %#v, err=%v", transferChanges, err)
	}
	requireTransferExportProjection(t, transferChanges.Changes[1].Projection, dataprotection.EffectMask)
	stored, err := commonexecution.NewTaskExecutionRepository(db).GetByExecutionID(context.Background(), execution.ExecutionID, 7)
	if err != nil || stored.Status != commonexecution.ExecutionStatusSuccess {
		t.Fatalf("execution = %#v, err=%v", stored, err)
	}
	current, err := enrollments.Get(context.Background(), 7, created.ID)
	if err != nil || current.State != models.EnrollmentStateEnrolling {
		t.Fatalf("enrollment = %#v, err=%v", current, err)
	}
}

func TestFindingListRejectsInvalidCurrentSnapshotFilters(t *testing.T) {
	discoveries := NewDiscoveryService(openSecurityTestDB(t), nil)
	if _, err := discoveries.ListFindings(context.Background(), 7, "not-a-uuid", "", 1, 20); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid enrollment filter error = %v", err)
	}
	if _, err := discoveries.ListFindings(context.Background(), 7, "", "sha256:not-a-hash", 1, 20); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid snapshot filter error = %v", err)
	}
}

func TestDocumentDiscoveryCreatesSearchIndexProjectionWithoutPersistingSampleText(t *testing.T) {
	db := openSecurityTestDB(t)
	definitions := newTestDefinitionService(db)
	classification, err := definitions.CreateClassification(models.DefinitionRequest{Code: "personal", Name: "个人信息"}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	grade, err := definitions.CreateGrade(models.DefinitionRequest{Code: "l3", Name: "三级", RiskOrder: 3}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	dataType, err := definitions.CreateType(models.SensitiveDataTypeRequest{Code: "phone", Name: "手机号", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID, ProtectionThreshold: 0.9}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	_, err = definitions.CreateBaseline(models.ProtectionBaselineRequest{SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID, Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1, KeepPrefix: 3, KeepSuffix: 4, InvalidValueEffect: dataprotection.EffectSuppress}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}

	enrollments := NewEnrollmentService(db)
	created, err := enrollments.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeFile, "documents/contact.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range requiredProtectionOwners {
		changes, err := enrollments.ListChanges(context.Background(), 7, owner, "", 20)
		if err != nil {
			t.Fatal(err)
		}
		if err := enrollments.Acknowledge(context.Background(), 7, owner, changes.NextCursor); err != nil {
			t.Fatal(err)
		}
	}

	observedAt := time.Now().UTC()
	phone := "13661384499"
	sampleText := "联系电话：" + phone + "，备用 13501206490。"
	sampleHash, err := dataprotection.DocumentTextSnapshotHash(sampleText, false)
	if err != nil {
		t.Fatal(err)
	}
	facts := &dataprotection.DataItemSecurityFacts{
		SchemaVersion: dataprotection.DataItemSecurityFactsSchemaV1, ItemFingerprint: created.Target.ResourceIdentity, ItemType: "file",
		Fields: nil, SourceSnapshotHash: "", ObservedAt: observedAt,
	}
	sample := &dataprotection.DataItemSecuritySample{
		SchemaVersion: dataprotection.DataItemSecuritySampleSchemaV1, ItemFingerprint: created.Target.ResourceIdentity, ItemType: "file",
		Text: sampleText, Truncated: false, SourceSnapshotHash: sampleHash, ObservedAt: observedAt,
	}
	discoveries := NewDiscoveryService(db, func(uint) SecurityFactsReader { return staticSecurityFactsReader{facts: facts, sample: sample} })
	execution, lease, err := discoveries.ClaimNext(context.Background(), "security-document-test", time.Now().UTC(), time.Minute)
	if err != nil || execution == nil || lease == nil {
		t.Fatalf("claim = %#v/%#v, err=%v", execution, lease, err)
	}
	if err := discoveries.Execute(context.Background(), execution, *lease); err != nil {
		t.Fatal(err)
	}

	listed, err := discoveries.ListFindings(context.Background(), 7, created.ID, sampleHash, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || listed.Data[0].ComponentKey != dataprotection.DocumentTextComponentKey || listed.Data[0].Evidence["match_count"] != float64(2) && listed.Data[0].Evidence["match_count"] != 2 {
		t.Fatalf("document findings = %#v", listed)
	}
	evidence, err := json.Marshal(listed.Data[0].Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(evidence), phone) || strings.Contains(string(evidence), sampleText) {
		t.Fatalf("document finding persisted sample text: %s", evidence)
	}

	managerChanges, err := enrollments.ListChanges(context.Background(), 7, "manager", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(managerChanges.Changes) != 2 || managerChanges.Changes[1].Projection == nil {
		t.Fatalf("manager changes = %#v", managerChanges.Changes)
	}
	projection := managerChanges.Changes[1].Projection
	if projection.State != dataprotection.ProjectionStateActive || projection.SourceSnapshotHash != sampleHash || len(projection.Rules) != 1 {
		t.Fatalf("document projection = %#v", projection)
	}
	rule := managerProjectionRule(t, projection, managerSearchIndexAction)
	if rule.Component.Key != dataprotection.DocumentTextComponentKey || rule.Decision.Effect != dataprotection.EffectMask || rule.Decision.Algorithm != dataprotection.AlgorithmPhoneOccurrencesV1 {
		t.Fatalf("document search rule = %#v", rule)
	}
	for _, forbiddenAction := range []string{managerPreviewAction, managerProfileAction} {
		for _, candidate := range projection.Rules {
			if candidate.Action == forbiddenAction {
				t.Fatalf("document projection contains forbidden %s rule: %#v", forbiddenAction, projection.Rules)
			}
		}
	}
	developChanges, err := enrollments.ListChanges(context.Background(), 7, "develop", "", 20)
	if err != nil || len(developChanges.Changes) != 2 || developChanges.Changes[1].Projection == nil || developChanges.Changes[1].Projection.State != dataprotection.ProjectionStateEnrolling {
		t.Fatalf("document develop changes = %#v, err=%v", developChanges, err)
	}
	serviceChanges, err := enrollments.ListChanges(context.Background(), 7, "service", "", 20)
	if err != nil || len(serviceChanges.Changes) != 2 || serviceChanges.Changes[1].Projection == nil || serviceChanges.Changes[1].Projection.State != dataprotection.ProjectionStateEnrolling {
		t.Fatalf("document service changes = %#v, err=%v", serviceChanges, err)
	}
	transferChanges, err := enrollments.ListChanges(context.Background(), 7, "transfer", "", 20)
	if err != nil || len(transferChanges.Changes) != 2 || transferChanges.Changes[1].Projection == nil || transferChanges.Changes[1].Projection.State != dataprotection.ProjectionStateEnrolling {
		t.Fatalf("document transfer changes = %#v, err=%v", transferChanges, err)
	}
	current, err := enrollments.Get(context.Background(), 7, created.ID)
	if err != nil || current.LatestSourceSnapshotHash != sampleHash {
		t.Fatalf("enrollment = %#v, err=%v", current, err)
	}
}

func TestDiscoveryRecoveryRetriesThenFailsAtAttemptLimit(t *testing.T) {
	db := openSecurityTestDB(t)
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	sourceTaskID := "enrollment-recovery"
	execution := commonexecution.TaskExecution{
		TenantID: 7, ExecutionID: "security-discovery-recovery",
		Module: commonexecution.ModuleSecurity, TaskType: commonexecution.TaskTypeSensitiveDataDiscovery,
		Source: commonexecution.ModuleSecurity, SourceTaskID: &sourceTaskID,
		Status: commonexecution.ExecutionStatusPending, Progress: 0,
		ExecutionBoundary: commonexecution.ExecutionBoundaryBounded,
		TriggerType:       commonexecution.TriggerTypeEvent, MaxAttempts: 2,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	discoveries := NewDiscoveryService(db, nil)

	first, _, err := discoveries.ClaimNext(context.Background(), "worker-a", now.Add(time.Minute), time.Minute)
	if err != nil || first == nil || first.Attempt != 1 {
		t.Fatalf("first claim = %#v, err=%v", first, err)
	}
	if count, err := discoveries.RecoverExpired(context.Background(), now.Add(3*time.Minute), 100); err != nil || count != 1 {
		t.Fatalf("first recovery = %d, %v", count, err)
	}
	var stored commonexecution.TaskExecution
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != commonexecution.ExecutionStatusPending || stored.Attempt != 1 || stored.LeaseOwner != nil || stored.LeaseToken != nil || stored.LeaseExpiresAt != nil {
		t.Fatalf("retried execution = %#v", stored)
	}

	second, _, err := discoveries.ClaimNext(context.Background(), "worker-b", now.Add(4*time.Minute), time.Minute)
	if err != nil || second == nil || second.Attempt != 2 {
		t.Fatalf("second claim = %#v, err=%v", second, err)
	}
	failedAt := now.Add(6 * time.Minute)
	if count, err := discoveries.RecoverExpired(context.Background(), failedAt, 100); err != nil || count != 1 {
		t.Fatalf("second recovery = %d, %v", count, err)
	}
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != commonexecution.ExecutionStatusFailed || stored.Attempt != 2 || stored.CompletedAt == nil || stored.ExecutionTimeMs == nil || stored.LeaseOwner != nil || stored.LeaseToken != nil || stored.LeaseExpiresAt != nil {
		t.Fatalf("failed execution = %#v", stored)
	}
	if code, ok := stored.ErrorDetails["error_code"].(string); !ok || code != "worker_lease_expired" {
		t.Fatalf("error details = %#v", stored.ErrorDetails)
	}
}
