package scanadapter_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanruntime"
	"github.com/addp/meta/internal/service"
	"gorm.io/gorm"
)

// Reused by the standard precise-scan T1 and PostgreSQL T2 gates.
func testCollectionContentProjection(t *testing.T, db *gorm.DB) {
	var documents []commonClient.ManagerContentDocument
	manager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.Header.Get("Authorization") != "Bearer meta-tenant-7" {
			t.Errorf("unexpected Manager request: %s %s", r.Method, r.URL)
		}
		var document commonClient.ManagerContentDocument
		if err := json.NewDecoder(r.Body).Decode(&document); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/api/v1/manager/runtime/content-documents/"+document.DocumentID {
			t.Errorf("projection path = %s", r.URL.Path)
		}
		documents = append(documents, document)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer manager.Close()
	client := commonClient.NewManagerContentClient(manager.URL, commonClient.ServiceTokenProviderFunc(func(_ context.Context, tenant uint) (string, error) {
		if tenant != 7 {
			t.Errorf("projection tenant = %d, want 7", tenant)
		}
		return "meta-tenant-7", nil
	}), manager.Client())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := metaRepo.NewScanRepository(db)
	p := &preciseCatalogPlugin{preciseCatalogBasePlugin: preciseCatalogBasePlugin{engineType: "mongodb", model: plugin.DynamicSchemaCatalogModel()}}
	resource := &commonModels.Engine{ID: 11, EngineType: p.Type(), Name: "MongoDB"}
	loc := &resourcetree.ResourceLocator{EngineID: 11, Type: "collection", Path: []string{"Outdoor", "Outdoors"}}
	path, err := resourcetree.EngineCatalogPathFromLocator(p.model, loc)
	if err != nil {
		t.Fatal(err)
	}
	p.entries = []plugin.EngineCatalogEntry{{Name: "Outdoors", Path: path, Term: "collection", Kind: "collection", Role: plugin.EngineCatalogRoleLeaf}}
	root, err := metaRepo.EnsureEngineCatalogRootNode(repo, 7, resource, p)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := repo.UpsertNode(7, 11, root, "database", "Outdoor", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := repo.UpsertItemWithDepth(7, 11, parent, "collection", "Persons", "Outdoor.Persons", models.JSONMap{}, nil, nil, nil, "basic")
	if err != nil {
		t.Fatal(err)
	}
	sibling, err = repo.GetItemByID(7, sibling.ID)
	if err != nil {
		t.Fatal(err)
	}
	var parentsBefore []models.MetaNode
	if err := db.Order("id").Find(&parentsBefore).Error; err != nil {
		t.Fatal(err)
	}
	runtimes := scanruntime.NewRuntimes(db, log, repo, service.NewIndexerService(client, log))
	d := scanadapter.NewEngineCatalogScanDispatcher(db, repo, log, runtimes.Database, runtimes.BranchLeaf, runtimes.DirectLeaf, nil)
	req := scanflow.DispatchRequest{Context: t.Context(), Resource: resource, EnginePlugin: p, TenantID: 7, Targets: []string{loc.ToURI()}, ScanDepth: "basic", Force: true}
	for _, depth := range []string{"basic", "deep", "basic"} {
		req.ScanDepth = depth
		p.calls = nil
		before := len(documents)
		result, err := d.Dispatch(req)
		if err != nil || result.Items != 1 || len(documents) != before+1 {
			t.Fatalf("%s scan result=%+v error=%v projections=%d, want %d", depth, result, err, len(documents), before+1)
		}
		if depth == "basic" && !reflect.DeepEqual(p.calls, []string{"resolve:Outdoor/Outdoors"}) {
			t.Fatalf("basic backfill read extra source facts: %v", p.calls)
		}
		if before > 0 && len(documents[before].Fields) != 1 {
			t.Fatalf("%s lost registered fields: %+v", depth, documents[before])
		}
	}
	item, found, err := repo.FindItemByFullName(7, 11, "Outdoor.Outdoors")
	if err != nil || !found {
		t.Fatalf("saved item: %v %v", found, err)
	}
	attrsBefore := item.Attributes
	req.Force = false
	p.calls = nil
	if _, err := d.Dispatch(req); err != nil {
		t.Fatal(err)
	}
	if len(documents) != 4 || !reflect.DeepEqual(p.calls, []string{"resolve:Outdoor/Outdoors"}) {
		t.Fatalf("unchanged collection did not republish stored projection: documents=%d calls=%v", len(documents), p.calls)
	}
	if _, err := runtimes.ItemRefresh.RefreshKnownItemByIDWithPlugin(t.Context(), p, resource, 7, item.ID); err != nil {
		t.Fatal(err)
	}
	if len(documents) != 5 {
		t.Fatalf("item refresh projections=%d, want 5", len(documents))
	}
	for _, doc := range documents {
		projectionLoc, err := resourcetree.ParseURI(doc.Locator)
		if err != nil || projectionLoc.Type != "collection" || !reflect.DeepEqual(projectionLoc.Path, loc.Path) || projectionLoc.ItemID == nil || *projectionLoc.ItemID != item.ID {
			t.Fatalf("collection locator=%s error=%v", doc.Locator, err)
		}
		if doc.DocumentID != item.Fingerprint || doc.EngineID != 11 || doc.DataItemType != "collection" || doc.Schema != "Outdoor" || doc.FullName != "Outdoor.Outdoors" || doc.PayloadKind != commonClient.ManagerContentPayloadTechnicalMetadata || doc.Validate() != nil {
			t.Fatalf("collection projection=%+v", doc)
		}
	}
	p.failFacts = true
	req.ScanDepth, req.Force = "deep", true
	if _, err := d.Dispatch(req); err == nil {
		t.Fatal("failed scan succeeded")
	}
	if _, err := runtimes.ItemRefresh.RefreshKnownItemByIDWithPlugin(t.Context(), p, resource, 7, item.ID); err == nil {
		t.Fatal("failed refresh succeeded")
	}
	if len(documents) != 5 {
		t.Fatal("failed source scan submitted a projection")
	}
	var parentsAfter []models.MetaNode
	if err := db.Order("id").Find(&parentsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parentsBefore, parentsAfter) {
		t.Fatal("precise scan changed ancestor state")
	}
	loadedSibling, err := repo.GetItemByID(7, sibling.ID)
	if err != nil || !reflect.DeepEqual(sibling, loadedSibling) {
		t.Fatalf("precise scan changed sibling: %v", err)
	}
	itemAfter, err := repo.GetItemByID(7, item.ID)
	if err != nil || !reflect.DeepEqual(attrsBefore, itemAfter.Attributes) || itemAfter.ScannedDepth != "deep" {
		t.Fatalf("registered structure changed: %v", err)
	}
}
