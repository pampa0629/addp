package scanadapter_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metarepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
	"github.com/addp/meta/internal/scanruntime"
)

type collectionDispatchCatalog struct {
	preciseCatalogBasePlugin
	reads     []string
	deepReads int
	afterRead func(int)
}

func TestCollectionRevocationAfterAdmittedEnumerationStopsNextRead(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	repo := metarepo.NewScanRepository(db)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	catalog := &collectionDispatchCatalog{preciseCatalogBasePlugin: preciseCatalogBasePlugin{
		engineType: "collection-revocation-pg-test", model: plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)}}
	plugin.Register(catalog)
	t.Cleanup(func() { plugin.Unregister(catalog.Type()) })
	tenant := uint(7)
	engine := &commonmodels.Engine{ID: 9, TenantID: &tenant, EngineType: catalog.Type(), Version: 3,
		ConnectionInfo: map[string]interface{}{"binding": "approved-engine"}}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	catalog.afterRead = func(reads int) {
		if reads == 2 {
			close(entered)
			<-release
		}
	}
	revoked := false
	checks := 0
	ctx := scanresource.WithCollectionReadCheck(t.Context(), func(context.Context, *commonmodels.Engine) error {
		checks++
		if revoked {
			return commonapi.ErrForbidden
		}
		return nil
	})
	dispatcher := scanadapter.NewEngineCatalogScanDispatcher(db, repo, log,
		scanruntime.NewDatabaseRuntime(db, log, repo, nil), nil, nil, nil)
	type outcome struct {
		result scanflow.DispatchResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := dispatcher.Dispatch(scanflow.DispatchRequest{Context: ctx, Resource: engine,
			EnginePlugin: catalog, TenantID: tenant, ScanDepth: "basic", Force: true, Mode: scanflow.DispatchManual})
		done <- outcome{result, err}
	}()
	select {
	case <-entered: // The first namespace enumeration has already been admitted.
	case <-time.After(5 * time.Second):
		t.Fatal("enumeration did not enter its bounded barrier")
	}
	revoked = true
	once.Do(func() { close(release) })
	var got outcome
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("revoked execution did not converge")
	}
	if !errors.Is(got.err, commonapi.ErrForbidden) {
		t.Fatalf("mid-scan revocation lost its typed denial: %v", got.err)
	}
	if count, samples := scanflow.FailedTargetDetails(got.err); count != 1 || len(samples) != 1 || samples[0].Target != "beta" {
		t.Fatalf("mid-scan denial lost its target diagnostic: count=%d samples=%v", count, samples)
	}
	if checks != 3 || !reflect.DeepEqual(catalog.reads, []string{"", "alpha"}) || catalog.deepReads != 0 || got.result.Items != 1 {
		t.Fatalf("checks=%d reads=%v deep=%d items=%d", checks, catalog.reads, catalog.deepReads, got.result.Items)
	}
	var items []models.MetaItem
	if err := db.Order("full_name").Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].FullName != "alpha.records" {
		t.Fatalf("admitted enumeration was lost or later source enumerated: %+v", items)
	}
	var roots []models.MetaNode
	if err := db.Where("parent_node_id IS NULL").Find(&roots).Error; err != nil {
		t.Fatal(err)
	}
	for _, root := range roots {
		if root.ScanStatus == "completed" {
			t.Fatal("revoked scope marked complete")
		}
	}
}

func (p *collectionDispatchCatalog) ListChildren(_ context.Context, conn plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	if conn["binding"] != "approved-engine" {
		return nil, errors.New("unexpected source connection")
	}
	p.reads = append(p.reads, target.StringPath())
	if p.afterRead != nil {
		p.afterRead(len(p.reads))
	}
	if len(target.Segments) == 1 {
		entries := make([]plugin.EngineCatalogEntry, 0, 2)
		for _, name := range []string{"alpha", "beta"} {
			entries = append(entries, plugin.EngineCatalogEntry{Name: name, Term: "schema", Kind: "schema", Role: plugin.EngineCatalogRoleBranch, Path: plugin.TabularNamespacePath(target.EngineID, "schema", name)})
		}
		return entries, nil
	}
	return []plugin.EngineCatalogEntry{{Name: "records", Term: "table", Kind: "table", Role: plugin.EngineCatalogRoleLeaf, Path: plugin.TabularItemPath(target.EngineID, "schema", target.Segments[1].Name, "records"), Table: &datatype.TableInfo{Name: "records", Kind: "table"}}}, nil
}

func (p *collectionDispatchCatalog) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return nil, errors.New("full basic collection must not resolve individual leaves")
}

func (p *collectionDispatchCatalog) DescribeEngineCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.deepReads++
	return nil, errors.New("basic collection attempted deep source access")
}

// Exercise the real dispatcher -> namespace runtime -> repository chain, so
// dropping the authorization callback between those layers cannot pass merely
// because the individual guard helpers were tested in isolation.
func TestCollectionDispatchRechecksBeforeEveryCatalogRead(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		denyAt      int
		cancelAfter int
		wantChecks  int
		wantReads   []string
		wantItems   []string
	}{
		{name: "approved basic", wantChecks: 3, wantReads: []string{"", "alpha", "beta"}, wantItems: []string{"alpha.records", "beta.records"}},
		{name: "unapproved root", denyAt: 1, wantChecks: 1},
		{name: "revoked after root", denyAt: 2, wantChecks: 3, wantReads: []string{""}},
		{name: "revoked between namespaces", denyAt: 3, wantChecks: 3, wantReads: []string{"", "alpha"}, wantItems: []string{"alpha.records"}},
		{name: "cancelled during namespace enumeration", cancelAfter: 2, wantChecks: 2, wantReads: []string{"", "alpha"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := metatest.OpenMetadataDB(t)
			repo := metarepo.NewScanRepository(db)
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			catalog := &collectionDispatchCatalog{preciseCatalogBasePlugin: preciseCatalogBasePlugin{engineType: "collection-dispatch-pg-test", model: plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)}}
			plugin.Register(catalog)
			t.Cleanup(func() { plugin.Unregister(catalog.Type()) })
			tenant := uint(7)
			engine := &commonmodels.Engine{ID: 9, TenantID: &tenant, EngineType: catalog.Type(), Version: 3, ConnectionInfo: map[string]interface{}{"binding": "approved-engine"}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario.cancelAfter > 0 {
				catalog.afterRead = func(reads int) {
					if reads == scenario.cancelAfter {
						cancel()
					}
				}
			}
			checks := 0
			ctx = scanresource.WithCollectionReadCheck(ctx, func(_ context.Context, resource *commonmodels.Engine) error {
				checks++
				if resource != engine {
					t.Fatal("source binding changed between qualification and catalog read")
				}
				if scenario.denyAt > 0 && checks >= scenario.denyAt {
					return errors.New("current collection qualification unavailable")
				}
				return nil
			})
			dispatcher := scanadapter.NewEngineCatalogScanDispatcher(db, repo, log, scanruntime.NewDatabaseRuntime(db, log, repo, nil), nil, nil, nil)
			result, err := dispatcher.Dispatch(scanflow.DispatchRequest{Context: ctx, Resource: engine, EnginePlugin: catalog, TenantID: tenant, ScanDepth: "basic", Force: true, Mode: scanflow.DispatchManual})
			wantSuccess := scenario.denyAt == 0 && scenario.cancelAfter == 0
			if (err == nil) != wantSuccess {
				t.Fatalf("success=%v error=%v", wantSuccess, err)
			}
			if checks != scenario.wantChecks || !reflect.DeepEqual(catalog.reads, scenario.wantReads) || catalog.deepReads != 0 {
				t.Fatalf("checks=%d reads=%q deep=%d; want checks=%d reads=%q and no deep reads", checks, catalog.reads, catalog.deepReads, scenario.wantChecks, scenario.wantReads)
			}
			var items []models.MetaItem
			if err := db.Order("full_name").Find(&items).Error; err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, item := range items {
				names = append(names, item.FullName)
				if item.ScannedDepth != "basic" {
					t.Fatal("basic source scan persisted another depth")
				}
			}
			if !reflect.DeepEqual(names, scenario.wantItems) || result.Items != len(scenario.wantItems) {
				t.Fatalf("items=%q count=%d, want %q", names, result.Items, scenario.wantItems)
			}
			var roots []models.MetaNode
			if err := db.Where("parent_node_id IS NULL").Find(&roots).Error; err != nil {
				t.Fatal(err)
			}
			for _, root := range roots {
				if !wantSuccess && root.ScanStatus == "completed" {
					t.Fatal("denied or cancelled scope was marked complete")
				}
			}
		})
	}
}
