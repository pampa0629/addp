package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"fmt"
	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonAuth "github.com/addp/common/middleware/auth"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

// Uses the actual Meta handlers, delegate policy and unified System client.
// Cryptographic delegate validation and current IAM rules are covered by System PG T2.
func TestPGMetadataHandlersForwardOriginalCredentialAndFilter(t *testing.T) {
	for _, scope := range []string{"ordinary", "resource.children.list", "resource.ancestors.get"} {
		t.Run(scope, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			tenant := uint(7)
			credential := "addp_at_viewer"
			if scope != "ordinary" {
				credential = "addp_dat_viewer"
			}
			allow, unavailable, checks := true, false, 0
			caps := commonModels.JSONString(`{"schema_version":"engine.capabilities/v1","engine_type":"postgresql","engine_family":"tabular","storage":{"store":{}}}`)
			engine := commonModels.Engine{ID: 9, TenantID: &tenant, Name: "PG", EngineType: "postgresql", LifecycleState: "active", Capabilities: &caps}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/system/oauth/token":
					json.NewEncoder(w).Encode(map[string]any{"access_token": "addp_at_meta_runtime", "token_type": "bearer", "expires_in": 300, "scope": "addp.api"})
				case "/api/v1/system/engines":
					json.NewEncoder(w).Encode([]commonModels.Engine{engine})
				case "/api/v1/system/engines/9":
					if r.Header.Get("Authorization") != "Bearer addp_at_meta_runtime" {
						t.Errorf("engine facts used wrong identity")
					}
					json.NewEncoder(w).Encode(engine)
				case "/api/v1/system/engine-access/metadata-checks/meta-query":
					checks++
					if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+credential || r.Header.Get("X-Tenant-ID") != "" {
						t.Errorf("user check substituted identity: %s %s", r.Method, r.Header.Get("Authorization"))
					}
					if unavailable {
						w.WriteHeader(503)
						w.Write([]byte(`{"error":"unavailable"}`))
						return
					}
					var query authorization.SourceMetadataCheckRequest
					if json.NewDecoder(r.Body).Decode(&query) != nil || !query.Valid() || (len(query.Targets) != 1 && len(query.Targets) != 2) {
						t.Errorf("invalid candidate query: %+v", query)
						w.WriteHeader(400)
						return
					}
					result := authorization.SourceMetadataCheck{ObservedAt: time.Now()}
					for _, target := range query.Targets {
						result.Targets = append(result.Targets, authorization.SourceMetadataTargetCheck{Target: target, Visible: allow && target.Segments[len(target.Segments)-1].Name == "visible"})
					}
					json.NewEncoder(w).Encode(result)
				default:
					t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer upstream.Close()
			tokenSource, err := commonClient.NewOAuthServiceTokenSource(upstream.URL, "addp-meta", "meta-resource-tree-test-secret-32-bytes", upstream.Client())
			if err != nil {
				t.Fatal(err)
			}
			client := commonClient.NewSystemServiceClient(upstream.URL, tokenSource, upstream.Client())
			db := metatest.OpenMetadataDB(t)
			root := createResourceTreeHandlerNode(t, db, models.MetaNode{TenantID: tenant, EngineID: 9, NodeType: "server", Name: "PG", FullName: "", Depth: 1})
			branch := createResourceTreeHandlerNode(t, db, models.MetaNode{TenantID: tenant, EngineID: 9, ParentNodeID: &root.ID, NodeType: "schema", Name: "public", FullName: "public", Depth: 2})
			hiddenBranch := createResourceTreeHandlerNode(t, db, models.MetaNode{TenantID: tenant, EngineID: 9, ParentNodeID: &root.ID, NodeType: "schema", Name: "hidden_schema", FullName: "hidden_schema", Depth: 2})
			var visible models.MetaItem
			for _, name := range []string{"visible", "hidden"} {
				item := models.MetaItem{TenantID: tenant, EngineID: 9, NodeID: branch.ID, ItemType: "table", Name: name, FullName: "public." + name, Fingerprint: name + "-fp"}
				if name == "hidden" {
					item.NodeID = hiddenBranch.ID
					item.FullName = "hidden_schema.hidden"
				}
				rowCount := int64(777)
				item.RowCount = &rowCount
				item.SizeBytes = &rowCount
				item.Attributes = models.JSONMap{
					"item":         map[string]interface{}{"data_type": "table", "format": "postgresql", "layout": "single", "sample": "private"},
					"storage":      map[string]interface{}{"physical_path": "private"},
					"type_info":    map[string]interface{}{"table": datatype.TableInfoPayload(&datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "struct_field", Type: datatype.FieldTypeInt, NativeType: "int4", PrimaryKey: true, Comment: "private", DefaultExpression: "private", GenerationExpression: "private"}}, PrimaryKey: []string{"struct_field"}, RowCount: &rowCount, Native: map[string]interface{}{"sample": "private"}})},
					"capabilities": map[string]interface{}{"statistics": map[string]interface{}{"sample": "private"}, "spatial": map[string]interface{}{"srid": 4326, "extent": []float64{1, 2, 3, 4}, "capture_location": map[string]interface{}{"latitude": 1, "longitude": 2}}},
				}
				if err := db.Create(&item).Error; err != nil {
					t.Fatal(err)
				}
				if name == "visible" {
					visible = item
				}
			}
			handler := NewHandler(service.NewEngineService(db, client), nil, nil, nil, service.NewMetadataQueryService(db), nil)
			handler.metadataVisibility = client
			router := gin.New()
			installResourceTreeTenantContext(t, router, tenant)
			router.Use(func(c *gin.Context) {
				current, _ := commonAuth.AuthContextFromGin(c)
				current.Authorization.RoleAssignments = []authorization.RoleAssignment{{AssignmentID: "1", RoleKey: "tenant.meta_viewer", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: authorization.AssignmentScope{Type: "tenant", TenantID: current.Context.TenantID}, Permissions: []string{"meta.catalog.read"}}}
				if scope != "ordinary" {
					current.Token.Type = "delegated_access_token"
					current.Client.Audiences = []string{"meta"}
					current.Client.ScopeMode = "restricted"
					current.Client.Scopes = []string{scope}
					current.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *current.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
				}
				if err := commonAuth.SetAuthContextForGin(c, current); err != nil {
					t.Fatal(err)
				}
				c.Next()
			})
			group := router.Group("/api/v1/meta", commonAuth.MustNewDelegatedPolicyGuard("meta", metaDelegatedToolPolicies()), commonAuth.MustNewPermissionGuard("meta.catalog.read"))
			group.GET("/stats", handler.GetStats)
			group.GET("/engines", handler.GetEngines)
			group.GET("/resource-tree/:engine_id", handler.GetResourceTree)
			group.GET("/resource-tree/:engine_id/node", handler.GetResourceTreeNode)
			group.GET("/resource-tree/:engine_id/ancestors", handler.GetResourceTreeAncestors)
			group.GET("/resource-tree/:engine_id/search", handler.SearchResourceTree)
			group.GET("/engines/:engine_id/items", handler.ListEngineItems)
			group.GET("/engines/:engine_id/tree", handler.GetMetadataTree)
			group.GET("/items/by-catalog-path", handler.QueryItemByCatalogPath)
			group.GET("/items/:item_id", handler.GetItemByID)
			group.GET("/items/:item_id/fields", handler.GetItemFieldsByID)
			group.GET("/items/:item_id/spatial", handler.GetItemSpatialMetadataByID)
			group.GET("/items/:item_id/ancestors", handler.GetItemAncestors)
			group.GET("/nodes/by-catalog-path", handler.QueryNodeByCatalogPath)
			group.GET("/nodes/:node_id", handler.GetMetaNodeByID)
			group.GET("/nodes/:node_id/items", handler.GetNodeItems)
			group.GET("/nodes/:node_id/children", handler.GetNodeChildren)
			group.GET("/nodes/:node_id/ancestors", handler.GetNodeAncestors)

			path := "/api/v1/meta/resource-tree/9/node?locator=" + url.QueryEscape((&resourcetree.ResourceLocator{EngineID: 9, Path: []string{"public"}, Type: resourcetree.TypeSchema, NodeID: &branch.ID}).ToURI())
			if scope == "resource.ancestors.get" {
				path = "/api/v1/meta/resource-tree/9/ancestors?locator=" + url.QueryEscape((&resourcetree.ResourceLocator{EngineID: 9, Path: []string{"public", "visible"}, Type: resourcetree.TypeTable, ItemID: &visible.ID}).ToURI())
			}
			get := func(path string) *httptest.ResponseRecorder {
				response := httptest.NewRecorder()
				request := httptest.NewRequest("GET", path, nil)
				request.Header.Set("Authorization", "Bearer "+credential)
				router.ServeHTTP(response, request)
				return response
			}
			response := get(path)
			if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), `"label":"visible"`) || strings.Contains(response.Body.String(), "hidden") || checks != 1 {
				t.Fatalf("visible projection: %d %s checks=%d", response.Code, response.Body, checks)
			}
			allow = false
			response = get(path)
			if response.Code != 404 || strings.Contains(response.Body.String(), "hidden") || checks != 2 {
				t.Fatalf("revocation: %d %s checks=%d", response.Code, response.Body, checks)
			}
			allow = true
			unavailable = true
			response = get(path)
			if response.Code != 503 || strings.Contains(response.Body.String(), `"label"`) || checks != 3 {
				t.Fatalf("upstream failure leaked data: %d %s", response.Code, response.Body)
			}
			if scope == "ordinary" {
				unavailable = false
				allow = true
				routes := []string{
					"/stats", "/engines",
					"/engines/9/items", "/engines/9/items?branch=public", "/engines/9/tree",
					"/items/by-catalog-path?engine_id=9&catalog_path=public.visible",
					fmt.Sprintf("/items/%d", visible.ID), fmt.Sprintf("/items/%d/fields", visible.ID), fmt.Sprintf("/items/%d/spatial", visible.ID), fmt.Sprintf("/items/%d/ancestors", visible.ID),
					fmt.Sprintf("/nodes/%d", branch.ID), fmt.Sprintf("/nodes/%d/items", branch.ID), fmt.Sprintf("/nodes/%d/children", root.ID), fmt.Sprintf("/nodes/%d/ancestors", branch.ID),
					"/nodes/by-catalog-path?engine_id=9&catalog_path=public",
				}
				for _, route := range routes {
					response = get("/api/v1/meta" + route)
					if response.Code != 200 {
						t.Fatalf("%s status=%d %s", route, response.Code, response.Body)
					}
					for _, forbidden := range []string{"hidden", "private", `"row_count"`, `"size_bytes"`, `"total_size_bytes"`, `"extent"`, `"capture_location"`} {
						if strings.Contains(response.Body.String(), forbidden) {
							t.Fatalf("%s leaked %s: %s", route, forbidden, response.Body)
						}
					}
					if response.Header().Get("Cache-Control") != "no-store" {
						t.Fatalf("%s missing no-store", route)
					}
				}
				response = get(fmt.Sprintf("/api/v1/meta/items/%d/fields", visible.ID))
				if !strings.Contains(response.Body.String(), "struct_field") || !strings.Contains(response.Body.String(), "int4") || !strings.Contains(response.Body.String(), `"primary_key":true`) {
					t.Fatalf("field structure lost: %s", response.Body)
				}
				response = get(fmt.Sprintf("/api/v1/meta/items/%d/spatial", visible.ID))
				if !strings.Contains(response.Body.String(), `"srid":4326`) {
					t.Fatalf("CRS lost: %s", response.Body)
				}
				response = get("/api/v1/meta/stats")
				if response.Body.String() != `{"total":1}` {
					t.Fatalf("unfiltered count: %s", response.Body)
				}
				response = get("/api/v1/meta/engines")
				if !strings.Contains(response.Body.String(), `"total_catalog_nodes":1`) {
					t.Fatalf("engine stats: %s", response.Body)
				}
				allow = false
				response = get("/api/v1/meta/stats")
				if response.Body.String() != `{"total":0}` {
					t.Fatalf("revoked count: %s", response.Body)
				}
				response = get("/api/v1/meta/engines")
				if !strings.Contains(response.Body.String(), `"total_catalog_nodes":0`) {
					t.Fatalf("revoked engine stats: %s", response.Body)
				}
				hidden := get("/api/v1/meta/nodes/by-catalog-path?engine_id=9&catalog_path=public")
				absent := get("/api/v1/meta/nodes/by-catalog-path?engine_id=9&catalog_path=not_present")
				if hidden.Code != 404 || absent.Code != hidden.Code || absent.Body.String() != hidden.Body.String() {
					t.Fatalf("path existence leak: %d %s / %d %s", hidden.Code, hidden.Body, absent.Code, absent.Body)
				}
				for _, route := range routes {
					response = get("/api/v1/meta" + route)
					list := route == "/stats" || route == "/engines" || (strings.HasPrefix(route, "/engines/") && !strings.Contains(route, "?branch=")) || strings.HasSuffix(route, "/children")
					want := 404
					if list {
						want = 200
					}
					if response.Code != want || strings.Contains(response.Body.String(), "struct_field") || strings.Contains(response.Body.String(), "private") || strings.Contains(response.Body.String(), "public.visible") {
						t.Fatalf("%s revocation=%d want=%d %s", route, response.Code, want, response.Body)
					}
				}
				allow, unavailable = true, true
				for _, route := range routes {
					response = get("/api/v1/meta" + route)
					if response.Code != 503 || strings.Contains(response.Body.String(), "struct_field") || strings.Contains(response.Body.String(), "public") || strings.Contains(response.Body.String(), `"total"`) {
						t.Fatalf("%s owner unavailable: %d %s", route, response.Code, response.Body)
					}
				}

			}
			if scope != "ordinary" {
				response = get("/api/v1/meta/resource-tree/9/search?q=visible")
				if response.Code != 403 || checks != 3 {
					t.Fatalf("delegate escaped route scope: %d checks=%d", response.Code, checks)
				}
			}
		})
	}
}
