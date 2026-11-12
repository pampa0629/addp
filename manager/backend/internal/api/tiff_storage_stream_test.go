package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func apiTIFFRangeFacts(t *testing.T, layout string, refs []authorizationfacts.FileCompositionRef) *authorizationfacts.AuthorizationFacts {
	t.Helper()
	composition, err := authorizationfacts.NewScannedFileCompositionBoundary("tiff", layout, refs)
	if err != nil {
		t.Fatal(err)
	}
	primary, _ := composition.PrimaryTarget()
	facts := &authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: 9, ItemType: "file", FullName: primary.StringPath(), ItemFingerprint: commonModels.GenerateItemFingerprint(9, primary.StringPath()), BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now().UTC(), Boundary: &authorizationfacts.AuthorizationBoundary{Layout: layout, FileComposition: composition}}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		t.Fatal(err)
	}
	facts.BoundaryHash = &hash
	return facts
}

func apiTIFFRangeMetaClient(t *testing.T, itemJSON string, facts *authorizationfacts.AuthorizationFacts) *client.MetaClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/meta/items/1":
			_, _ = w.Write([]byte(itemJSON))
		case "/api/v1/meta/runtime/data-items/authorization-facts/query":
			if facts == nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			if r.Method != http.MethodPost {
				t.Error("facts query must use POST")
			}
			var query authorizationfacts.AuthorizationFactsQuery
			if err := json.NewDecoder(r.Body).Decode(&query); err != nil || !reflect.DeepEqual(query.Fingerprints, []string{facts.ItemFingerprint}) {
				t.Error("lost exact DataItem facts query", err)
			}
			_ = json.NewEncoder(w).Encode(authorizationfacts.AuthorizationFactsBatch{Data: []authorizationfacts.AuthorizationFactsResult{{ItemFingerprint: facts.ItemFingerprint, Facts: facts}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return client.NewMetaClient(server.URL, client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "test-token", nil }))
}

type apiTIFFStreamIO struct {
	*apiDownloadTestFilePlugin
	stats, opens int
}

func (p *apiTIFFStreamIO) DescribeEngineCatalogFacts(ctx context.Context, info plugin.ConnectionInfo, target plugin.EngineCatalogPath, options plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.stats++
	return p.apiDownloadTestFilePlugin.DescribeEngineCatalogFacts(ctx, info, target, options)
}
func (p *apiTIFFStreamIO) OpenContent(ctx context.Context, info plugin.ConnectionInfo, target plugin.EngineCatalogPath, options plugin.ReadOptions) (io.ReadCloser, error) {
	p.opens++
	return p.apiDownloadTestFilePlugin.OpenContent(ctx, info, target, options)
}

func TestNFSTIFFRangeUsesFormalWholeDataItemComposition(t *testing.T) {
	const primary = "images/source.tif"
	refs := []authorizationfacts.FileCompositionRef{{Target: plugin.FileItemPath(9, primary), Role: "image", Primary: true, Required: true}}
	for _, extra := range []struct{ name, role string }{{"source.hdr", "header"}, {"source.tfw", "world_file"}, {"source.prj", "crs"}, {"source.tif.aux.xml", "auxiliary_metadata"}, {"source.tif.ovr", "overview"}, {"source.tif.msk", "mask"}} {
		refs = append(refs, authorizationfacts.FileCompositionRef{Target: plugin.FileItemPath(9, "images/"+extra.name), Role: extra.role})
	}
	for _, scenario := range []string{"allow", "denied", "revoked before open", "missing boundary", "changed composition", "meta unavailable", "ticket", "no range", "unregistered sibling", "asset still denied", "download still denied"} {
		t.Run(scenario, func(t *testing.T) {
			files := map[string]string{}
			itemRefs := []map[string]interface{}{}
			for _, ref := range refs {
				name := ref.Target.StringPath()
				files[name] = "exact bytes: " + name
				itemRefs = append(itemRefs, map[string]interface{}{"path": name, "role": ref.Role, "primary": ref.Primary, "required": ref.Required})
			}
			previous, previousErr := plugin.Get("nfs")
			provider := &apiTIFFStreamIO{apiDownloadTestFilePlugin: newAPIDownloadTestFilePlugin("nfs", files)}
			plugin.Register(provider)
			t.Cleanup(func() {
				if previousErr == nil {
					plugin.Register(previous)
				} else {
					plugin.Unregister("nfs")
				}
			})
			item, _ := json.Marshal(map[string]interface{}{"id": 1, "tenant_id": 1, "engine_id": 9, "item_type": "file", "name": "source.tif", "full_name": primary, "attributes": map[string]interface{}{"item": map[string]interface{}{"format": "tiff", "data_type": "media", "layout": "multi", "refs": itemRefs}, "storage": map[string]interface{}{"physical_path": primary}}})
			facts := apiTIFFRangeFacts(t, "multi", refs)
			if scenario == "missing boundary" {
				facts.BasisState = authorizationfacts.AuthorizationBasisNeedsRefresh
				facts.Boundary = nil
				facts.BoundaryHash = nil
			}
			if scenario == "changed composition" {
				facts = apiTIFFRangeFacts(t, "multi", refs[:len(refs)-1])
			}
			metaFacts := facts
			if scenario == "meta unavailable" {
				metaFacts = nil
			}
			metadata := service.NewMetadataService(nil, apiDownloadTestSystemClient(t, 9, "nfs"), apiTIFFRangeMetaClient(t, string(item), metaFacts), nil, nil)
			checks := 0
			metadata.SetFileSourceReadChecker(apiModelReadChecker(func(_ context.Context, credential string, scope client.ManagerFileExpectedSource) error {
				checks++
				if credential != "addp_at_current_user" || scope.EngineVersion != "1" || scope.FileComposition.SourceLayout != "multi" || !reflect.DeepEqual(scope.FileComposition.Refs, facts.Boundary.FileComposition.Refs) {
					t.Fatal("did not consume complete formal Meta composition")
				}
				if scenario == "denied" || (scenario == "revoked before open" && checks == 2) {
					return client.ErrManagerPreviewReadDenied
				}
				return nil
			}))
			handler := NewExplorerHandler(nil, nil, metadata, apiDownloadProtectionGate{}, nil)
			router := gin.New()
			router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 1); c.Next() })
			router.GET("/storage-stream", handler.StorageStream)
			router.GET("/storage-assets/:engine_id/items/:item_id/*storage_ref", handler.StorageAsset)
			router.GET("/downloads/file", NewDownloadHandler(metadata, apiDownloadProtectionGate{}).DownloadFile)
			requested := []string{"images/source.tif.msk"}
			if scenario == "allow" {
				requested = nil
				for _, ref := range refs {
					requested = append(requested, ref.Target.StringPath())
				}
			}
			for _, name := range requested {
				checks = 0
				provider.stats = 0
				provider.opens = 0
				if scenario == "unregistered sibling" {
					name = "images/not-registered.msk"
				}
				url := "/storage-stream?locator=" + urlQueryEscape("addp://engine/9/path/"+primary+"?type=file&item_id=1") + "&storage_ref=" + urlQueryEscape(name)
				if scenario == "asset still denied" {
					url = "/storage-assets/9/items/1/" + name
				}
				if scenario == "download still denied" {
					url = "/downloads/file?locator=" + urlQueryEscape("addp://engine/9/path/"+primary+"?type=file&item_id=1")
				}
				req := httptest.NewRequest(http.MethodGet, url, nil)
				req.Header.Set("Range", "bytes=0-65536")
				req.Header.Set("Authorization", "Bearer addp_at_current_user")
				if scenario == "ticket" {
					req.Header.Set("Authorization", "Bearer addp_rat_ticket")
				}
				if scenario == "no range" {
					req.Header.Del("Range")
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				want := http.StatusForbidden
				switch scenario {
				case "allow":
					want = http.StatusPartialContent
				case "ticket":
					want = http.StatusUnauthorized
				case "unregistered sibling":
					want = http.StatusBadRequest
				case "meta unavailable":
					want = http.StatusServiceUnavailable
				}
				if response.Code != want {
					t.Fatalf("%s: status=%d want=%d body=%s", name, response.Code, want, response.Body.String())
				}
				if scenario == "allow" {
					if response.Body.String() != files[name] || checks != 2 || provider.stats != 1 || provider.opens != 1 {
						t.Fatalf("bytes/authority/IO mismatch: checks=%d stats=%d opens=%d body=%q", checks, provider.stats, provider.opens, response.Body.String())
					}
				} else if provider.opens != 0 || (scenario != "revoked before open" && provider.stats != 0) {
					t.Fatalf("denial touched source: stats=%d opens=%d", provider.stats, provider.opens)
				}
			}
		})
	}
}
