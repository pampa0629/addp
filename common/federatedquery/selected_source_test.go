package federatedquery_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/federatedquery"
	"github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

type selectedSourceTokens struct{}

func (selectedSourceTokens) Token(context.Context, uint) (string, error)   { return "fixture", nil }
func (selectedSourceTokens) PlatformToken(context.Context) (string, error) { return "fixture", nil }

func TestSelectedSourceUsesExactTenantItemAncestorsWithoutEnumeratingOrTrustingLocatorNames(t *testing.T) {
	for _, runtimeKind := range []string{"spark", "duckdb"} {
		for _, sourceKind := range []string{"postgresql", "mysql"} {
			t.Run(runtimeKind+"/"+sourceKind, func(t *testing.T) {
				registered, _ := plugin.Get(runtimeKind)
				provider := registered.(plugin.FederatedQueryRuntimeProvider)
				raw, err := dbbridge.GenerateCapabilities(sourceKind)
				if err != nil {
					t.Fatal(err)
				}
				caps := models.JSONString(raw)
				descriptor := models.EngineRuntimeDescriptor{ID: 12, Name: "Business Source", EngineType: sourceKind, LifecycleState: "active", ConnectionStatus: "online", Capabilities: &caps}
				namespaceType := "schema"
				if sourceKind == "mysql" {
					namespaceType = "database"
				}
				facts := models.MetaItemAncestors{
					Item:      models.MetaItem{ID: 7, EngineID: 12, TenantID: 9, NodeID: 3, ItemType: "table", Name: " 原.`名\" ", FullName: "misleading.display.path"},
					Ancestors: []models.MetaNode{{ID: 3, EngineID: 12, TenantID: 9, NodeType: namespaceType, Name: " 原.schema ", FullName: "namespace"}},
				}
				metaCalls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v1/system/runtime/engine-descriptors/12":
						_ = json.NewEncoder(w).Encode(descriptor)
					case "/api/v1/meta/items/7/ancestors":
						metaCalls++
						_ = json.NewEncoder(w).Encode(facts)
					default:
						t.Errorf("unexpected enumeration or fallback: %s", r.URL.Path)
						http.NotFound(w, r)
					}
				}))
				defer server.Close()
				catalog := federatedquery.NewCatalog(client.NewSystemServiceClient(server.URL, selectedSourceTokens{}, server.Client()), client.NewMetaClient(server.URL, selectedSourceTokens{}))
				locator, _ := resourcetree.ParseURI("addp://engine/12/path/forged/table?type=table&item_id=7")
				sources, err := catalog.SourceForItem(t.Context(), 9, 90, provider, locator)
				if err != nil || len(sources) != 1 || len(sources[0].Tables) != 1 || sources[0].Tables[0].Table != facts.Item.Name || sources[0].Tables[0].Schema != facts.Ancestors[0].Name {
					t.Fatalf("sources=%+v error=%v", sources, err)
				}
				quote := registered.Capabilities().Compute.Query.IdentifierQuotes["sql"]
				candidates, err := federatedquery.Candidates(sources, 10, quote)
				if err != nil || len(candidates) != 1 || !strings.Contains(candidates[0].Query, quote+strings.ReplaceAll(facts.Item.Name, quote, quote+quote)+quote) {
					t.Fatalf("candidates=%+v error=%v", candidates, err)
				}
				original, _ := json.Marshal(facts)
				for _, mutate := range []func(){
					func() { facts.Item.TenantID = 10 }, func() { facts.Item.EngineID = 13 }, func() { facts.Item.ID = 8 },
					func() { facts.Item.NodeID = 99 }, func() { facts.Ancestors[0].TenantID = 10 },
					func() { parent := uint(99); facts.Ancestors[0].ParentNodeID = &parent },
					func() { facts.Ancestors = append(facts.Ancestors, facts.Ancestors[0]) },
				} {
					_ = json.Unmarshal(original, &facts)
					mutate()
					if _, err := catalog.SourceForItem(t.Context(), 9, 90, provider, locator); !errors.Is(err, federatedquery.ErrSourceItemInvalid) {
						t.Fatalf("invalid Owner facts accepted: %v", err)
					}
				}
				_ = json.Unmarshal(original, &facts)
				if runtimeKind == "spark" {
					before := metaCalls
					descriptor.EngineType = "minio"
					rawMinio, _ := dbbridge.GenerateCapabilities("minio")
					minioCaps := models.JSONString(rawMinio)
					descriptor.Capabilities = &minioCaps
					if _, err := catalog.SourceForItem(t.Context(), 9, 90, provider, locator); !errors.Is(err, federatedquery.ErrSourceItemInvalid) || metaCalls != before {
						t.Fatal("unsupported source reached Meta")
					}
					descriptor.EngineType = sourceKind
					descriptor.Capabilities = &caps
				}
				before := metaCalls
				descriptor.LifecycleState = "disabled"
				if _, err := catalog.SourceForItem(t.Context(), 9, 90, provider, locator); !errors.Is(err, federatedquery.ErrSourceItemInvalid) || metaCalls != before {
					t.Fatal("disabled source reached Meta")
				}
				locator.ItemID = nil
				if _, err := catalog.SourceForItem(t.Context(), 9, 90, provider, locator); !errors.Is(err, federatedquery.ErrSourceItemInvalid) {
					t.Fatal("branch accepted as item")
				}
			})
		}
	}
}
