package projectionstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
)

func scopeOnlyPlan(t *testing.T, ids ...uint) (plugin.PreparedQuery, *int) {
	t.Helper()
	calls := 0
	p, err := plugin.NewPreparedQuery(&plugin.QueryAnalysis{Language: "cypher", SchemaCoverage: plugin.QuerySchemaCoverageUnknown},
		func(context.Context) (*plugin.QueryEngineReadScope, error) {
			return plugin.NewQueryEngineReadScope(ids...)
		},
		func(context.Context) (*plugin.QueryReadSet, error) {
			calls++
			return nil, plugin.ErrQueryReadSetUnresolved
		}, nil,
		func(context.Context) (*plugin.QueryResult, error) { return &plugin.QueryResult{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	return p, &calls
}

func TestEngineScopeExclusionRequiresCompleteProjectionSnapshot(t *testing.T) {
	store, err := Migrate(openProjectionStoreDB(t), "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, calls := scopeOnlyPlan(t, 13)
	prepare := func() error {
		_, err := store.PrepareQueryProtection(t.Context(), 7, plugin.GraphCatalogModel(), plan, "query", dataprotection.SubjectReference{}, time.Now().UTC())
		return err
	}
	if err := prepare(); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("empty uninitialized store admitted: %v", err)
	}
	projection := enrollingProjection(t, "develop", dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "mongodb-persons"})
	projection.TargetEngineID = 11
	projection.ExpiresAt = dataprotection.ProjectionDeadline(time.Now().Add(-time.Second))
	projection.ValidFrom = projection.ExpiresAt.Add(-time.Hour)
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	batch := &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "page1", HasMore: true, Changes: []dataprotection.ProjectionChange{{ChangeID: "p1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}}
	if err := store.ApplyBatch(t.Context(), 7, "", batch, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := prepare(); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("partial replay admitted: %v", err)
	}
	if err := store.ApplyBatch(t.Context(), 7, "page1", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "page1"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := prepare(); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Fatal("disjoint engine forced exact read set")
	}
	if err := store.RequireReadPlanUnmanaged(t.Context(), 7, plugin.GraphCatalogModel(), plan, time.Now()); err != nil {
		t.Fatalf("graph exclusion: %v", err)
	}
	overlapping, overlapCalls := scopeOnlyPlan(t, 11, 13)
	if err := store.RequireReadPlanUnmanaged(t.Context(), 7, plugin.GraphCatalogModel(), overlapping, time.Now()); !errors.Is(err, plugin.ErrQueryReadSetUnresolved) || *overlapCalls != 1 {
		t.Fatalf("expired enrolling target lost protection: %v", err)
	}
	initializeTenant(t, store, 8)
	if err := store.RequireReadPlanUnmanaged(t.Context(), 8, plugin.GraphCatalogModel(), overlapping, time.Now()); err != nil {
		t.Fatalf("cross tenant protection: %v", err)
	}
}

func TestEmptyInitialSyncRefreshesOtherProcessAtUnchangedCursor(t *testing.T) {
	db := openProjectionStoreDB(t)
	writer, err := Migrate(db, "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(db, "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.EnsureCurrent(t.Context(), 7); !errors.Is(err, ErrNotInitialized) {
		t.Fatal(err)
	}
	initializeTenant(t, writer, 7)
	if err := reader.EnsureCurrent(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if !reader.engineScopeDisjoint(7, &plugin.QueryEngineReadScope{EngineIDs: []uint{13}}) {
		t.Fatal("completed empty feed was not observed")
	}
}

type replaySource struct {
	pages            map[string]*dataprotection.ProjectionChangesResponse
	fail             bool
	acknowledgements []string
}

func (s *replaySource) ListProtectionProjectionChangesForTenant(_ context.Context, _ uint, cursor string, _ int) (*dataprotection.ProjectionChangesResponse, error) {
	if s.fail && cursor == "old" {
		return nil, errors.New("source offline")
	}
	return s.pages[cursor], nil
}
func (s *replaySource) AcknowledgeProtectionProjectionCursorForTenant(_ context.Context, _ uint, cursor string) error {
	if cursor != "new" {
		return errors.New("ack would move backwards")
	}
	s.acknowledgements = append(s.acknowledgements, cursor)
	return nil
}
func TestReplayAcknowledgesOnlyCompleteFeedAndResumesAfterFailure(t *testing.T) {
	store, err := Migrate(openProjectionStoreDB(t), "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	source := &replaySource{fail: true, pages: map[string]*dataprotection.ProjectionChangesResponse{
		"":    {SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "old", HasMore: true},
		"old": {SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "new"},
	}}
	runner := NewRunner(store, source, nil, time.Second, nil)
	if err := runner.syncTenant(t.Context(), 7); err == nil {
		t.Fatal("expected source failure")
	}
	if len(source.acknowledgements) != 0 {
		t.Fatal("partial replay acknowledged")
	}
	if err := store.EnsureCurrent(t.Context(), 7); !errors.Is(err, ErrNotInitialized) {
		t.Fatal(err)
	}
	source.fail = false
	if err := runner.syncTenant(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if len(source.acknowledgements) != 1 || source.acknowledgements[0] != "new" {
		t.Fatal(source.acknowledgements)
	}
	if err := store.EnsureCurrent(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
}

func TestEngineScopeRetainsOtherTargetsAfterOneRelease(t *testing.T) {
	store, err := Migrate(openProjectionStoreDB(t), "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	first := enrollingProjection(t, "develop", dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "first"})
	first.TargetEngineID = 13
	_ = first.Seal()
	second := first
	second.ProjectionID = "second"
	second.Target.ResourceIdentity = "second"
	_ = second.Seal()
	batch := &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "both", Changes: []dataprotection.ProjectionChange{
		{ChangeID: "first", Operation: dataprotection.ChangeOperationUpsert, Projection: &first},
		{ChangeID: "second", Operation: dataprotection.ChangeOperationUpsert, Projection: &second},
	}}
	if err := store.ApplyBatch(t.Context(), 7, "", batch, time.Now()); err != nil {
		t.Fatal(err)
	}
	scope, _ := plugin.NewQueryEngineReadScope(13)
	if store.engineScopeDisjoint(7, scope) {
		t.Fatal("managed engine missing")
	}
	release := dataprotection.ProjectionRelease{ProjectionID: first.ProjectionID, Revision: "00000000000000000002", Target: first.Target}
	batch.NextCursor = "one"
	batch.Changes = []dataprotection.ProjectionChange{{ChangeID: "release-first", Operation: dataprotection.ChangeOperationRelease, Release: &release}}
	if err := store.ApplyBatch(t.Context(), 7, "both", batch, time.Now()); err != nil {
		t.Fatal(err)
	}
	if store.engineScopeDisjoint(7, scope) {
		t.Fatal("one release removed another target's engine scope")
	}
	release.ProjectionID = second.ProjectionID
	release.Target = second.Target
	batch.NextCursor = "none"
	if err := store.ApplyBatch(t.Context(), 7, "one", batch, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !store.engineScopeDisjoint(7, scope) {
		t.Fatal("explicit last release did not remove engine scope")
	}
}
