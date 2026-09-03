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
	"github.com/google/uuid"
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

func TestPhoneMetadataDetectorRecognizesADDPFlattenedPathWithoutChangingPhysicalComponentKey(t *testing.T) {
	detector := configuredDetector{
		Binding:    models.Detector{TenantID: 7, CapabilityKey: models.FindingDetectorPhoneMetadataV2, SensitiveDataTypeID: 9, ConfidenceThreshold: 0.9, Enabled: true},
		DataType:   models.SensitiveDataType{ID: 9, TenantID: 7},
		Capability: models.DetectorCapability{Key: models.FindingDetectorPhoneMetadataV2},
	}
	fields := []datatype.FieldInfo{
		{Name: "members__userInfo__phone", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "members__device__microphone", Type: datatype.FieldTypeString, Nullable: true},
	}
	facts := dataprotection.DataItemSecurityFacts{
		Fields: fields, SourceSnapshotHash: "sha256:test", ObservedAt: time.Now().UTC(),
	}
	findings, err := NewDiscoveryService(nil, nil).detectFieldFindings(
		models.ProtectionEnrollment{ID: "enrollment", TenantID: 7}, "execution", detector, facts,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}
	if findings[0].ComponentKey != "members__userInfo__phone" || findings[0].Component.Key != "members__userInfo__phone" {
		t.Fatalf("physical component key changed: %#v", findings[0])
	}
}

func TestEmailMetadataDetectorRecognizesExactSemanticAliases(t *testing.T) {
	detector := configuredDetector{
		Binding:    models.Detector{TenantID: 7, CapabilityKey: models.FindingDetectorEmailMetadataV1, SensitiveDataTypeID: 10, ConfidenceThreshold: 0.9, Enabled: true},
		DataType:   models.SensitiveDataType{ID: 10, TenantID: 7},
		Capability: models.DetectorCapability{Key: models.FindingDetectorEmailMetadataV1, Code: "email_metadata"},
	}
	fields := []datatype.FieldInfo{
		{Name: "email", Path: []string{"email"}, Type: datatype.FieldTypeString, Nullable: false},
		{Name: "customer__email_address", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "email_verified", Path: []string{"email_verified"}, Type: datatype.FieldTypeString, Nullable: true},
		{Name: "email_count", Path: []string{"email_count"}, Type: datatype.FieldTypeInt, Nullable: true},
	}
	facts := dataprotection.DataItemSecurityFacts{
		Fields: fields, SourceSnapshotHash: "sha256:email-test", ObservedAt: time.Now().UTC(),
	}
	findings, err := NewDiscoveryService(nil, nil).detectFieldFindings(
		models.ProtectionEnrollment{ID: "enrollment", TenantID: 7}, "execution", detector, facts,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %#v", findings)
	}
	if findings[0].ComponentKey != "email" || findings[1].ComponentKey != "customer__email_address" {
		t.Fatalf("email component keys = %#v", findings)
	}
	if findings[1].Evidence["semantic_terminal"] != "email_address" || findings[1].Evidence["matched_alias"] != "emailaddress" {
		t.Fatalf("email evidence = %#v", findings[1].Evidence)
	}
}

func TestEmailMetadataFindingCompilesGenericSuppressionForEveryStructuredOutlet(t *testing.T) {
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
	dataType, err := definitions.CreateType(models.SensitiveDataTypeRequest{Code: "email", Name: "邮箱", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.CreateDetector(models.DetectorRequest{CapabilityKey: models.FindingDetectorEmailMetadataV1, SensitiveDataTypeID: dataType.ID, ConfidenceThreshold: 0.9}, 7, 11); err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.CreateBaseline(models.ProtectionBaselineRequest{SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID, Effect: dataprotection.EffectSuppress, InvalidValueEffect: dataprotection.EffectSuppress}, 7, 11); err != nil {
		t.Fatal(err)
	}

	enrollments := NewEnrollmentService(db)
	created, err := enrollments.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(2, resourcetree.TypeTable, "business.customers"))
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
		{Name: "customer_code", Path: []string{"customer_code"}, Type: datatype.FieldTypeString, Nullable: false},
		{Name: "email", Path: []string{"email"}, Type: datatype.FieldTypeString, Nullable: false},
	}
	hash, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	facts := &dataprotection.DataItemSecurityFacts{
		SchemaVersion: dataprotection.DataItemSecurityFactsSchemaV1, ItemFingerprint: created.Target.ResourceIdentity,
		ItemType: string(resourcetree.TypeTable), Fields: fields, SourceSnapshotHash: hash, ObservedAt: time.Now().UTC(),
	}
	discoveries := NewDiscoveryService(db, func(uint) SecurityFactsReader { return staticSecurityFactsReader{facts: facts} })
	execution, lease, err := discoveries.ClaimNext(context.Background(), "security-email-test", time.Now().UTC(), time.Minute)
	if err != nil || execution == nil || lease == nil {
		t.Fatalf("claim = %#v/%#v, err=%v", execution, lease, err)
	}
	if err := discoveries.Execute(context.Background(), execution, *lease); err != nil {
		t.Fatal(err)
	}

	listed, err := discoveries.ListFindings(context.Background(), 7, FindingListFilter{EnrollmentID: created.ID, DiscoveryExecutionID: execution.ExecutionID}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || listed.Data[0].ComponentKey != "email" || listed.Data[0].DetectorVersion != models.FindingDetectorEmailMetadataV1 {
		t.Fatalf("email findings = %#v", listed)
	}
	for owner, assertion := range map[string]func(*testing.T, *dataprotection.Projection, string){
		"manager":  requireManagerProjectionEffects,
		"develop":  requireDevelopQueryProjection,
		"service":  requireServiceExecuteProjection,
		"transfer": requireTransferExportProjection,
	} {
		changes, err := enrollments.ListChanges(context.Background(), 7, owner, "", 20)
		if err != nil || len(changes.Changes) != 2 {
			t.Fatalf("%s changes = %#v, err=%v", owner, changes, err)
		}
		assertion(t, changes.Changes[1].Projection, dataprotection.EffectSuppress)
	}
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
	dataType, err := definitions.CreateType(models.SensitiveDataTypeRequest{Code: "contact_number", Name: "联系电话", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.CreateDetector(models.DetectorRequest{CapabilityKey: models.FindingDetectorPhoneMetadataV2, SensitiveDataTypeID: dataType.ID, ConfidenceThreshold: 0.9}, 7, 11); err != nil {
		t.Fatal(err)
	}
	baseline, err := definitions.CreateBaseline(models.ProtectionBaselineRequest{SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID, Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1, KeepPrefix: 3, KeepSuffix: 4, InvalidValueEffect: dataprotection.EffectSuppress}, 7, 11)
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

	listed, err := discoveries.ListFindings(context.Background(), 7, FindingListFilter{EnrollmentID: created.ID, SourceSnapshotHash: hash, DiscoveryExecutionID: execution.ExecutionID}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || listed.Data[0].ComponentKey != "userInfo.phone" || listed.Data[0].Confidence != 1 {
		t.Fatalf("findings = %#v", listed)
	}
	explanation := listed.Data[0].Explanation
	if explanation.Capability == nil || explanation.Capability.Key != models.FindingDetectorPhoneMetadataV2 ||
		explanation.AutomaticAdoptionThreshold == nil || *explanation.AutomaticAdoptionThreshold != 0.9 || !explanation.MeetsAutomaticThreshold ||
		explanation.DecisionState != models.FindingDecisionAutomatic || explanation.GovernanceSource != models.FindingGovernanceDetectorDefault ||
		explanation.EffectiveSensitiveDataTypeID == nil || *explanation.EffectiveSensitiveDataTypeID != dataType.ID ||
		explanation.EffectiveSecurityClassificationID == nil || *explanation.EffectiveSecurityClassificationID != classification.ID ||
		explanation.EffectiveSecurityGradeID == nil || *explanation.EffectiveSecurityGradeID != grade.ID ||
		explanation.Baseline == nil || explanation.Baseline.ID != baseline.ID || explanation.Baseline.Effect != dataprotection.EffectMask {
		t.Fatalf("finding explanation = %#v", explanation)
	}
	managerOutlet := findingOutlet(t, explanation, "manager")
	if managerOutlet.ProjectionState != dataprotection.ProjectionStateActive || managerOutlet.Acknowledged ||
		findingOutletRule(t, managerOutlet, managerPreviewAction).Effect != dataprotection.EffectMask ||
		findingOutletRule(t, managerOutlet, managerProfileAction).Effect != dataprotection.EffectSuppress {
		t.Fatalf("manager finding explanation = %#v", managerOutlet)
	}
	if findingOutletRule(t, findingOutlet(t, explanation, "develop"), developQueryAction).Effect != dataprotection.EffectMask ||
		findingOutletRule(t, findingOutlet(t, explanation, "service"), serviceExecuteAction).Effect != dataprotection.EffectMask ||
		findingOutletRule(t, findingOutlet(t, explanation, "transfer"), transferExportAction).Effect != dataprotection.EffectMask {
		t.Fatalf("outlet finding explanation = %#v", explanation.Outlets)
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

func findingOutlet(t *testing.T, explanation models.SensitiveFindingExplanation, owner string) models.FindingOutletProtection {
	t.Helper()
	for _, outlet := range explanation.Outlets {
		if outlet.ConsumerOwner == owner {
			return outlet
		}
	}
	t.Fatalf("missing %s outlet in %#v", owner, explanation.Outlets)
	return models.FindingOutletProtection{}
}

func findingOutletRule(t *testing.T, outlet models.FindingOutletProtection, action string) models.FindingOutletProtectionRule {
	t.Helper()
	for _, rule := range outlet.Rules {
		if rule.Action == action {
			return rule
		}
	}
	t.Fatalf("missing %s rule in %#v", action, outlet.Rules)
	return models.FindingOutletProtectionRule{}
}

func TestFindingListRejectsInvalidCurrentSnapshotFilters(t *testing.T) {
	discoveries := NewDiscoveryService(openSecurityTestDB(t), nil)
	if _, err := discoveries.ListFindings(context.Background(), 7, FindingListFilter{EnrollmentID: "not-a-uuid"}, 1, 20); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid enrollment filter error = %v", err)
	}
	if _, err := discoveries.ListFindings(context.Background(), 7, FindingListFilter{SourceSnapshotHash: "sha256:not-a-hash"}, 1, 20); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid snapshot filter error = %v", err)
	}
	if _, err := discoveries.ListFindings(context.Background(), 7, FindingListFilter{DiscoveryExecutionID: "not-a-uuid"}, 1, 20); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid discovery execution filter error = %v", err)
	}
	if _, err := discoveries.ListFindings(context.Background(), 7, FindingListFilter{SnapshotScope: "latest"}, 1, 20); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid snapshot scope error = %v", err)
	}
	if _, err := discoveries.ListFindings(context.Background(), 7, FindingListFilter{ReviewState: "open"}, 1, 20); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid review state error = %v", err)
	}
	invalidTypeID := int64(0)
	if _, err := discoveries.ListFindings(context.Background(), 7, FindingListFilter{SensitiveDataTypeID: &invalidTypeID}, 1, 20); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid sensitive type error = %v", err)
	}
}

func TestDiscoveryQualityUsesLatestWorkloadAndDeduplicatedHumanEvidence(t *testing.T) {
	db := openSecurityTestDB(t)
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	enrollmentID := uuid.NewString()
	currentExecutionID := uuid.NewString()
	oldExecutionID := uuid.NewString()
	snapshotHash := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := db.Create(&models.ProtectionEnrollment{
		ID: enrollmentID, TenantID: 7, TargetOwner: "meta", TargetType: "data_item", TargetIdentity: "item",
		TargetEngineID: 2, TargetItemType: "table", TargetFullName: "outdoor.people", State: models.EnrollmentStateActive,
		Version: 1, LatestSourceSnapshotHash: snapshotHash, LatestDiscoveryExecutionID: currentExecutionID,
		CreatedBy: 11, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	newFinding := func(componentKey, executionID string, createdAt time.Time) models.SensitiveFinding {
		return models.SensitiveFinding{
			ID: uuid.NewString(), TenantID: 7, EnrollmentID: enrollmentID, DiscoveryExecutionID: executionID,
			ComponentKey: componentKey, SensitiveDataTypeID: 9, DetectorCode: "addp.detector.phone_metadata",
			DetectorVersion: models.FindingDetectorPhoneMetadataV2, Confidence: 1,
			Evidence:           map[string]interface{}{"schema": models.FindingEvidenceSchemaV1},
			Component:          dataprotection.Component{Key: componentKey, Path: []dataprotection.PathSegment{{Name: componentKey, Container: "scalar"}}, ValueType: "string"},
			SourceSnapshotHash: snapshotHash, ObservedAt: createdAt, CreatedAt: createdAt,
		}
	}
	oldPhone := newFinding("contact.phone", oldExecutionID, now)
	currentPhone := newFinding("contact.phone", currentExecutionID, now.Add(time.Minute))
	currentPending := newFinding("backup.phone", currentExecutionID, now.Add(2*time.Minute))
	adjusted := newFinding("assistant.phone", oldExecutionID, now.Add(3*time.Minute))
	rejected := newFinding("device.telephone", oldExecutionID, now.Add(4*time.Minute))
	for _, finding := range []models.SensitiveFinding{oldPhone, currentPhone, currentPending, adjusted, rejected} {
		if err := db.Create(&finding).Error; err != nil {
			t.Fatal(err)
		}
	}
	reviews := []models.SensitiveFindingReview{
		{ID: uuid.NewString(), TenantID: 7, FindingID: oldPhone.ID, Decision: models.FindingReviewDecisionReject, Rationale: "旧结论", ReviewedBy: 21, CreatedAt: now.Add(5 * time.Minute)},
		{ID: uuid.NewString(), TenantID: 7, FindingID: currentPhone.ID, Decision: models.FindingReviewDecisionConfirm, Rationale: "最新结论", ReviewedBy: 21, CreatedAt: now.Add(6 * time.Minute)},
		{ID: uuid.NewString(), TenantID: 7, FindingID: adjusted.ID, Decision: models.FindingReviewDecisionAdjust, Rationale: "确认敏感但调整定义", ReviewedBy: 21, CreatedAt: now.Add(7 * time.Minute)},
		{ID: uuid.NewString(), TenantID: 7, FindingID: rejected.ID, Decision: models.FindingReviewDecisionReject, Rationale: "误识别", ReviewedBy: 21, CreatedAt: now.Add(8 * time.Minute)},
	}
	for _, review := range reviews {
		if err := db.Create(&review).Error; err != nil {
			t.Fatal(err)
		}
	}

	for index, conclusion := range []string{models.AssessmentConclusionSensitive, models.AssessmentConclusionNotSensitive} {
		assessmentID := uuid.NewString()
		assessment := models.ResourceSecurityAssessment{
			ID: assessmentID, TenantID: 7, EnrollmentID: enrollmentID, ComponentKey: "manual." + conclusion,
			Version: 1, CurrentRevision: 1, CreatedBy: 21, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&assessment).Error; err != nil {
			t.Fatal(err)
		}
		revision := models.ResourceSecurityAssessmentRevision{
			ID: uuid.NewString(), TenantID: 7, AssessmentID: assessmentID, Revision: 1,
			SourceKind: models.AssessmentRevisionSourceManual, Conclusion: conclusion,
			SensitiveDataTypeID: 9, SecurityClassificationID: 3, SecurityGradeID: 4,
			SourceSnapshotHash: snapshotHash,
			Component:          dataprotection.Component{Key: assessment.ComponentKey, Path: []dataprotection.PathSegment{{Name: assessment.ComponentKey, Container: "scalar"}}, ValueType: "string"},
			Rationale:          "人工补充", CreatedBy: int64(21 + index), CreatedAt: now,
		}
		if err := db.Create(&revision).Error; err != nil {
			t.Fatal(err)
		}
	}

	typeID := int64(9)
	summary, err := NewDiscoveryService(db, nil).GetQualitySummary(context.Background(), 7, &typeID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.CurrentFindingCount != 2 || summary.AwaitingReviewCount != 1 || summary.ReviewedSampleCount != 3 ||
		summary.ConfirmedCount != 1 || summary.AdjustedCount != 1 || summary.RejectedCount != 1 ||
		summary.ActiveManualAssessmentCount != 1 || summary.RevokedManualAssessmentCount != 1 {
		t.Fatalf("quality summary = %#v", summary)
	}
	if summary.SensitiveConfirmationRate == nil || *summary.SensitiveConfirmationRate != float64(2)/3 || len(summary.Capabilities) != 1 {
		t.Fatalf("quality rate/capabilities = %#v", summary)
	}
	for sequence, owner := range requiredProtectionOwners {
		record := models.ProtectionProjectionRecord{
			ID: uuid.NewString(), TenantID: 7, EnrollmentID: enrollmentID, ConsumerOwner: owner,
			Revision: "1", State: dataprotection.ProjectionStateEnrolling, ProjectionPayload: `{"rules":[]}`,
			PublishedSequence: int64(sequence + 1), CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	typeID = 9
	queue, err := NewDiscoveryService(db, nil).ListFindings(context.Background(), 7, FindingListFilter{
		SnapshotScope: models.FindingSnapshotScopeCurrent, ReviewState: models.FindingReviewStatePending,
		SensitiveDataTypeID: &typeID, DetectorVersion: models.FindingDetectorPhoneMetadataV2,
	}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if queue.Total != 1 || queue.Data[0].ID != currentPending.ID || queue.Data[0].Review != nil {
		t.Fatalf("current pending queue = %#v", queue)
	}
	if queue.Data[0].TargetSnapshot.EngineID != 2 || queue.Data[0].TargetSnapshot.ItemType != "table" || queue.Data[0].TargetSnapshot.FullName != "outdoor.people" {
		t.Fatalf("queue target snapshot = %#v", queue.Data[0].TargetSnapshot)
	}
	emptyQueue, err := NewDiscoveryService(db, nil).ListFindings(context.Background(), 7, FindingListFilter{
		SnapshotScope: models.FindingSnapshotScopeCurrent, ReviewState: models.FindingReviewStatePending, DetectorVersion: models.FindingDetectorPhoneDocumentV1,
	}, 1, 20)
	if err != nil || emptyQueue.Total != 0 {
		t.Fatalf("filtered empty queue = %#v, err=%v", emptyQueue, err)
	}
	capability := summary.Capabilities[0]
	if capability.CapabilityKey != models.FindingDetectorPhoneMetadataV2 || capability.CurrentFindingCount != 2 ||
		capability.AwaitingReviewCount != 1 || capability.ReviewedSampleCount != 3 || capability.RejectedCount != 1 ||
		capability.SensitiveConfirmationRate == nil || *capability.SensitiveConfirmationRate != float64(2)/3 {
		t.Fatalf("capability quality = %#v", capability)
	}

	empty, err := NewDiscoveryService(db, nil).GetQualitySummary(context.Background(), 8, nil)
	if err != nil || empty.SensitiveConfirmationRate != nil || len(empty.Capabilities) != 0 {
		t.Fatalf("empty quality = %#v, err=%v", empty, err)
	}
	invalidTypeID := int64(0)
	if _, err := NewDiscoveryService(db, nil).GetQualitySummary(context.Background(), 7, &invalidTypeID); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid type error = %v", err)
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
	dataType, err := definitions.CreateType(models.SensitiveDataTypeRequest{Code: "phone", Name: "手机号", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.CreateDetector(models.DetectorRequest{CapabilityKey: models.FindingDetectorPhoneDocumentV1, SensitiveDataTypeID: dataType.ID, ConfidenceThreshold: 0.9}, 7, 11); err != nil {
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

	listed, err := discoveries.ListFindings(context.Background(), 7, FindingListFilter{EnrollmentID: created.ID, SourceSnapshotHash: sampleHash, DiscoveryExecutionID: execution.ExecutionID}, 1, 20)
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

func TestDetectorBindingControlsDiscoveryWithoutSensitiveTypeCodeFallback(t *testing.T) {
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
	dataType, err := definitions.CreateType(models.SensitiveDataTypeRequest{Code: "phone", Name: "手机号", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.CreateBaseline(models.ProtectionBaselineRequest{SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID, Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1, KeepPrefix: 3, KeepSuffix: 4, InvalidValueEffect: dataprotection.EffectSuppress}, 7, 11); err != nil {
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
	}
	hash, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	facts := &dataprotection.DataItemSecurityFacts{SchemaVersion: dataprotection.DataItemSecurityFactsSchemaV1, ItemFingerprint: created.Target.ResourceIdentity, ItemType: "collection", Fields: fields, SourceSnapshotHash: hash, ObservedAt: time.Now().UTC()}
	discoveries := NewDiscoveryService(db, func(uint) SecurityFactsReader { return staticSecurityFactsReader{facts: facts} })

	withoutBinding, lease, err := discoveries.ClaimNext(context.Background(), "security-binding-test", time.Now().UTC(), time.Minute)
	if err != nil || withoutBinding == nil || lease == nil {
		t.Fatalf("claim without binding = %#v/%#v, err=%v", withoutBinding, lease, err)
	}
	if err := discoveries.Execute(context.Background(), withoutBinding, *lease); err != nil {
		t.Fatal(err)
	}
	if findings, err := discoveries.ListFindings(context.Background(), 7, FindingListFilter{EnrollmentID: created.ID, SourceSnapshotHash: hash, DiscoveryExecutionID: withoutBinding.ExecutionID}, 1, 20); err != nil || findings.Total != 0 {
		t.Fatalf("findings without binding = %#v, err=%v", findings, err)
	}

	binding, err := definitions.CreateDetector(models.DetectorRequest{CapabilityKey: models.FindingDetectorPhoneMetadataV2, SensitiveDataTypeID: dataType.ID, ConfidenceThreshold: 0.9}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	withBinding, lease, err := discoveries.ClaimNext(context.Background(), "security-binding-test", time.Now().UTC(), time.Minute)
	if err != nil || withBinding == nil || lease == nil {
		t.Fatalf("claim with binding = %#v/%#v, err=%v", withBinding, lease, err)
	}
	if err := discoveries.Execute(context.Background(), withBinding, *lease); err != nil {
		t.Fatal(err)
	}
	if findings, err := discoveries.ListFindings(context.Background(), 7, FindingListFilter{EnrollmentID: created.ID, SourceSnapshotHash: hash, DiscoveryExecutionID: withBinding.ExecutionID}, 1, 20); err != nil || findings.Total != 1 || findings.Data[0].SensitiveDataTypeID != dataType.ID {
		t.Fatalf("findings with binding = %#v, err=%v", findings, err)
	}

	disabled := false
	binding, err = definitions.UpdateDetector(binding.ID, 7, 11, models.DetectorRequest{
		CapabilityKey: binding.CapabilityKey, SensitiveDataTypeID: binding.SensitiveDataTypeID, ConfidenceThreshold: binding.ConfidenceThreshold, Enabled: &disabled, Version: binding.Version,
	})
	if err != nil || binding.Enabled {
		t.Fatalf("disable binding = %#v, err=%v", binding, err)
	}
	withoutBindingAgain, lease, err := discoveries.ClaimNext(context.Background(), "security-binding-test", time.Now().UTC(), time.Minute)
	if err != nil || withoutBindingAgain == nil || lease == nil {
		t.Fatalf("claim after disable = %#v/%#v, err=%v", withoutBindingAgain, lease, err)
	}
	if err := discoveries.Execute(context.Background(), withoutBindingAgain, *lease); err != nil {
		t.Fatal(err)
	}
	if findings, err := discoveries.ListFindings(context.Background(), 7, FindingListFilter{EnrollmentID: created.ID, SourceSnapshotHash: hash, DiscoveryExecutionID: withoutBindingAgain.ExecutionID}, 1, 20); err != nil || findings.Total != 0 {
		t.Fatalf("findings after disable = %#v, err=%v", findings, err)
	}
	managerChanges, err := enrollments.ListChanges(context.Background(), 7, "manager", "", 20)
	if err != nil || len(managerChanges.Changes) != 4 || managerChanges.Changes[3].Projection == nil || managerChanges.Changes[3].Projection.State != dataprotection.ProjectionStateEnrolling {
		t.Fatalf("manager changes after disable = %#v, err=%v", managerChanges, err)
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
