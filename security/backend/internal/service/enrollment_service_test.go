package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	commonexecution "github.com/addp/common/execution"
	"github.com/addp/common/resourcetree"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"gorm.io/gorm"
)

func TestGraphEnrollmentUsesCanonicalCatalogIdentity(t *testing.T) {
	assertGraphEnrollmentUsesCanonicalCatalogIdentity(t, openSecurityTestDB(t))
}

func assertGraphEnrollmentUsesCanonicalCatalogIdentity(t *testing.T, db *gorm.DB) {
	t.Helper()
	for index, test := range []struct {
		path     []string
		fullName string
	}{
		{[]string{"neo4j", "graph"}, "neo4j.graph"},
		{[]string{"tenant.alpha/2026"}, "tenant.alpha/2026"},
	} {
		engineID, itemID := uint(741+index), uint(578+index)
		locator := &resourcetree.ResourceLocator{EngineID: engineID, Path: test.path, Type: resourcetree.TypeGraph, ItemID: &itemID}
		created, err := NewEnrollmentService(db).Create(context.Background(), 7, 11, models.CreateProtectionEnrollmentRequest{Locator: locator.ToURI()})
		if err != nil {
			t.Fatal(err)
		}
		if created.TargetSnapshot.FullName != test.fullName || created.Target.ResourceIdentity != testDataItemFingerprint(engineID, test.fullName) {
			t.Fatalf("Graph enrollment identity must match Meta: target=%#v snapshot=%#v expected=%s", created.Target, created.TargetSnapshot, test.fullName)
		}
	}
}

func TestEnrollmentSearchAndProjectionValidity(t *testing.T) {
	assertEnrollmentSearchAndProjectionValidity(t, openSecurityTestDB(t))
}

func assertEnrollmentSearchAndProjectionValidity(t *testing.T, db *gorm.DB) {
	t.Helper()
	svc := NewEnrollmentService(db)
	start := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	now := start
	svc.now = func() time.Time { return now }
	var target models.ProtectionEnrollmentResponse
	for index, item := range []struct {
		tenant int64
		engine uint
		name   string
	}{
		{7, 73, "test.Customer_%!"}, {7, 73, "test.Customer_A"}, {7, 74, "test.Customer_%!"}, {8, 73, "test.Customer_%!"},
	} {
		request := testDataItemEnrollmentRequest(item.engine, resourcetree.TypeTable, item.name)
		request.ValidityMode = "until"
		request.ExpiresAt = dataprotection.ProjectionDeadline(start.Add(24 * time.Hour))
		created, err := svc.Create(context.Background(), item.tenant, 11, request)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			target = *created
		}
	}
	filter := models.ProtectionEnrollmentFilter{ResourceName: " CUSTOMER_ ", EngineID: 73, State: models.EnrollmentStateActivating}
	page, err := svc.List(context.Background(), 7, models.EnrollmentListScopeCurrent, 1, 1, filter)
	if err != nil || page.Total != 2 || len(page.Data) != 1 || page.TotalPages != 2 {
		t.Fatalf("filtered pagination = %#v, error = %v", page, err)
	}
	filter.ResourceName = "%!"
	page, err = svc.List(context.Background(), 7, models.EnrollmentListScopeAll, 1, 20, filter)
	if err != nil || page.Total != 1 || page.Data[0].ID != target.ID {
		t.Fatalf("literal filter and isolation = %#v, error = %v", page, err)
	}
	filter.State = models.EnrollmentStateReleased
	page, err = svc.List(context.Background(), 7, models.EnrollmentListScopeCurrent, 1, 20, filter)
	if err != nil || page.Total != 0 {
		t.Fatalf("scope intersection = %#v, error = %v", page, err)
	}
	filter.State = "invented"
	if _, err := svc.List(context.Background(), 7, models.EnrollmentListScopeCurrent, 1, 20, filter); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid state = %v", err)
	}
	for _, owner := range allRequiredProtectionOwners() {
		changes, err := svc.ListChanges(context.Background(), 7, owner, "", 200)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.Acknowledge(context.Background(), 7, owner, changes.NextCursor); err != nil {
			t.Fatal(err)
		}
	}
	for _, point := range []struct {
		at       time.Time
		validity string
	}{{start.Add(-time.Second), "not_yet_valid"}, {start, "valid"}, {start.Add(24 * time.Hour), "expired"}} {
		now = point.at
		response, err := svc.Get(context.Background(), 7, target.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, owner := range response.OwnerProgress {
			if !owner.ValidFrom.Equal(start) || !owner.ExpiresAt.Equal(start.Add(24*time.Hour)) || owner.Validity != point.validity || !owner.Acknowledged {
				t.Fatalf("validity must not change acknowledgement: %#v", owner)
			}
		}
	}
	if err := db.Model(&models.ProtectionEnrollment{}).Where("id = ?", target.ID).Update("state", models.EnrollmentStateReleased).Error; err != nil {
		t.Fatal(err)
	}
	response, err := svc.Get(context.Background(), 7, target.ID)
	if err != nil || response.OwnerProgress[0].Validity != "released" {
		t.Fatalf("released validity = %#v, error = %v", response, err)
	}
}

func TestEnrollmentRequiresEveryOwnerAcknowledgementBeforeEnrolling(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := NewEnrollmentService(db)
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	created, err := svc.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeCollection, "Outdoor.Persons"))
	if err != nil {
		t.Fatal(err)
	}
	if created.State != models.EnrollmentStateActivating || len(created.OwnerProgress) != len(requiredProtectionOwnerContracts) {
		t.Fatalf("created state/progress = %s/%d", created.State, len(created.OwnerProgress))
	}
	if created.TargetSnapshot.EngineID != 11 || created.TargetSnapshot.ItemType != "collection" || created.TargetSnapshot.FullName != "Outdoor.Persons" {
		t.Fatalf("target snapshot = %#v", created.TargetSnapshot)
	}
	for _, progress := range created.OwnerProgress {
		if progress.ProjectionState != dataprotection.ProjectionStateEnrolling || len(progress.Rules) != 0 {
			t.Fatalf("initial owner progress = %#v", progress)
		}
	}

	for index, owner := range allRequiredProtectionOwners() {
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
		if index == len(requiredProtectionOwnerContracts)-1 {
			want = models.EnrollmentStateEnrolling
		}
		if current.State != want {
			t.Fatalf("after owner %s state = %s, want %s", owner, current.State, want)
		}
	}
	current, err := svc.Get(context.Background(), 7, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range allRequiredProtectionOwners() {
		changes, err := svc.ListChanges(context.Background(), 7, owner, "", 20)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.Acknowledge(context.Background(), 7, owner, changes.NextCursor); err != nil {
			t.Fatal(err)
		}
	}
	after, err := svc.Get(context.Background(), 7, created.ID)
	if err != nil || after.State != models.EnrollmentStateEnrolling || after.Version != current.Version {
		t.Fatalf("gate-only acknowledgements must not activate: enrollment=%#v error=%v", after, err)
	}
}

func TestEnrollmentRequiresLatestEffectiveProjectionAcknowledgementsBeforeActive(t *testing.T) {
	assertEnrollmentEffectiveProjectionActivation(t, openSecurityTestDB(t))
}

func assertEnrollmentEffectiveProjectionActivation(t *testing.T, db *gorm.DB) {
	t.Helper()
	_, svc, finding, _, _ := prepareReviewablePhoneFindingOnDB(t, db)
	ctx := context.Background()
	initial, err := svc.Get(ctx, 7, finding.EnrollmentID)
	if err != nil || initial.State != models.EnrollmentStateEnrolling {
		t.Fatalf("before effective acknowledgements: enrollment=%#v error=%v", initial, err)
	}
	owners := allRequiredProtectionOwners()
	acknowledge := func(owner string) {
		t.Helper()
		changes, err := svc.ListChanges(ctx, 7, owner, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.Acknowledge(ctx, 7, owner, changes.NextCursor); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range owners[:len(owners)-1] {
		acknowledge(owner)
	}
	// Replace the effective revision after three owners have acknowledged it.
	if _, err := NewAssessmentService(db, nil).ReviewFinding(ctx, 7, 21, finding.ID, models.FindingReviewRequest{
		Decision: models.FindingReviewDecisionConfirm, Rationale: "确认有效投影激活屏障",
	}); err != nil {
		t.Fatal(err)
	}
	acknowledge(owners[len(owners)-1])
	current, err := svc.Get(ctx, 7, initial.ID)
	if err != nil || current.State != models.EnrollmentStateEnrolling || current.Version != initial.Version {
		t.Fatalf("stale acknowledgements must not activate: enrollment=%#v error=%v", current, err)
	}
	for index, owner := range owners[:len(owners)-1] {
		acknowledge(owner)
		current, err = svc.Get(ctx, 7, initial.ID)
		wantState := models.EnrollmentStateEnrolling
		wantVersion := initial.Version
		if index == len(owners)-2 {
			wantState, wantVersion = models.EnrollmentStateActive, initial.Version+1
		}
		if err != nil || current.State != wantState || current.Version != wantVersion {
			t.Fatalf("after %s effective acknowledgement: state/version=%s/%d want=%s/%d error=%v", owner, current.State, current.Version, wantState, wantVersion, err)
		}
	}
	for _, owner := range owners {
		acknowledge(owner)
	}
	current, err = svc.Get(ctx, 7, initial.ID)
	if err != nil || current.Version != initial.Version+1 {
		t.Fatalf("repeated acknowledgements changed version: enrollment=%#v error=%v", current, err)
	}
	var discoveries int64
	if err := db.Model(&commonexecution.TaskExecution{}).Where("tenant_id = ? AND module = ? AND source_task_id = ?", 7, commonexecution.ModuleSecurity, initial.ID).Count(&discoveries).Error; err != nil || discoveries != 1 {
		t.Fatalf("effective activation must not enqueue discovery again: count=%d error=%v", discoveries, err)
	}
}

func TestProjectionRuleSummariesExposeInstalledActionEffects(t *testing.T) {
	record := models.ProtectionProjectionRecord{
		ID:    "projection-1",
		State: dataprotection.ProjectionStateActive,
		ProjectionPayload: `{
 "expires_at":null,
			"rules": [
				{"action":"preview","decision":{"effect":"mask"}},
				{"action":"profile","decision":{"effect":"suppress"}},
				{"action":"preview","decision":{"effect":"mask"}}
			]
		}`,
	}

	var projection dataprotection.Projection
	if err := json.Unmarshal([]byte(record.ProjectionPayload), &projection); err != nil {
		t.Fatal(err)
	}
	rules := projectionRuleSummaries(projection, time.Now().UTC())
	want := []models.ProtectionOwnerRuleSummary{
		{Action: "preview", Effect: dataprotection.EffectMask},
		{Action: "profile", Effect: dataprotection.EffectSuppress},
	}
	if len(rules) != len(want) {
		t.Fatalf("rule summaries = %#v, want %#v", rules, want)
	}
	for index := range want {
		if rules[index] != want[index] {
			t.Fatalf("rule summaries = %#v, want %#v", rules, want)
		}
	}
}

func TestExplicitRediscoveryIsUniqueAndRenewsLatestSchemaProjection(t *testing.T) {
	for _, changedSchema := range []bool{false, true} {
		name := "unchanged schema"
		if changedSchema {
			name = "changed schema"
		}
		t.Run(name, func(t *testing.T) { assertExplicitRediscoveryRenewal(t, changedSchema) })
	}
}

func assertExplicitRediscoveryRenewal(t *testing.T, changedSchema bool) {
	t.Helper()
	db, enrollments, finding, _, _ := prepareReviewablePhoneFinding(t)
	assessments := NewAssessmentService(db, nil)
	if _, err := assessments.ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段"}); err != nil {
		t.Fatal(err)
	}
	current, err := enrollments.Get(context.Background(), 7, finding.EnrollmentID)
	if err != nil {
		t.Fatal(err)
	}
	previous := make(map[string]dataprotection.Projection)
	for _, owner := range allRequiredProtectionOwners() {
		changes, err := enrollments.ListChanges(context.Background(), 7, owner, "", 20)
		if err != nil || len(changes.Changes) == 0 {
			t.Fatalf("previous %s projection changes: %#v, %v", owner, changes, err)
		}
		last := changes.Changes[len(changes.Changes)-1].Projection
		if last == nil || last.State != dataprotection.ProjectionStateActive {
			t.Fatalf("previous %s active projection = %#v", owner, last)
		}
		previous[owner] = *last
	}
	fields := []datatype.FieldInfo{
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
		{Name: "userInfo.nickName", Path: []string{"userInfo", "nickName"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	if !changedSchema {
		fields = fields[:2]
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
	if created.Status != "pending" || created.EnrollmentID != current.ID || created.EnrollmentVersion != current.Version+1 {
		t.Fatalf("rediscovery = %#v", created)
	}
	afterCreate, err := enrollments.Get(context.Background(), 7, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enrollments.CreateDiscoveryExecution(context.Background(), 7, 41, current.ID, models.CreateProtectionDiscoveryExecutionRequest{Version: current.Version}); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("stale rediscovery error = %v", err)
	}
	if _, err := enrollments.CreateDiscoveryExecution(context.Background(), 7, 41, current.ID, models.CreateProtectionDiscoveryExecutionRequest{Version: afterCreate.Version}); !errors.Is(err, ErrDiscoveryExecutionInProgress) {
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
	for _, owner := range allRequiredProtectionOwners() {
		changes, err := enrollments.ListChanges(context.Background(), 7, owner, "", 20)
		if err != nil || len(changes.Changes) == 0 {
			t.Fatalf("renewed %s projection changes: %#v, %v", owner, changes, err)
		}
		last := changes.Changes[len(changes.Changes)-1].Projection
		old := previous[owner]
		if last == nil || last.SourceSnapshotHash != newHash || last.ProjectionID != old.ProjectionID || last.Revision <= old.Revision {
			t.Fatalf("renewed %s projection = %#v", owner, last)
		}
		if last.ExpiresAt != nil || old.ExpiresAt != nil || !last.ValidFrom.After(old.ValidFrom) {
			t.Fatalf("renewed %s validity = %s to %s; previous = %s to %s", owner, last.ValidFrom, last.ExpiresAt, old.ValidFrom, old.ExpiresAt)
		}
		if err := last.Validate(time.Now().UTC()); err != nil {
			t.Fatalf("renewed %s projection validation: %v", owner, err)
		}
		if owner == managerProtectionOwner {
			requireManagerProjectionEffects(t, last, dataprotection.EffectMask)
		}
	}
}

func TestEnrollmentReleaseWaitsForEveryOwnerAndRemovesFeedProjection(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := NewEnrollmentService(db)
	created, err := svc.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeCollection, "Outdoor.Persons"))
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range allRequiredProtectionOwners() {
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
	for index, owner := range allRequiredProtectionOwners() {
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
		if index == len(requiredProtectionOwnerContracts)-1 {
			want = models.EnrollmentStateReleased
		}
		if current.State != want {
			t.Fatalf("after release owner %s state = %s, want %s", owner, current.State, want)
		}
	}
}

func TestReleasedEnrollmentCanCreateANewProtectionLifecycle(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := NewEnrollmentService(db)
	created, err := svc.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeCollection, "Outdoor.Persons"))
	if err != nil {
		t.Fatal(err)
	}
	releasedAt := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	releaseRequestedAt := releasedAt.Add(-5 * time.Minute)
	if err := db.Model(&models.ProtectionEnrollment{}).Where("tenant_id = ? AND id = ?", 7, created.ID).Updates(map[string]any{
		"state": models.EnrollmentStateReleased, "release_basis": models.ReleaseBasisManual,
		"release_reason": "test release", "release_requested_by": 17,
		"release_requested_at": releaseRequestedAt, "released_at": releasedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	source, err := svc.Get(context.Background(), 7, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReEnroll(context.Background(), 7, 21, source.ID, models.ReEnrollProtectionEnrollmentRequest{Version: source.Version + 1}); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("stale re-enrollment error = %v", err)
	}

	result, err := svc.ReEnroll(context.Background(), 7, 21, source.ID, models.ReEnrollProtectionEnrollmentRequest{Version: source.Version})
	if err != nil {
		t.Fatal(err)
	}
	reenrolled := result.Enrollment
	if reenrolled.ID == source.ID || reenrolled.State != models.EnrollmentStateActivating || reenrolled.CreatedBy != 21 || len(reenrolled.OwnerProgress) != len(requiredProtectionOwnerContracts) {
		t.Fatalf("new enrollment = %#v", reenrolled)
	}
	if result.SourceEnrollmentVersion != source.Version+1 {
		t.Fatalf("source enrollment version = %d", result.SourceEnrollmentVersion)
	}
	if reenrolled.Target != source.Target || reenrolled.TargetSnapshot != source.TargetSnapshot {
		t.Fatalf("re-enrolled target changed: source=%#v new=%#v", source, reenrolled)
	}
	unchanged, err := svc.Get(context.Background(), 7, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.State != models.EnrollmentStateReleased || unchanged.Version != source.Version+1 || unchanged.ReleaseBasis != models.ReleaseBasisManual || unchanged.ReleaseReason != "test release" || unchanged.ReleaseRequestedBy == nil || *unchanged.ReleaseRequestedBy != 17 || unchanged.ReleaseRequestedAt == nil || !unchanged.ReleaseRequestedAt.Equal(releaseRequestedAt) || unchanged.ReleasedAt == nil || !unchanged.ReleasedAt.Equal(releasedAt) {
		t.Fatalf("released audit changed = %#v", unchanged)
	}
	if _, err := svc.ReEnroll(context.Background(), 7, 21, source.ID, models.ReEnrollProtectionEnrollmentRequest{Version: source.Version}); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("stale duplicate re-enrollment error = %v", err)
	}
	if _, err := svc.ReEnroll(context.Background(), 7, 21, source.ID, models.ReEnrollProtectionEnrollmentRequest{Version: unchanged.Version}); !errors.Is(err, ErrLiveEnrollmentAlreadyExists) {
		t.Fatalf("duplicate live re-enrollment error = %v", err)
	}
}

func TestZeroFindingDiscoverySummaryAndAuditedRelease(t *testing.T) {
	db, svc, current := prepareZeroFindingEnrollment(t)
	if current.DiscoverySummary.Status != models.DiscoverySummaryStatusCompleted || current.DiscoverySummary.FindingCount != 0 {
		t.Fatalf("discovery summary = %#v", current.DiscoverySummary)
	}
	listed, err := svc.List(context.Background(), 7, models.EnrollmentListScopeCurrent, 1, 20, models.ProtectionEnrollmentFilter{})
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
	earlierCreatedLaterReleased, err := svc.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(3, resourcetree.TypeTable, "outdoor.ods_outdoor_activities"))
	if err != nil {
		t.Fatal(err)
	}
	createdLater := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	createdEarlier := createdLater.Add(-time.Hour)
	releasedEarlier := createdLater.Add(time.Hour)
	releasedLater := releasedEarlier.Add(time.Hour)
	if err := db.Model(&models.ProtectionEnrollment{}).Where("tenant_id = ? AND id = ?", 7, released.ID).Updates(map[string]any{
		"state": models.EnrollmentStateReleased, "created_at": createdLater, "released_at": releasedEarlier,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ProtectionEnrollment{}).Where("tenant_id = ? AND id = ?", 7, earlierCreatedLaterReleased.ID).Updates(map[string]any{
		"state": models.EnrollmentStateReleased, "created_at": createdEarlier, "released_at": releasedLater,
	}).Error; err != nil {
		t.Fatal(err)
	}

	currentList, err := svc.List(context.Background(), 7, models.EnrollmentListScopeCurrent, 1, 20, models.ProtectionEnrollmentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if currentList.Total != 1 || len(currentList.Data) != 1 || currentList.Data[0].ID != current.ID {
		t.Fatalf("current list = %#v", currentList)
	}

	releasedList, err := svc.List(context.Background(), 7, models.EnrollmentListScopeReleased, 1, 20, models.ProtectionEnrollmentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if releasedList.Total != 2 || len(releasedList.Data) != 2 || releasedList.Data[0].ID != earlierCreatedLaterReleased.ID || releasedList.Data[1].ID != released.ID {
		t.Fatalf("released list = %#v", releasedList)
	}

	allList, err := svc.List(context.Background(), 7, models.EnrollmentListScopeAll, 1, 20, models.ProtectionEnrollmentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if allList.Total != 3 || len(allList.Data) != 3 {
		t.Fatalf("all list = %#v", allList)
	}

	if _, err := svc.List(context.Background(), 7, "unknown", 1, 20, models.ProtectionEnrollmentFilter{}); !errors.Is(err, commonapi.ErrBadRequest) {
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
	for _, owner := range allRequiredProtectionOwners() {
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

func TestResourceProtectionValidity(t *testing.T) {
	assertResourceProtectionValidity(t, openSecurityTestDB(t))
}

func assertResourceProtectionValidity(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	svc := NewEnrollmentService(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	svc.now = func() time.Time { return now }
	row, err := svc.Create(ctx, 7, 11, testDataItemEnrollmentRequest(97, resourcetree.TypeTable, "validity.accounts"))
	if err != nil {
		t.Fatal(err)
	}
	if row.ExpiresAt != nil || row.ValidityMode != "permanent" {
		t.Fatal("new resource must be permanent")
	}
	for _, owner := range row.OwnerProgress {
		if owner.ExpiresAt != nil || owner.Validity != "valid" {
			t.Fatal("initial deny must use resource validity")
		}
	}
	deadline := now.Add(3 * time.Hour)
	updated, err := svc.UpdateValidity(ctx, 7, 12, row.ID, models.UpdateProtectionValidityRequest{Version: row.Version, ValidityMode: "until", ExpiresAt: &deadline})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != row.Version+1 || updated.ExpiresAt == nil || !updated.ExpiresAt.Equal(deadline) || updated.ValidityUpdatedBy == nil || *updated.ValidityUpdatedBy != 12 {
		t.Fatal("validity update omitted version, deadline or actor")
	}
	var count int64
	db.Model(&models.ProtectionProjectionChange{}).Where("enrollment_id = ?", row.ID).Count(&count)
	if count != int64(2*len(requiredProtectionOwnerContracts)) {
		t.Fatalf("want initial and revised projections for every required owner; got %d", count)
	}
	for i, request := range []models.UpdateProtectionValidityRequest{
		{Version: row.Version, ValidityMode: "permanent"},
		{Version: updated.Version, ValidityMode: "unknown"},
		{Version: updated.Version, ValidityMode: "permanent", ExpiresAt: &deadline},
		{Version: updated.Version, ValidityMode: "until"},
		{Version: updated.Version, ValidityMode: "until", ExpiresAt: &now},
	} {
		_, err := svc.UpdateValidity(ctx, 7, 12, row.ID, request)
		if err == nil {
			t.Fatalf("invalid request %d accepted", i)
		}
		if i == 0 && !errors.Is(err, repository.ErrVersionConflict) {
			t.Fatal(err)
		}
	}
	if _, err := svc.UpdateValidity(ctx, 8, 12, row.ID, models.UpdateProtectionValidityRequest{Version: updated.Version, ValidityMode: "permanent"}); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("cross tenant: %v", err)
	}
	var afterCount int64
	db.Model(&models.ProtectionProjectionChange{}).Where("enrollment_id = ?", row.ID).Count(&afterCount)
	if afterCount != count {
		t.Fatal("rejected writes changed projections")
	}
	now = deadline
	expired, err := svc.Get(ctx, 7, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range expired.OwnerProgress {
		if owner.Validity != "expired" || owner.ExpiringSoon {
			t.Fatal("expiry must be authoritative")
		}
	}
	restored, err := svc.UpdateValidity(ctx, 7, 12, row.ID, models.UpdateProtectionValidityRequest{Version: updated.Version, ValidityMode: "permanent"})
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range restored.OwnerProgress {
		if owner.ExpiresAt != nil || owner.Validity != "valid" || owner.Revision <= "00000000000000000002" {
			t.Fatal("permanent restoration must publish higher revisions")
		}
	}
	// A later Owner failure rolls back the deadline, actor, version and all earlier publications.
	var record models.ProtectionProjectionRecord
	if err := db.Where("enrollment_id = ? AND consumer_owner = ?", row.ID, "transfer").First(&record).Error; err != nil {
		t.Fatal(err)
	}
	payload := record.ProjectionPayload
	if err := db.Model(&record).Update("projection_payload", "{}").Error; err != nil {
		t.Fatal(err)
	}
	var beforeChanges int64
	db.Model(&models.ProtectionProjectionChange{}).Where("enrollment_id = ?", row.ID).Count(&beforeChanges)
	future := now.Add(time.Hour)
	if _, err := svc.UpdateValidity(ctx, 7, 12, row.ID, models.UpdateProtectionValidityRequest{Version: restored.Version, ValidityMode: "until", ExpiresAt: &future}); err == nil {
		t.Fatal("invalid last Owner accepted")
	}
	var unchanged models.ProtectionEnrollment
	db.First(&unchanged, "id = ?", row.ID)
	db.Model(&models.ProtectionProjectionChange{}).Where("enrollment_id = ?", row.ID).Count(&afterCount)
	if unchanged.Version != restored.Version || unchanged.ProtectionExpiresAt != nil || afterCount != beforeChanges {
		t.Fatal("partial validity publication escaped the transaction")
	}
	if err := db.Model(&record).Update("projection_payload", payload).Error; err != nil {
		t.Fatal(err)
	}

}

func TestActiveValidityUpdatePreservesRulesAndBoundsPlaintext(t *testing.T) {
	db, svc, finding, _, _ := prepareReviewablePhoneFinding(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	svc.now = func() time.Time { return now }
	if _, err := NewAssessmentService(db, nil).ReviewFinding(ctx, 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认有效期测试"}); err != nil {
		t.Fatal(err)
	}
	row, err := svc.Get(ctx, 7, finding.EnrollmentID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := now.Add(20 * time.Minute)
	row, err = svc.UpdateValidity(ctx, 7, 21, row.ID, models.UpdateProtectionValidityRequest{Version: row.Version, ValidityMode: "until", ExpiresAt: &deadline})
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range row.OwnerProgress {
		if owner.ProjectionState != "active" || owner.ExpiresAt == nil || !owner.ExpiresAt.Equal(deadline) {
			t.Fatal("active rules lost during validity change")
		}
	}

	// An older control-plane caller must not republish its formerly permanent deadline.
	var stale models.ProtectionEnrollment
	if err := db.First(&stale, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	stale.ProtectionExpiresAt = nil
	if err := db.Transaction(func(tx *gorm.DB) error {
		return compileProtectionProjections(tx, stale, stale.LatestSourceSnapshotHash, now, allRequiredProtectionOwners())
	}); err != nil {
		t.Fatal(err)
	}
	current, err := svc.Get(ctx, 7, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range current.OwnerProgress {
		if owner.ExpiresAt == nil || !owner.ExpiresAt.Equal(deadline) {
			t.Fatal("stale compiler replaced the configured deadline")
		}
	}
	requests := NewAccessRequestService(db, testAccessActorResolver)
	requests.now = func() time.Time { return now }
	var assessment models.ResourceSecurityAssessment
	if err := db.Where("enrollment_id = ?", row.ID).First(&assessment).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := requests.Create(ctx, 7, 41, models.CreateProtectionAccessRequest{Scopes: []models.ProtectionAccessScope{{ConsumerOwner: managerProtectionOwner, Action: managerPreviewAction}}, AssessmentID: assessment.ID, RequestedExpiresAt: deadline.Add(time.Minute), Rationale: "期限之外"}); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("plaintext exceeded protection deadline: %v", err)
	}

	grantDeadline := now.Add(15 * time.Minute)
	application, err := requests.Create(ctx, 7, 41, models.CreateProtectionAccessRequest{Scopes: []models.ProtectionAccessScope{{ConsumerOwner: managerProtectionOwner, Action: managerPreviewAction}}, AssessmentID: assessment.ID, RequestedExpiresAt: grantDeadline, Rationale: "期限内核验"})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := requests.Decide(ctx, 7, 42, application.ID, models.DecideProtectionAccessRequest{Version: application.Version, Decision: "approve", ExpiresAt: grantDeadline, Rationale: "独立复核"})
	if err != nil {
		t.Fatal(err)
	}
	_ = approved
	row, err = svc.Get(ctx, 7, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline = now.Add(10 * time.Minute)
	row, err = svc.UpdateValidity(ctx, 7, 21, row.ID, models.UpdateProtectionValidityRequest{Version: row.Version, ValidityMode: "until", ExpiresAt: &deadline})
	if err != nil {
		t.Fatal(err)
	}
	targets, err := requests.Targets(ctx, 7, 41, row.Target.ResourceIdentity, managerProtectionOwner, managerPreviewAction)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets.Data) != 1 || targets.Data[0].AuthorizedUntil == nil || !targets.Data[0].AuthorizedUntil.Equal(deadline) || targets.Data[0].ProtectionExpiresAt == nil || !targets.Data[0].ProtectionExpiresAt.Equal(deadline) {
		t.Fatal("plaintext target exceeded the effective resource deadline")
	}
	var grant models.ProtectionExemptionRevision
	if err := db.Where("source_request_id = ?", application.ID).First(&grant).Error; err != nil {
		t.Fatal(err)
	}
	if !grant.ExpiresAt.Equal(grantDeadline) {
		t.Fatal("resource deadline rewrote grant history")
	}
	changes, err := svc.ListChanges(ctx, 7, managerProtectionOwner, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	installed := changes.Changes[len(changes.Changes)-1].Projection
	if len(installed.Rules[0].Authorizations) != 1 || !installed.Rules[0].Authorizations[0].ValidUntil.Equal(deadline) {
		t.Fatal("compiled plaintext grant exceeds the resource deadline")
	}
	// Rediscovery or recompilation must retain the explicit deadline, even after expiry.
	now = deadline.Add(time.Second)
	var stored models.ProtectionEnrollment
	db.First(&stored, "id = ?", row.ID)
	if err := db.Transaction(func(tx *gorm.DB) error {
		return compileProtectionProjections(tx, stored, stored.LatestSourceSnapshotHash, now, allRequiredProtectionOwners())
	}); err != nil {
		t.Fatal(err)
	}
	expired, err := svc.Get(ctx, 7, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range expired.OwnerProgress {
		if owner.Validity != "expired" || owner.ProjectionState != "enrolling" || !owner.ExpiresAt.Equal(deadline) {
			t.Fatal("recompile renewed or relaxed expired protection")
		}
	}
	restored, err := svc.UpdateValidity(ctx, 7, 21, row.ID, models.UpdateProtectionValidityRequest{Version: row.Version, ValidityMode: "permanent"})
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range restored.OwnerProgress {
		if owner.ProjectionState != "active" || owner.ExpiresAt != nil {
			t.Fatal("explicit permanent update did not restore current rules")
		}
	}
}
