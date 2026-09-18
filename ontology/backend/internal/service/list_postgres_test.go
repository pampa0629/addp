package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
)

// Runs inside TestPostgresRevisionLifecycle's standard owner fixture/cleanup.
func testManagementLists(t *testing.T, s *RevisionService) {
	ctx := context.Background()
	actor := testActor(301)
	page := models.ListPage{Page: 1, PageSize: 1}
	rows, total, err := s.ListOntologies(ctx, actor, page)
	if err != nil || total != 0 || rows == nil || len(rows) != 0 {
		t.Fatalf("empty tenant: %+v %d %v", rows, total, err)
	}
	create := func(tenant uint64, id string, revision uint64) *models.Revision {
		t.Helper()
		d := testDefinition(id)
		d.Scope.TenantID, d.Scope.Revision = tenant, revision
		r, err := s.CreateDraft(ctx, testActor(tenant), d)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	create(301, "z_outdoor", 1)
	first := create(301, "a_outdoor", 1)
	create(302, "a_outdoor", 1) // Same identity, different Tenant.
	create(302, "foreign_only", 1)
	rows, total, err = s.ListOntologies(ctx, actor, page)
	if err != nil || total != 2 || len(rows) != 1 || rows[0].OntologyID != "a_outdoor" || rows[0].TenantID != 301 {
		t.Fatalf("first page: %+v %d %v", rows, total, err)
	}
	page.Page = 2
	rows, total, err = s.ListOntologies(ctx, actor, page)
	if err != nil || total != 2 || len(rows) != 1 || rows[0].OntologyID != "z_outdoor" {
		t.Fatalf("second page: %+v %d %v", rows, total, err)
	}
	page.Page = 3
	rows, total, err = s.ListOntologies(ctx, actor, page)
	if err != nil || total != 2 || rows == nil || len(rows) != 0 {
		t.Fatalf("past last: %+v %d %v", rows, total, err)
	}
	scope := testDefinition("a_outdoor").Scope
	scope.TenantID = 301
	for _, action := range []string{"submit", "publish", "withdraw"} {
		first, err = s.Transition(ctx, actor, scope, first.Version, action)
		if err != nil {
			t.Fatal(err)
		}
	}
	second := create(301, "a_outdoor", 2)
	page.Page = 1
	history, total, err := s.ListRevisions(ctx, actor, "a_outdoor", page)
	if err != nil || total != 2 || len(history) != 1 || history[0].Revision != 2 || history[0].Status != models.Draft || history[0].Digest != second.Digest || history[0].InitialGeneration != nil {
		t.Fatalf("newest: %+v %d %v", history, total, err)
	}
	page.Page = 2
	history, total, err = s.ListRevisions(ctx, actor, "a_outdoor", page)
	if err != nil || total != 2 || len(history) != 1 {
		t.Fatalf("history: %+v %d %v", history, total, err)
	}
	h := history[0]
	if h.Revision != 1 || h.Version != first.Version || h.Status != models.Withdrawn || h.Digest != first.Digest || h.InitialGeneration == nil || *h.InitialGeneration != *first.Generation || h.InitialExecutionID == nil || *h.InitialExecutionID != *first.BuildExecutionID || h.PublishedAt == nil || !h.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("lost provenance: %+v", h)
	}
	page.Page = 3
	history, total, err = s.ListRevisions(ctx, actor, "a_outdoor", page)
	if err != nil || total != 2 || history == nil || len(history) != 0 {
		t.Fatalf("past history: %+v %d %v", history, total, err)
	}
	page.Page = 1
	history, total, err = s.ListRevisions(ctx, testActor(302), "a_outdoor", page)
	if err != nil || total != 1 || len(history) != 1 || history[0].Status != models.Draft || history[0].Digest == first.Digest {
		t.Fatalf("tenant isolation: %+v %d %v", history, total, err)
	}
	for _, id := range []string{"foreign_only", "missing"} {
		if _, _, err := s.ListRevisions(ctx, actor, id, page); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("%s visible: %v", id, err)
		}
	}
	rows, total, err = s.ListOntologies(ctx, actor, page)
	if err != nil || total != 2 || len(rows) != 1 || rows[0].LastRevision != 2 || rows[0].ActiveRevision != nil {
		t.Fatalf("head conflates draft/active: %+v %d %v", rows, total, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := s.ListOntologies(cancelled, actor, page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel list: %v", err)
	}
	if _, _, err := s.ListRevisions(cancelled, actor, "a_outdoor", page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel history: %v", err)
	}
}
