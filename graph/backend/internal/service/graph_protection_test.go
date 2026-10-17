package service

import (
	"errors"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/falkordb"
	"github.com/addp/common/engine/plugins/neo4j"
	"github.com/addp/common/resourcetree"
)

func graphProtectionFixture(t *testing.T) (*projectionstore.Store, *GraphProtectionGate) {
	t.Helper()
	db := setupGraphCleanupTestDB(t)
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Exec("ATTACH DATABASE 'file:graph-protection?mode=memory&cache=private' AS graph").Error; err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.Migrate(db, "graph", "graph", nil)
	if err != nil {
		t.Fatal(err)
	}
	return store, NewGraphProtectionGate(store)
}
func syncGraphProtection(t *testing.T, store *projectionstore.Store, tenant int64, cursor string, changes ...dataprotection.ProjectionChange) {
	t.Helper()
	before, err := store.CurrentCursor(t.Context(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ApplyBatch(t.Context(), tenant, before, &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: cursor, Changes: changes}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}
func graphProtectionProjection(t *testing.T, provider plugin.PropertyGraphProvider, path plugin.EngineCatalogPath, id string) dataprotection.Projection {
	t.Helper()
	target, err := dataprotection.DataItemTargetFromCatalogPath(provider.(plugin.EngineCatalogModelProvider).EngineCatalogModel(), path)
	if err != nil {
		t.Fatal(err)
	}
	p := dataprotection.Projection{SchemaVersion: dataprotection.ProjectionSchemaV4, ProjectionID: id, Revision: "00000000000000000001", ConsumerOwner: "graph", State: dataprotection.ProjectionStateEnrolling, Target: target, TargetEngineID: path.EngineID, Rules: []dataprotection.Rule{}, ValidFrom: time.Now().UTC().Add(-time.Minute)}
	if err = p.Seal(); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestGraphProtectionTargetLifecycle(t *testing.T) {
	for _, provider := range []plugin.PropertyGraphProvider{&neo4j.Neo4jPlugin{}, &falkordb.FalkorDBPlugin{}} {
		t.Run(provider.Type(), func(t *testing.T) {
			model := provider.(plugin.EngineCatalogModelProvider).EngineCatalogModel()
			names := []string{"graph.中文/原样"}
			if provider.Type() == "neo4j" {
				names = []string{"neo4j", "graph"}
			}
			path, err := resourcetree.EngineCatalogPathFromLocator(model, &resourcetree.ResourceLocator{EngineID: 13, Type: resourcetree.TypeGraph, Path: names})
			if err != nil {
				t.Fatal(err)
			}
			store, gate := graphProtectionFixture(t)
			if _, err = gate.BeginTarget(t.Context(), 7, provider, path); !errors.Is(err, ErrGraphProtectionRequired) {
				t.Fatalf("uninitialized=%v", err)
			}
			syncGraphProtection(t, store, 7, "")
			ctx, endResponse := WithGraphProtectionResponse(t.Context())
			end, err := gate.BeginTarget(ctx, 7, provider, path)
			if err != nil {
				t.Fatal(err)
			}
			end()
			if err = gate.ReadyToAcknowledge(ctx, 7, "next"); err == nil {
				t.Fatal("acknowledged before response ended")
			}
			other := path
			other.EngineID = 14
			unrelated := graphProtectionProjection(t, provider, other, "other-engine")
			syncGraphProtection(t, store, 7, "other", dataprotection.ProjectionChange{ChangeID: "other", Operation: dataprotection.ChangeOperationUpsert, Projection: &unrelated})
			end, err = gate.BeginTarget(t.Context(), 7, provider, path)
			if err != nil {
				t.Fatalf("unrelated engine blocked: %v", err)
			}
			end()
			adjacent := path
			adjacent.Segments = append([]plugin.EngineCatalogSegment(nil), path.Segments...)
			adjacent.Segments[1].Name += "-adjacent"
			neighbor := graphProtectionProjection(t, provider, adjacent, "neighbor")
			syncGraphProtection(t, store, 7, "neighbor", dataprotection.ProjectionChange{ChangeID: "neighbor", Operation: dataprotection.ChangeOperationUpsert, Projection: &neighbor})
			end, err = gate.BeginTarget(t.Context(), 7, provider, path)
			if err != nil {
				t.Fatalf("unmanaged graph in same engine blocked: %v", err)
			}
			end()
			managed := graphProtectionProjection(t, provider, path, "target")
			syncGraphProtection(t, store, 7, "target", dataprotection.ProjectionChange{ChangeID: "target", Operation: dataprotection.ChangeOperationUpsert, Projection: &managed})
			if _, err = gate.BeginTarget(t.Context(), 7, provider, path); !errors.Is(err, ErrGraphProtectionRequired) {
				t.Fatalf("managed read=%v", err)
			}
			if err = gate.ReadyToAcknowledge(ctx, 7, "target"); err == nil {
				t.Fatal("new enrollment overtook old response")
			}
			endResponse()
			endResponse()
			if err = gate.ReadyToAcknowledge(ctx, 7, "target"); err != nil {
				t.Fatal(err)
			}
			if _, err = gate.BeginTarget(t.Context(), 8, provider, path); err == nil {
				t.Fatal("cross tenant missing checkpoint admitted")
			}
			syncGraphProtection(t, store, 8, "")
			end, err = gate.BeginTarget(t.Context(), 8, provider, path)
			if err != nil {
				t.Fatal(err)
			}
			end()
			release := dataprotection.ProjectionRelease{ProjectionID: managed.ProjectionID, Revision: "00000000000000000002", Target: managed.Target}
			syncGraphProtection(t, store, 7, "released", dataprotection.ProjectionChange{ChangeID: "release", Operation: dataprotection.ChangeOperationRelease, Release: &release})
			end, err = gate.BeginTarget(t.Context(), 7, provider, path)
			if err != nil {
				t.Fatalf("release failed: %v", err)
			}
			end()
			if _, err = gate.BeginTarget(ctx, 7, provider, path); !errors.Is(err, ErrGraphProtectionRequired) {
				t.Fatal("closed response context admitted a late read", err)
			}
			if err = gate.ReadyToAcknowledge(t.Context(), 7, "released"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
