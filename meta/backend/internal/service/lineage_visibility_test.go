package service

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func TestUserLineageFiltersBeforeTraversalLimitsAndCounts(t *testing.T) {
	exerciseUserLineageVisibility(t, openLineageTestDB(t))
}

func exerciseUserLineageVisibility(t *testing.T, db *gorm.DB) {
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	root := createLineageItem(t, db, 7, "root", "root")
	hidden := createLineageItem(t, db, 7, "hidden", "hidden")
	behind := createLineageItem(t, db, 7, "behind", "behind")
	visible := createLineageItem(t, db, 7, "visible", "visible")
	next := createLineageItem(t, db, 7, "next", "next")
	now := time.Now().UTC().Add(-time.Hour)
	for _, pair := range [][2]uint{{root.ID, hidden.ID}, {hidden.ID, behind.ID}, {root.ID, visible.ID}, {visible.ID, next.ID}} {
		r := models.LineageItemRelation{TenantID: 7, SourceItemID: pair[0], TargetItemID: pair[1], RelationKind: "derive", Granularity: "item", Status: "active", FirstObservedAt: now, LastObservedAt: now}
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.RecordServicePublication(context.Background(), 7, models.RecordServicePublicationRequest{ServiceID: 77, ServiceName: "secret service", ServiceUpdatedAt: now, PublishedRevision: "r1", Dependencies: []models.LineageServiceDependencyInput{{SourceItemID: root.ID, DependencyKind: "table"}}}); err != nil {
		t.Fatal(err)
	}
	allow := []uint{root.ID, visible.ID, next.ID, behind.ID}
	calls := 0
	ctx := WithLineageVisibility(context.Background(), func(context.Context) ([]uint, error) { calls++; return allow, nil })
	request := models.LineageGraphRequest{SubjectKind: "data_item", ItemID: &root.ID, Direction: "downstream", Depth: 1, Limit: 2}
	for _, historical := range []bool{false, true} {
		if historical {
			at := now.Add(time.Minute)
			request.AsOf = &at
		}
		graph, err := svc.GetGraph(ctx, 7, request)
		if err != nil || len(graph.Nodes) != 2 || len(graph.Edges) != 1 || graph.Truncated || !graph.PermissionFiltered {
			t.Fatalf("visible slice: %+v %v", graph, err)
		}
		if *graph.Edges[0].Target.ItemID != visible.ID || graph.Subject.HiddenDownstreamCount != 0 || graph.Edges[0].Target.HiddenDownstreamCount != 1 {
			t.Fatalf("hidden counts/limit leaked: %+v", graph)
		}
	}
	request.AsOf = nil
	request.Depth = 5
	request.Limit = 20
	graph, err := svc.GetGraph(ctx, 7, request)
	if err != nil || len(graph.Nodes) != 3 || len(graph.Edges) != 2 || !graph.PermissionFiltered {
		t.Fatalf("hidden bridge traversed: %+v %v", graph, err)
	}
	for _, n := range graph.Nodes {
		if n.ItemID == nil || *n.ItemID == hidden.ID || *n.ItemID == behind.ID || n.ServiceID != nil {
			t.Fatalf("hidden node: %+v", n)
		}
	}
	allow = nil
	if _, err = svc.GetGraph(ctx, 7, request); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("revocation ignored: %v", err)
	}
	if calls != 4 {
		t.Fatalf("observations=%d", calls)
	}
	request.SubjectKind = "published_service"
	request.ServiceID = uintPtr(77)
	request.Revision = "r1"
	if _, err = svc.GetGraph(ctx, 7, request); !errors.Is(err, commonapi.ErrForbidden) || calls != 4 {
		t.Fatalf("service queried: %v %d", err, calls)
	}
	request.SubjectKind = "data_item"
	failure := errors.New("authority unavailable")
	ctx = WithLineageVisibility(context.Background(), func(context.Context) ([]uint, error) { return []uint{root.ID}, failure })
	if graph, err = svc.GetGraph(ctx, 7, request); !errors.Is(err, failure) || len(graph.Nodes) != 0 {
		t.Fatalf("partial authority accepted: %+v %v", graph, err)
	}
}

func TestUserFieldLineageRequiresBothEndpointsAndSafeEvidence(t *testing.T) {
	exerciseUserFieldLineageVisibility(t, openLineageTestDB(t))
}

func exerciseUserFieldLineageVisibility(t *testing.T, db *gorm.DB) {
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	schema := fieldTestSchema(t, "id", "other")
	source := fieldTestItem(t, db, 7, "source", schema)
	target := fieldTestItem(t, db, 7, "target", schema)
	fieldTestExecution(t, db, "private-expression", source, target, schema, schema, "replace", time.Now().UTC().Add(-time.Hour), commonExecution.LineageFieldMapping{SourceField: "id", TargetField: "id", Transformation: "direct"})
	if _, err := svc.CollectExecution(context.Background(), 7, "private-expression"); err != nil {
		t.Fatal(err)
	}
	allow := []uint{target.ID}
	ctx := WithLineageVisibility(context.Background(), func(context.Context) ([]uint, error) { return allow, nil })
	req := models.LineageGraphRequest{SubjectKind: "field_ref", ItemID: &target.ID, FieldName: "id", Direction: "upstream", Depth: 2, Limit: 2}
	graph, err := svc.GetGraph(ctx, 7, req)
	if err != nil || len(graph.Nodes) != 1 || len(graph.Edges) != 0 || graph.Truncated || !graph.PermissionFiltered {
		t.Fatalf("hidden field: %+v %v", graph, err)
	}
	req.FieldName = "other"
	graph, err = svc.GetGraph(ctx, 7, req)
	if err != nil || graph.PermissionFiltered || len(graph.Edges) != 0 {
		t.Fatalf("another field disclosed hidden evidence: %+v %v", graph, err)
	}
	req.FieldName, req.Direction = "id", "downstream"
	graph, err = svc.GetGraph(ctx, 7, req)
	if err != nil || graph.PermissionFiltered || len(graph.Edges) != 0 {
		t.Fatalf("opposite field direction disclosed hidden evidence: %+v %v", graph, err)
	}
	req.Direction = "upstream"
	allow = append(allow, source.ID)
	graph, err = svc.GetGraph(ctx, 7, req)
	if err != nil || len(graph.Nodes) != 2 || len(graph.Edges) != 1 || graph.PermissionFiltered {
		t.Fatalf("visible field: %+v %v", graph, err)
	}
	if graph.Edges[0].Transformation != "" || graph.Edges[0].Evidence["transformation"] != nil || graph.Edges[0].Evidence["execution_id"] != "private-expression" {
		t.Fatalf("unsafe evidence: %+v", graph.Edges[0])
	}
	// Machine fact consumers retain their separate contract.
	raw, err := svc.GetGraph(context.Background(), 7, req)
	if err != nil || raw.Edges[0].Evidence["transformation"] != "direct" || raw.PermissionFiltered {
		t.Fatalf("machine contract changed: %+v %v", raw, err)
	}
	// Basic identification need not persist deep field structure. The existing
	// explicit-hash query must prove its fields from the execution observation.
	if err := db.Model(&models.MetaItem{}).Where("id IN ?", []uint{source.ID, target.ID}).UpdateColumn("attributes", "{}").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetGraph(ctx, 7, req); err == nil || err.Error() != "table schema fields are required" {
		t.Fatalf("basic-only current-schema query: %v", err)
	}
	req.SchemaSnapshotHash = schema.Hash
	graph, err = svc.GetGraph(ctx, 7, req)
	if err != nil || len(graph.Nodes) != 2 || len(graph.Edges) != 1 || graph.Truncated {
		t.Fatalf("proven execution schema without deep metadata: %+v %v", graph, err)
	}
	allow = []uint{target.ID}
	graph, err = svc.GetGraph(ctx, 7, req)
	if err != nil || len(graph.Nodes) != 1 || len(graph.Edges) != 0 || graph.Truncated || !graph.PermissionFiltered {
		t.Fatalf("execution schema bypassed source withdrawal: %+v %v", graph, err)
	}
	allow = nil
	if _, err := svc.GetGraph(ctx, 7, req); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("execution schema bypassed root withdrawal: %v", err)
	}
}

func TestUserLineagePermissionNoticeHonorsQueryBoundary(t *testing.T) {
	exerciseUserLineagePermissionBoundary(t, openLineageTestDB(t))
}

func exerciseUserLineagePermissionBoundary(t *testing.T, db *gorm.DB) {
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	root := createLineageItem(t, db, 7, "notice_root", "notice_root")
	visible := createLineageItem(t, db, 7, "notice_visible", "notice_visible")
	hidden := createLineageItem(t, db, 7, "notice_hidden", "notice_hidden")
	side := createLineageItem(t, db, 7, "notice_side", "notice_side")
	deleted := createLineageItem(t, db, 7, "notice_deleted", "notice_deleted")
	other := createLineageItem(t, db, 8, "notice_other_tenant", "notice_other_tenant")
	if err := db.Delete(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Hour)
	add := func(source, target uint, status string) {
		t.Helper()
		r := models.LineageItemRelation{TenantID: 7, SourceItemID: source, TargetItemID: target,
			RelationKind: "derive", Granularity: "item", Status: status, FirstObservedAt: now, LastObservedAt: now}
		if status == "closed" {
			closed := now.Add(time.Minute)
			r.ClosedAt = &closed
		}
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	add(root.ID, visible.ID, "active")
	add(side.ID, visible.ID, "active") // Not reachable by changing a downstream traversal's direction.
	add(root.ID, deleted.ID, "active")
	add(root.ID, other.ID, "active")
	add(root.ID, hidden.ID, "closed")
	ctx := WithLineageVisibility(context.Background(), func(context.Context) ([]uint, error) { return []uint{root.ID, visible.ID}, nil })
	req := models.LineageGraphRequest{SubjectKind: "data_item", ItemID: &root.ID, Direction: "downstream", Depth: 2, Limit: 20}
	check := func(want bool) {
		t.Helper()
		graph, err := svc.GetGraph(ctx, 7, req)
		if err != nil || graph.PermissionFiltered != want || graph.Truncated {
			t.Fatalf("notice direction=%s depth=%d history=%v: %+v %v", req.Direction, req.Depth, req.AsOf, graph, err)
		}
		for _, node := range graph.Nodes {
			if node.ItemID == nil || (*node.ItemID != root.ID && *node.ItemID != visible.ID) {
				t.Fatalf("notice disclosed endpoint: %+v", node)
			}
		}
	}
	check(false)
	at := now.Add(2 * time.Minute)
	req.AsOf = &at
	check(false) // Closed, deleted and cross-Tenant endpoints are not permission-hidden.
	req.AsOf = nil
	add(visible.ID, hidden.ID, "active")
	req.Depth = 0
	check(false)
	req.Depth = 1
	check(false) // Do not probe beyond the requested depth.
	req.Depth = 2
	check(true)
	req.Direction = "upstream"
	check(false)
	req.Direction = "downstream"
	req.AsOf = &at
	check(true)
	if err := db.Model(&models.LineageItemRelation{}).Where("source_item_id = ? AND target_item_id = ?", visible.ID, hidden.ID).Update("last_observed_at", now.Add(3*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	check(false) // A future observation cannot disclose a hidden historical relationship.
}

func TestUserLineageVisibilityAgainstPostgres(t *testing.T) {
	for name, verify := range map[string]func(*testing.T, *gorm.DB){"item": exerciseUserLineageVisibility, "field": exerciseUserFieldLineageVisibility, "permission_boundary": exerciseUserLineagePermissionBoundary, "basic_execution_snapshot": exerciseBasicExecutionFieldLineage, "missing_fields_keep_stale": exerciseMissingCurrentFieldsNeverRestoreStaleLineage} {
		t.Run(name, func(t *testing.T) {
			db := openLineageCollectorPostgresDB(t)
			if err := db.AutoMigrate(&models.LineageServiceDependency{}); err != nil {
				t.Fatal(err)
			}
			verify(t, db)
		})
	}
}
