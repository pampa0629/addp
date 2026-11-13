package service

import (
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/cleanup"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Compose the real public scan lifecycle/result transaction using the same
// candidate discovery and impact algorithm as Graph's scan Runtime. This is a
// protocol fixture, not a System IAM/acceptance or native engine substitute.
func graphCleanupScanFixture(t *testing.T, db *gorm.DB, template events.CleanupRequest) (string, string) {
	t.Helper()
	now, err := execution.DatabaseNow(db)
	if err != nil {
		t.Fatal(err)
	}
	scan := template
	scan.RequestID, scan.ParentExecutionID = uuid.NewString(), uuid.NewString()
	scan.Action, scan.CleanupMode, scan.BasedOnScan = events.CleanupActionScan, "", ""
	scan.SourceOperationID = ""
	scan.Confirmation = events.CleanupConfirmation{}
	scan.Context.ExternalArtifactPolicy = ""
	scan.RequestedAt, scan.Deadline = now, now.Add(time.Minute)
	if err := scan.Seal(); err != nil {
		t.Fatal(err)
	}
	ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext(strconv.Itoa(int(scan.TenantID)), strconv.Itoa(int(scan.RequestedBy)), nil))
	parent := execution.TaskExecution{ExecutionID: scan.ParentExecutionID, TenantID: int(scan.TenantID), Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: scan.TriggerType, ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}}
	if err := execution.ApplyUserOrigin(ctx, &parent); err != nil {
		t.Fatal(err)
	}
	children, err := execution.CreateCleanupExecutions(ctx, db, &parent, scan)
	if err != nil {
		t.Fatal(err)
	}
	identity := func(e execution.TaskExecution) execution.PendingIdentity {
		return execution.PendingIdentity{ExecutionID: e.ExecutionID, TenantID: e.TenantID, Module: e.Module, TaskType: e.TaskType, Source: e.Source}
	}
	options := func(e execution.TaskExecution) execution.ClaimOptions {
		return execution.ClaimOptions{Module: e.Module, TaskType: e.TaskType, Source: e.Source, WorkerID: "scan-baseline-fixture", LeaseDuration: time.Minute}
	}
	_, pl, err := execution.ClaimPending(ctx, db, identity(parent), options(parent), nil)
	if err != nil {
		t.Fatal(err)
	}
	child, cl, err := execution.ClaimCleanupChild(ctx, db, identity(children[0]), options(children[0]), nil)
	if err != nil || cl == nil {
		t.Fatalf("scan child claim: %v", err)
	}
	candidates, err := (&CleanupService{db: db}).listCandidates(ctx, scan.TenantID, scan.Context.Map())
	if err != nil {
		t.Fatal(err)
	}
	impact, err := graphEngineDeletionImpact(candidates)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := execution.NewCleanupReceipt(*child, events.CleanupResultData{TaskID: scan.RequestID, TenantID: scan.TenantID, Module: "graph", Action: "scan", Status: "success", Summary: graphScanSummary(candidates.stats()), Impact: &impact}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup.Complete(ctx, db, *cl, receipt, graphCleanupCounters(candidates.stats())); err != nil {
		t.Fatal(err)
	}
	if err := execution.CompleteWithLease(ctx, db, *pl, "success", nil); err != nil {
		t.Fatal(err)
	}
	aggregate, err := events.BuildCleanupImpactData([]events.CleanupImpactItem{{StableRef: "graph:" + impact.Digest, Disposition: events.CleanupImpactWillDelete}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return scan.RequestID, aggregate.Digest
}
