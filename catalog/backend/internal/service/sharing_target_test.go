package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

type sharingTargetTestTokens struct{ t *testing.T }

func (s sharingTargetTestTokens) Token(_ context.Context, tenantID uint) (string, error) {
	if tenantID != 7 {
		s.t.Errorf("unexpected tenant %d", tenantID)
	}
	return "catalog-tenant-service-token", nil
}

func (s sharingTargetTestTokens) PlatformToken(context.Context) (string, error) {
	s.t.Error("table sharing must not use a platform token")
	return "", errors.New("platform token forbidden")
}

func TestSharingTargetResolverUsesCanonicalAuthorizationFactsClient(t *testing.T) {
	for _, scenario := range []string{"valid", "missing", "meta_offline", "credential_rejected", "malformed",
		"wrong_fingerprint", "wrong_boundary", "incomplete", "needs_refresh", "unsupported",
		"system_offline", "disabled_engine", "wrong_engine", "missing_model", "wrong_model", "invalid_capabilities"} {
		t.Run(scenario, func(t *testing.T) {
			path := plugin.TabularItemPath(12, "schema", " team.space/一 ", " orders.v2/a ")
			facts := authorizationfacts.AuthorizationFacts{
				SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: 12,
				ItemType: "table", FullName: " team.space/一 . orders.v2/a ",
				BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now().UTC(),
				Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: path},
			}
			fingerprint := commonModels.GenerateItemFingerprint(12, facts.FullName)
			facts.ItemFingerprint = fingerprint
			hash, err := facts.ComputeBoundaryHash()
			if err != nil {
				t.Fatal(err)
			}
			facts.BoundaryHash = &hash
			model := plugin.TabularCatalogModel("schema")
			if scenario == "wrong_model" {
				model = plugin.TabularCatalogModel("database")
			}
			encodedCaps, err := json.Marshal(plugin.EngineCapabilities{SchemaVersion: plugin.CapabilitiesSchemaVersion, EngineType: "postgresql", Storage: &plugin.StorageCapabilities{CatalogModel: &model}})
			if err != nil {
				t.Fatal(err)
			}
			caps := commonModels.JSONString(encodedCaps)
			descriptor := commonModels.EngineRuntimeDescriptor{ID: 12, LifecycleState: "active", Capabilities: &caps}
			want, wantSystemCalls := ErrReferenceValidationUnavailable, 0
			switch scenario {
			case "valid":
				want, wantSystemCalls = nil, 1
			case "missing":
				want = ErrSharingTargetUnsupported
			case "wrong_fingerprint":
				facts.FullName = "outdoor.other"
				facts.ItemFingerprint = commonModels.GenerateItemFingerprint(12, facts.FullName)
				facts.Boundary.NativeTarget = plugin.TabularItemPath(12, "schema", "outdoor", "other")
				hash, err = facts.ComputeBoundaryHash()
				if err != nil {
					t.Fatal(err)
				}
				facts.BoundaryHash = &hash
			case "wrong_boundary":
				facts.Boundary.NativeTarget.Segments[2].Name = "another table"
			case "incomplete", "needs_refresh", "unsupported":
				facts.BasisState = authorizationfacts.AuthorizationBasisState(scenario)
				facts.Boundary, facts.BoundaryHash = nil, nil
				want = ErrSharingTargetUnsupported
			case "system_offline":
				wantSystemCalls = 1
			case "disabled_engine":
				descriptor.LifecycleState = "disabled"
				want, wantSystemCalls = ErrSharingTargetUnsupported, 1
			case "wrong_engine":
				descriptor.ID++
				want, wantSystemCalls = ErrSharingTargetUnsupported, 1
			case "missing_model", "wrong_model", "invalid_capabilities":
				want, wantSystemCalls = ErrSharingTargetUnsupported, 1
				if scenario == "missing_model" {
					descriptor.Capabilities = nil
				}
				if scenario == "invalid_capabilities" {
					caps = commonModels.JSONString("invalid")
				}
			}
			metaCalls, systemCalls := 0, 0
			metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				metaCalls++
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/meta/runtime/data-items/"+fingerprint+"/authorization-facts" ||
					r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer catalog-tenant-service-token" ||
					r.Header.Get("X-Tenant-ID") != "" || r.Header.Get("X-Internal-API-Key") != "" {
					t.Error("unexpected Meta facts request")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				switch scenario {
				case "meta_offline":
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				case "missing":
					w.WriteHeader(http.StatusNotFound)
					return
				case "credential_rejected":
					w.WriteHeader(http.StatusForbidden)
					return
				case "malformed":
					_, _ = w.Write([]byte("{}"))
					return
				}
				_ = json.NewEncoder(w).Encode(facts)
			}))
			defer metaServer.Close()
			systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				systemCalls++
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/system/runtime/engine-descriptors/12" ||
					r.Header.Get("Authorization") != "Bearer catalog-tenant-service-token" {
					t.Error("unexpected System descriptor request")
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
			resolver := NewSharingTargetResolver(commonClient.NewMetaClient(metaServer.URL, tokens),
				commonClient.NewSystemServiceClient(systemServer.URL, tokens, systemServer.Client()))
			result, err := resolver.ResolveSharingTarget(context.Background(), 7, fingerprint)
			if metaCalls != 1 || systemCalls != wantSystemCalls {
				t.Fatalf("remote reads: meta=%d system=%d", metaCalls, systemCalls)
			}
			if want == nil {
				if err != nil || result.EngineID != 12 || len(result.Segments) != 3 ||
					result.Segments[1].Name != " team.space/一 " || result.Segments[2].Name != " orders.v2/a " {
					t.Fatalf("resolved path=%+v err=%v", result, err)
				}
			} else if !errors.Is(err, want) || result.EngineID != 0 {
				t.Fatalf("err=%v want=%v path=%+v", err, want, result)
			}
		})
	}
}
