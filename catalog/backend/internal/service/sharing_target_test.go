package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

type sharingTargetTestTokens struct{ t *testing.T }

func (s sharingTargetTestTokens) Token(_ context.Context, tenantID uint) (string, error) {
	if tenantID != 7 {
		s.t.Errorf("unexpected tenant %d", tenantID)
	}
	return "tenant-service-token", nil
}

func (s sharingTargetTestTokens) PlatformToken(context.Context) (string, error) {
	s.t.Error("table sharing must not use a platform token")
	return "", errors.New("platform token forbidden")
}

func TestSharingTargetResolverUsesOnlyTenantScopedMetadataReads(t *testing.T) {
	for _, scenario := range []string{"valid", "meta_offline", "foreign_item", "wrong_fingerprint", "system_offline", "disabled_engine", "wrong_engine"} {
		t.Run(scenario, func(t *testing.T) {
			rootID := uint(10)
			facts := commonModels.MetaItemAncestors{
				Item: commonModels.MetaItem{ID: 21, TenantID: 7, EngineID: 12, NodeID: 11, ItemType: "table", Name: "orders.v2/a", FullName: "team.space.orders.v2/a"},
				Ancestors: []commonModels.MetaNode{
					{ID: rootID, TenantID: 7, EngineID: 12, NodeType: "server"},
					{ID: 11, ParentNodeID: &rootID, TenantID: 7, EngineID: 12, NodeType: "schema", Name: "team.space"},
				},
			}
			fingerprint := commonModels.GenerateItemFingerprint(12, facts.Item.FullName)
			facts.Item.Fingerprint = fingerprint
			model := plugin.TabularCatalogModel("schema")
			caps, err := json.Marshal(plugin.EngineCapabilities{SchemaVersion: plugin.CapabilitiesSchemaVersion, EngineType: "postgresql", Storage: &plugin.StorageCapabilities{CatalogModel: &model}})
			if err != nil {
				t.Fatal(err)
			}
			capabilities := commonModels.JSONString(caps)
			descriptor := commonModels.EngineRuntimeDescriptor{ID: 12, LifecycleState: "active", Capabilities: &capabilities}
			switch scenario {
			case "foreign_item":
				facts.Item.TenantID++
			case "wrong_fingerprint":
				facts.Item.Fingerprint = "changed"
			case "disabled_engine":
				descriptor.LifecycleState = "disabled"
			case "wrong_engine":
				descriptor.ID++
			}
			metaCalls, systemCalls := 0, 0
			metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				metaCalls++
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/meta/items/21/ancestors" || r.Header.Get("Authorization") != "Bearer tenant-service-token" {
					t.Errorf("unexpected Meta request %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if scenario == "meta_offline" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_ = json.NewEncoder(w).Encode(facts)
			}))
			defer metaServer.Close()
			systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				systemCalls++
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/system/runtime/engine-descriptors/12" || r.Header.Get("Authorization") != "Bearer tenant-service-token" {
					t.Errorf("unexpected System request %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if scenario == "system_offline" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_ = json.NewEncoder(w).Encode(descriptor)
			}))
			defer systemServer.Close()
			tokens := sharingTargetTestTokens{t}
			resolver := NewSharingTargetResolver(commonClient.NewMetaClient(metaServer.URL, tokens), commonClient.NewSystemServiceClient(systemServer.URL, tokens, systemServer.Client()))
			path, err := resolver.ResolveSharingTarget(context.Background(), 7, 21, fingerprint)
			wantSystemCalls := 1
			if scenario == "meta_offline" || scenario == "foreign_item" || scenario == "wrong_fingerprint" {
				wantSystemCalls = 0
			}
			if metaCalls != 1 || systemCalls != wantSystemCalls {
				t.Fatalf("unexpected remote reads: meta=%d system=%d", metaCalls, systemCalls)
			}
			if scenario == "valid" {
				if err != nil || path.EngineID != 12 || len(path.Segments) != 3 || path.Segments[1].Name != "team.space" || path.Segments[2].Name != "orders.v2/a" {
					t.Fatalf("resolved path=%+v err=%v", path, err)
				}
				return
			}
			want := ErrSharingTargetUnsupported
			if scenario == "meta_offline" || scenario == "system_offline" {
				want = ErrReferenceValidationUnavailable
			}
			if !errors.Is(err, want) {
				t.Fatalf("err=%v want=%v", err, want)
			}
		})
	}
}
