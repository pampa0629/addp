package service

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/resourcetree"
	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

func TestEnrollmentRequiresEveryOwnerAcknowledgementBeforeEnrolling(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := NewEnrollmentService(db)
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	created, err := svc.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeCollection, "Outdoor.Persons"))
	if err != nil {
		t.Fatal(err)
	}
	if created.State != models.EnrollmentStateActivating || len(created.OwnerProgress) != 4 {
		t.Fatalf("created state/progress = %s/%d", created.State, len(created.OwnerProgress))
	}
	if created.TargetSnapshot.EngineID != 11 || created.TargetSnapshot.ItemType != "collection" || created.TargetSnapshot.FullName != "Outdoor.Persons" {
		t.Fatalf("target snapshot = %#v", created.TargetSnapshot)
	}
	for _, progress := range created.OwnerProgress {
		if progress.ProjectionState != dataprotection.ProjectionStateEnrolling || len(progress.Effects) != 1 || progress.Effects[0] != dataprotection.EffectDeny {
			t.Fatalf("initial owner progress = %#v", progress)
		}
	}

	for index, owner := range requiredProtectionOwners {
		changes, err := svc.ListChanges(context.Background(), 7, owner, "", 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(changes.Changes) != 1 || changes.Changes[0].Projection == nil || changes.Changes[0].Projection.State != dataprotection.ProjectionStateEnrolling {
			t.Fatalf("owner %s changes = %#v", owner, changes.Changes)
		}
		if err := svc.Acknowledge(context.Background(), 7, owner, changes.NextCursor); err != nil {
			t.Fatal(err)
		}
		current, err := svc.Get(context.Background(), 7, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := models.EnrollmentStateActivating
		if index == len(requiredProtectionOwners)-1 {
			want = models.EnrollmentStateEnrolling
		}
		if current.State != want {
			t.Fatalf("after owner %s state = %s, want %s", owner, current.State, want)
		}
	}
}

func TestExplicitRediscoveryIsUniqueAndRenewsLatestSchemaProjection(t *testing.T) {
	db, enrollments, finding, _, _ := prepareReviewablePhoneFinding(t)
	assessments := NewAssessmentService(db)
	if _, err := assessments.ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段"}); err != nil {
		t.Fatal(err)
	}
	current, err := enrollments.Get(context.Background(), 7, finding.EnrollmentID)
	if err != nil {
		t.Fatal(err)
	}
	fields := []datatype.FieldInfo{
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
		{Name: "userInfo.nickName", Path: []string{"userInfo", "nickName"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	newHash, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	facts := &dataprotection.DataItemSecurityFacts{
		SchemaVersion: dataprotection.DataItemSecurityFactsSchemaV1, ItemFingerprint: testDataItemFingerprint(11, "Outdoor.Persons"),
		ItemType: "collection", Fields: fields, SourceSnapshotHash: newHash, ObservedAt: time.Now().UTC(),
	}
	discoveries := NewDiscoveryService(db, func(uint) SecurityFactsReader { return staticSecurityFactsReader{facts: facts} })
	created, err := enrollments.CreateDiscoveryExecution(context.Background(), 7, 41, current.ID, models.CreateProtectionDiscoveryExecutionRequest{Version: current.Version})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != "pending" || created.EnrollmentID != current.ID {
		t.Fatalf("rediscovery = %#v", created)
	}
	afterCreate, err := enrollments.Get(context.Background(), 7, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enrollments.CreateDiscoveryExecution(context.Background(), 7, 41, current.ID, models.CreateProtectionDiscoveryExecutionRequest{Version: afterCreate.Version}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate rediscovery error = %v", err)
	}
	execution, lease, err := discoveries.ClaimNext(context.Background(), "security-rediscovery-test", time.Now().UTC(), time.Minute)
	if err != nil || execution == nil || lease == nil || execution.ExecutionID != created.ExecutionID {
		t.Fatalf("claim = %#v/%#v, err=%v", execution, lease, err)
	}
	if err := discoveries.Execute(context.Background(), execution, *lease); err != nil {
		t.Fatal(err)
	}
	completed, err := enrollments.Get(context.Background(), 7, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.LatestSourceSnapshotHash != newHash || completed.LastDiscoveredAt == nil || completed.Version != afterCreate.Version+1 {
		t.Fatalf("completed enrollment = %#v", completed)
	}
	changes, err := enrollments.ListChanges(context.Background(), 7, "manager", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	last := changes.Changes[len(changes.Changes)-1].Projection
	if last == nil || last.SourceSnapshotHash != newHash {
		t.Fatalf("renewed projection = %#v", last)
	}
	requireManagerProjectionEffects(t, last, dataprotection.EffectMask)
}

func TestEnrollmentReleaseWaitsForEveryOwnerAndRemovesFeedProjection(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := NewEnrollmentService(db)
	created, err := svc.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeCollection, "Outdoor.Persons"))
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range requiredProtectionOwners {
		changes, _ := svc.ListChanges(context.Background(), 7, owner, "", 20)
		if err := svc.Acknowledge(context.Background(), 7, owner, changes.NextCursor); err != nil {
			t.Fatal(err)
		}
	}
	current, _ := svc.Get(context.Background(), 7, created.ID)
	releasing, err := svc.Release(context.Background(), 7, 31, created.ID, models.ReleaseProtectionEnrollmentRequest{Version: current.Version, Basis: models.ReleaseBasisManual, Reason: "no longer protected"})
	if err != nil {
		t.Fatal(err)
	}
	if releasing.State != models.EnrollmentStateReleasing {
		t.Fatalf("release state = %s", releasing.State)
	}
	for index, owner := range requiredProtectionOwners {
		first, _ := svc.ListChanges(context.Background(), 7, owner, "", 1)
		second, err := svc.ListChanges(context.Background(), 7, owner, first.NextCursor, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(second.Changes) != 1 || second.Changes[0].Operation != dataprotection.ChangeOperationRelease || second.Changes[0].Release == nil {
			t.Fatalf("owner %s release changes = %#v", owner, second.Changes)
		}
		if err := svc.Acknowledge(context.Background(), 7, owner, second.NextCursor); err != nil {
			t.Fatal(err)
		}
		current, _ = svc.Get(context.Background(), 7, created.ID)
		want := models.EnrollmentStateReleasing
		if index == len(requiredProtectionOwners)-1 {
			want = models.EnrollmentStateReleased
		}
		if current.State != want {
			t.Fatalf("after release owner %s state = %s, want %s", owner, current.State, want)
		}
	}
}

func TestZeroFindingDiscoverySummaryAndAuditedRelease(t *testing.T) {
	db, svc, current := prepareZeroFindingEnrollment(t)
	if current.DiscoverySummary.Status != models.DiscoverySummaryStatusCompleted || current.DiscoverySummary.FindingCount != 0 {
		t.Fatalf("discovery summary = %#v", current.DiscoverySummary)
	}
	listed, err := svc.List(context.Background(), 7, models.EnrollmentListScopeCurrent, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Data) != 1 || listed.Data[0].DiscoverySummary != current.DiscoverySummary {
		t.Fatalf("listed discovery summary = %#v", listed.Data)
	}

	releasing, err := svc.Release(context.Background(), 7, 41, current.ID, models.ReleaseProtectionEnrollmentRequest{
		Version: current.Version,
		Basis:   models.ReleaseBasisNoSupportedFindings,
		Reason:  "已结合业务语义复核，当前无需保护",
	})
	if err != nil {
		t.Fatal(err)
	}
	if releasing.State != models.EnrollmentStateReleasing || releasing.ReleaseBasis != models.ReleaseBasisNoSupportedFindings ||
		releasing.ReleaseRequestedBy == nil || *releasing.ReleaseRequestedBy != 41 || releasing.ReleaseRequestedAt == nil ||
		releasing.ReleaseSourceSnapshotHash != current.LatestSourceSnapshotHash {
		t.Fatalf("audited release = %#v", releasing)
	}
	var stored models.ProtectionEnrollment
	if err := db.Where("tenant_id = ? AND id = ?", 7, current.ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ReleaseBasis != models.ReleaseBasisNoSupportedFindings || stored.ReleaseRequestedBy == nil || *stored.ReleaseRequestedBy != 41 ||
		stored.ReleaseRequestedAt == nil || stored.ReleaseSourceSnapshotHash != current.LatestSourceSnapshotHash {
		t.Fatalf("stored release audit = %#v", stored)
	}
}

func TestEnrollmentListSeparatesCurrentAndReleasedHistory(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := NewEnrollmentService(db)
	current, err := svc.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeCollection, "Outdoor.Persons"))
	if err != nil {
		t.Fatal(err)
	}
	released, err := svc.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(2, resourcetree.TypeTable, "outdoor.ods_outdoor_persons"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ProtectionEnrollment{}).Where("tenant_id = ? AND id = ?", 7, released.ID).Update("state", models.EnrollmentStateReleased).Error; err != nil {
		t.Fatal(err)
	}

	currentList, err := svc.List(context.Background(), 7, models.EnrollmentListScopeCurrent, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if currentList.Total != 1 || len(currentList.Data) != 1 || currentList.Data[0].ID != current.ID {
		t.Fatalf("current list = %#v", currentList)
	}

	releasedList, err := svc.List(context.Background(), 7, models.EnrollmentListScopeReleased, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if releasedList.Total != 1 || len(releasedList.Data) != 1 || releasedList.Data[0].ID != released.ID {
		t.Fatalf("released list = %#v", releasedList)
	}

	allList, err := svc.List(context.Background(), 7, models.EnrollmentListScopeAll, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if allList.Total != 2 || len(allList.Data) != 2 {
		t.Fatalf("all list = %#v", allList)
	}

	if _, err := svc.List(context.Background(), 7, "unknown", 1, 20); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid scope error = %v, want bad request", err)
	}
}

func TestNoSupportedFindingsReleaseRejectsStaleOrUnsupportedEvidence(t *testing.T) {
	_, zeroService, zeroCurrent := prepareZeroFindingEnrollment(t)
	if _, err := zeroService.CreateDiscoveryExecution(context.Background(), 7, 41, zeroCurrent.ID, models.CreateProtectionDiscoveryExecutionRequest{Version: zeroCurrent.Version}); err != nil {
		t.Fatal(err)
	}
	withPending, err := zeroService.Get(context.Background(), 7, zeroCurrent.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = zeroService.Release(context.Background(), 7, 41, withPending.ID, models.ReleaseProtectionEnrollmentRequest{
		Version: withPending.Version, Basis: models.ReleaseBasisNoSupportedFindings, Reason: "当前无需保护",
	})
	if !errors.Is(err, ErrNoSupportedFindingsReleaseUnavailable) {
		t.Fatalf("pending discovery release error = %v", err)
	}

	_, findingService, finding, _, _ := prepareReviewablePhoneFinding(t)
	findingCurrent, err := findingService.Get(context.Background(), 7, finding.EnrollmentID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = findingService.Release(context.Background(), 7, 41, findingCurrent.ID, models.ReleaseProtectionEnrollmentRequest{
		Version: findingCurrent.Version, Basis: models.ReleaseBasisNoSupportedFindings, Reason: "当前无需保护",
	})
	if !errors.Is(err, ErrNoSupportedFindingsReleaseUnavailable) {
		t.Fatalf("nonzero finding release error = %v", err)
	}
}

func TestEnrollmentRejectsInvalidLocatorAndForgedCursor(t *testing.T) {
	svc := NewEnrollmentService(openSecurityTestDB(t))
	_, err := svc.Create(context.Background(), 7, 11, models.CreateProtectionEnrollmentRequest{Locator: "not-a-locator"})
	if !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("create error = %v, want bad request", err)
	}
	if err := svc.Acknowledge(context.Background(), 7, "manager", "forged"); !errors.Is(err, ErrProjectionCursorConflict) {
		t.Fatalf("ack error = %v, want cursor conflict", err)
	}
	if _, err := svc.ListChanges(context.Background(), 7, "manager", "forged", 20); !errors.Is(err, ErrProjectionCursorConflict) {
		t.Fatalf("changes error = %v, want cursor conflict", err)
	}
}

func prepareZeroFindingEnrollment(t *testing.T) (*gorm.DB, *EnrollmentService, *models.ProtectionEnrollmentResponse) {
	t.Helper()
	db := openSecurityTestDB(t)
	enrollments := NewEnrollmentService(db)
	created, err := enrollments.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeTable, "outdoor.ods_outdoor_persons"))
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
		{Name: "person_id", Path: []string{"person_id"}, Type: datatype.FieldTypeString, Nullable: false},
		{Name: "nickname", Path: []string{"nickname"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	hash, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	facts := &dataprotection.DataItemSecurityFacts{
		SchemaVersion:   dataprotection.DataItemSecurityFactsSchemaV1,
		ItemFingerprint: created.Target.ResourceIdentity,
		ItemType:        "table", Fields: fields, SourceSnapshotHash: hash, ObservedAt: time.Now().UTC(),
	}
	discoveries := NewDiscoveryService(db, func(uint) SecurityFactsReader { return staticSecurityFactsReader{facts: facts} })
	execution, lease, err := discoveries.ClaimNext(context.Background(), "security-zero-finding-test", time.Now().UTC(), time.Minute)
	if err != nil || execution == nil || lease == nil {
		t.Fatalf("claim = %#v/%#v, err=%v", execution, lease, err)
	}
	if err := discoveries.Execute(context.Background(), execution, *lease); err != nil {
		t.Fatal(err)
	}
	current, err := enrollments.Get(context.Background(), 7, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	return db, enrollments, current
}
