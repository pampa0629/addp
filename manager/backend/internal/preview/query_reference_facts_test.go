package preview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

func TestResourceFactsUseMetaAncestorsAndRejectCallerOrCrossTenantNameFacts(t *testing.T) {
	tenant, itemID := uint(1), uint(7)
	item := commonModels.MetaItem{ID: itemID, TenantID: tenant, EngineID: 11, NodeID: 8, ItemType: "table", Name: " table.with dot ", FullName: "opaque display", ScannedDepth: "deep", Attributes: map[string]interface{}{"item": map[string]interface{}{"data_type": "table", "layout": "single"}}}
	parentID := uint(2)
	facts := commonModels.MetaItemAncestors{Item: item, Ancestors: []commonModels.MetaNode{
		{ID: parentID, TenantID: tenant, EngineID: 11, NodeType: "server", FullName: ""},
		{ID: 8, TenantID: tenant, EngineID: 11, ParentNodeID: &parentID, NodeType: "schema", Name: " schema.with dot ", FullName: "display schema"},
	}}
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/engines/11":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 11, "tenant_id": 1, "name": "Business PostgreSQL", "engine_type": "postgresql", "lifecycle_state": "active", "connection_status": "online"})
		case "/api/v1/meta/items/7":
			_ = json.NewEncoder(w).Encode(item)
		case "/api/v1/meta/items/7/ancestors":
			_ = json.NewEncoder(w).Encode(facts)
		default:
			t.Errorf("unexpected data or source call: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer service.Close()
	tokens := client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "test-service-token", nil })
	resolver := NewPreviewResolver(nil, client.NewSystemClient(service.URL, tokens), client.NewMetaClient(service.URL, tokens))
	result, err := resolver.ResourceFactsFromURI(context.Background(), "addp://engine/11/path/forged/other?type=table&item_id=7", &tenant)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.QueryReferences["sql"].Parts, []string{" schema.with dot ", " table.with dot "}) {
		t.Fatalf("caller path was trusted: %#v", result)
	}
	req := &PreviewResolverRequest{Engine: &commonModels.Engine{ID: 11, EngineType: "postgresql"}, TenantID: &tenant, MetaItemID: &itemID, ItemName: item.Name, ItemFullName: item.FullName, ItemType: item.ItemType, Locator: &resourcetree.ResourceLocator{EngineID: 11, Path: []string{"forged", "other"}, Type: "table"}}
	if len(queryReferences(req)) != 0 {
		t.Fatal("caller path alone became owner facts")
	}
	for _, change := range []func(*commonModels.MetaItemAncestors){
		func(f *commonModels.MetaItemAncestors) { f.Item.TenantID = 2 },
		func(f *commonModels.MetaItemAncestors) { f.Item.EngineID = 12 },
		func(f *commonModels.MetaItemAncestors) { f.Item.Name = "raced table" },
		func(f *commonModels.MetaItemAncestors) { f.Ancestors[1].TenantID = 2 },
		func(f *commonModels.MetaItemAncestors) { f.Ancestors[1].ParentNodeID = nil },
		func(f *commonModels.MetaItemAncestors) { f.Ancestors[1].ID = 9 },
	} {
		altered := facts
		altered.Ancestors = append([]commonModels.MetaNode(nil), facts.Ancestors...)
		change(&altered)
		if _, err := queryCatalogPathFromMeta(req, &altered); err == nil {
			t.Fatal("invalid owner facts accepted")
		}
	}
	path, err := queryCatalogPathFromMeta(req, &facts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugin.EngineCatalogPathWithoutRoot(path).Segments) != 2 {
		t.Fatal(path)
	}
}
