package preview

import (
	"context"
	"encoding/json"
	"github.com/addp/common/dataitem/authorizationfacts"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func pgMetaForRead(name string) *models.MetaItem {
	return &models.MetaItem{ID: 7, NodeID: 3, TenantID: 2, EngineID: 12, Name: name, FullName: "public." + name, ItemType: "table", Fingerprint: models.GenerateItemFingerprint(12, "public."+name), Attributes: map[string]interface{}{"item": map[string]interface{}{"layout": "single"}, "storage": map[string]interface{}{"schema_name": "public"}}}
}
func TestPGReadBindingsReusesResolvedItemAndProvesEveryDependency(t *testing.T) {
	tenant := uint(2)
	primary := pgMetaForRead("C")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/meta/runtime/data-items/authorization-facts/query" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer meta-service" {
			t.Error("wrong dependency fact query")
		}
		var query authorizationfacts.AuthorizationFactsQuery
		_ = json.NewDecoder(r.Body).Decode(&query)
		item := pgMetaForRead("D")
		if len(query.Fingerprints) != 1 || query.Fingerprints[0] != item.Fingerprint {
			t.Error("primary or unrelated dependency queried")
		}
		fact := authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: 12, ItemType: "table", ItemFingerprint: item.Fingerprint, FullName: item.FullName, BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now().UTC(), Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: plugin.TabularItemPath(12, "schema", "public", "D")}}
		hash, err := fact.ComputeBoundaryHash()
		if err != nil {
			t.Fatal(err)
		}
		fact.BoundaryHash = &hash
		_ = json.NewEncoder(w).Encode(authorizationfacts.AuthorizationFactsBatch{Data: []authorizationfacts.AuthorizationFactsResult{{ItemFingerprint: fact.ItemFingerprint, Facts: &fact}}})
	}))
	defer server.Close()
	resolver := &PreviewResolver{metaClient: client.NewMetaClient(server.URL, client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "meta-service", nil }))}
	req := &PreviewResolverRequest{Engine: &models.Engine{ID: 12, EngineType: "postgresql"}, TenantID: &tenant, MetaItem: primary}
	c := plugin.TabularItemPath(12, "schema", "public", "C")
	d := plugin.TabularItemPath(12, "schema", "public", "D")
	if bindings, err := resolver.PGReadBindings(t.Context(), req, []plugin.EngineCatalogPath{c}); err != nil || len(bindings) != 1 || calls != 0 {
		t.Fatal("primary facts were not reused", bindings, err, calls)
	}
	if bindings, err := resolver.PGReadBindings(t.Context(), req, []plugin.EngineCatalogPath{c, d}); err != nil || len(bindings) != 2 || bindings[0].ItemFingerprint != primary.Fingerprint || calls != 1 {
		t.Fatal("incomplete read set", bindings, err, calls)
	}
	for _, change := range []func(*PreviewResolverRequest){
		func(r *PreviewResolverRequest) { r.MetaItem = nil },
		func(r *PreviewResolverRequest) { other := *r.MetaItem; other.TenantID = 3; r.MetaItem = &other },
		func(r *PreviewResolverRequest) { other := *r.MetaItem; other.Attributes = nil; r.MetaItem = &other },
		func(r *PreviewResolverRequest) {
			other := *r.MetaItem
			other.Fingerprint = models.GenerateItemFingerprint(12, "wrong")
			r.MetaItem = &other
		},
		func(r *PreviewResolverRequest) {
			other := *r.MetaItem
			other.Attributes = map[string]interface{}{"item": map[string]interface{}{"layout": "single"}, "storage": map[string]interface{}{"schema_name": "public"}, "type_info": map[string]interface{}{"table": map[string]interface{}{"kind": "view"}}}
			r.MetaItem = &other
		},
		func(r *PreviewResolverRequest) { r.Engine = &models.Engine{ID: 12, EngineType: "mysql"} },
	} {
		copy := *req
		change(&copy)
		if _, err := resolver.PGReadBindings(t.Context(), &copy, []plugin.EngineCatalogPath{c}); err == nil {
			t.Fatal("invalid evidence became PG proof")
		}
	}
	if calls != 1 {
		t.Fatal("invalid current item triggered a replacement lookup", calls)
	}
}
