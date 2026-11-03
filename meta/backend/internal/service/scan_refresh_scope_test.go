package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/scantask"
)

type refreshScopeClientFunc func(context.Context, string, authorization.MetadataRefreshQuery) (*authorization.MetadataRefreshScope, error)

func (f refreshScopeClientFunc) GetMetadataRefreshScope(ctx context.Context, token string, q authorization.MetadataRefreshQuery) (*authorization.MetadataRefreshScope, error) {
	return f(ctx, token, q)
}

func TestRefreshPlanProjectionBindsFrozenHumanAndCompleteRange(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	createTaskExecutionTable(t, db)
	binding := factstest.Binding(plugin.TabularItemPath(9, "schema", "public", "people"))
	ctx := withMetaTestUserOrigin(t.Context(), 7, 33)
	run := scantask.NewManualExecution(7, 33, 9, 0, "postgresql", nil, nil, nil, "basic", true, "manager", time.Now())
	if err := execution.ApplyUserOrigin(ctx, run); err != nil {
		t.Fatal(err)
	}
	run.ExecutionConfig["engine_type"] = "postgresql"
	run.ExecutionConfig["engine_version"] = "3"
	scope := &authorization.MetadataRefreshScope{TenantID: 7, EngineVersion: "3", Query: authorization.MetadataRefreshQuery{Kind: "node", Target: plugin.TabularNamespacePath(9, "schema", "public")}, Operator: authorization.SharingFulfillmentOperator{PrincipalID: *run.ActorPrincipalID, MembershipID: *run.ActorTenantMembershipID, AuthorizationVersion: *run.IssuedAuthorizationVersion}, ObservedAt: time.Now(), Ranges: []authorization.MetadataRefreshRange{{Item: &binding}}}
	if err := freezeManualRefreshScope(run, scope); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	service := NewScanExecutionService(db, nil, nil, nil)
	facts, err := service.GetScanPlanFacts(t.Context(), 7, run.ExecutionID)
	if err != nil || facts.RefreshScope == nil || facts.SourceScope != nil || facts.Validate() != nil {
		t.Fatalf("frozen facts=%+v err=%v", facts, err)
	}
	// A creator/source field cannot replace the authenticated immutable origin.
	if err := db.Model(run).Update("actor_principal_id", int64(34)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetScanPlanFacts(t.Context(), 7, run.ExecutionID); err == nil {
		t.Fatal("wrong actor was accepted")
	}
}

func TestRefreshRangeDedupKeepsNonrecursiveChildAndIndependentSources(t *testing.T) {
	parent := authorization.SourceNodeScope{Target: plugin.FileDirectoryPath(9, "data")}
	child := authorization.SourceNodeScope{Target: plugin.FileDirectoryPath(9, "data/child"), Recursive: true}
	input := []authorization.MetadataRefreshRange{{Node: &parent}, {Node: &child}}
	if got := minimalRefreshRanges(input); len(got) != 2 {
		t.Fatal("nonrecursive parent swallowed child scope")
	}
	parent.Recursive = true
	if got := minimalRefreshRanges(input); len(got) != 1 || got[0].Node != &parent {
		t.Fatal("recursive subtree was read twice")
	}
	if len(input) != 2 {
		t.Fatal("dedup changed original Grant sources")
	}
}
