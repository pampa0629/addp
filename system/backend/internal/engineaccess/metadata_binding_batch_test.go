package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"gorm.io/gorm"
)

type metadataBatchReaderFunc func(context.Context, uint, []string) (*authorizationfacts.AuthorizationFactsBatch, error)

func (f metadataBatchReaderFunc) ReadAuthorizationFacts(ctx context.Context, tenant uint, fps []string) (*authorizationfacts.AuthorizationFactsBatch, error) {
	return f(ctx, tenant, fps)
}

func TestMetadataFactQueriesUseFourOwnerHTTPCallsForTwoHundredItems(t *testing.T) {
	for _, absentRoute := range []bool{false, true} {
		t.Run(fmt.Sprint(absentRoute), func(t *testing.T) {
			calls := 0
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/meta/runtime/data-items/authorization-facts/query" || r.Header.Get("Authorization") != "Bearer system-tenant-seven" || r.Header.Get("X-Tenant-ID") != "" {
					t.Error("wrong owner contract or credential")
				}
				if absentRoute {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				var query authorizationfacts.AuthorizationFactsQuery
				if json.NewDecoder(r.Body).Decode(&query) != nil || query.Validate() != nil {
					t.Fatal("invalid owner query")
				}
				batch := authorizationfacts.AuthorizationFactsBatch{}
				for _, fp := range query.Fingerprints {
					batch.Data = append(batch.Data, authorizationfacts.AuthorizationFactsResult{ItemFingerprint: fp})
				}
				_ = json.NewEncoder(w).Encode(batch)
			}))
			defer owner.Close()
			tokens := client.ServiceTokenProviderFunc(func(ctx context.Context, tenant uint) (string, error) {
				if tenant != 7 {
					t.Error("wrong tenant")
				}
				return "system-tenant-seven", nil
			})
			svc := NewService(nil, nil).WithAuthorizationFactsReader(client.NewMetaClient(owner.URL, tokens))
			paths := make([]plugin.EngineCatalogPath, 200)
			for i := range paths {
				paths[i] = plugin.TabularItemPath(9, "schema", "public", fmt.Sprint(i))
			}
			ctx, timing := ObserveManagerSearchTiming(t.Context())
			bindings, err := svc.resolveMetadataBindings(ctx, 7, paths)
			if absentRoute {
				if calls != 1 || !errors.Is(err, ErrAuthorizationFactsUnavailable) || bindings != nil {
					t.Fatal("missing owner route became absent items", err)
				}
			} else if err != nil || len(bindings) != 200 || calls != 4 {
				t.Fatalf("RPC budget violated: calls=%d err=%v", calls, err)
			}
			if absentRoute {
				if timing.metaClientCalls != 0 {
					t.Fatal("failed owner call became a successful client timing")
				}
			} else {
				stages := timing.metaClient.Preparation + timing.metaClient.HTTP + timing.metaClient.ResponseRead + timing.metaClient.StrictJSON + timing.metaClient.Decode + timing.metaClient.Validation
				if timing.metaClientCalls != calls || stages <= 0 || timing.metaClient.Total < stages || timing.metaFacts < timing.metaClient.Total {
					t.Fatalf("client timing escaped the existing Meta facts interval: calls=%d client=%+v facts=%v", timing.metaClientCalls, timing.metaClient, timing.metaFacts)
				}
			}
		})
	}
}

func TestMetadataFactBatchDedupStillProvesEachTypedTarget(t *testing.T) {
	actual := plugin.TabularItemPath(2, "schema", "a.b", "c")
	collision := plugin.TabularItemPath(2, "schema", "a", "b.c")
	facts := factstest.Single(actual)
	calls := 0
	svc := NewService(NewRepository(productionBindingDB(t)), nil).WithAuthorizationFactsReader(metadataBatchReaderFunc(func(ctx context.Context, tenant uint, fps []string) (*authorizationfacts.AuthorizationFactsBatch, error) {
		calls++
		if tenant != 7 || len(fps) != 1 || fps[0] != facts.ItemFingerprint {
			t.Fatal("wrong exact owner query")
		}
		return &authorizationfacts.AuthorizationFactsBatch{Data: []authorizationfacts.AuthorizationFactsResult{{ItemFingerprint: fps[0], Facts: &facts}}}, nil
	}))
	bindings, err := svc.resolveMetadataBindings(t.Context(), 7, []plugin.EngineCatalogPath{actual, collision})
	if err != nil || calls != 1 || len(bindings) != 2 || bindings[0].ItemFingerprint == "" || bindings[1].ItemFingerprint != "" {
		t.Fatalf("fingerprint sharing bypassed typed target proof: %+v %v", bindings, err)
	}
}

func TestMetadataVisibilityUsesBoundedFactQueriesAndNoPartialFailure(t *testing.T) {
	paths := make([]plugin.EngineCatalogPath, 200)
	const limit = authorizationfacts.MaxAuthorizationFactsQueryItems
	wantCalls := (len(paths) + limit - 1) / limit
	for i := range paths {
		paths[i] = plugin.TabularItemPath(9, "schema", "public", fmt.Sprintf("item_%03d", i))
	}
	for _, mode := range []string{"absent", "last_batch_error", "last_batch_reordered", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			svc := NewService(nil, nil).WithAuthorizationFactsReader(metadataBatchReaderFunc(func(c context.Context, tenant uint, fps []string) (*authorizationfacts.AuthorizationFactsBatch, error) {
				calls++
				if tenant != 7 || len(fps) == 0 || len(fps) > limit {
					t.Fatal("wrong owner scope or unbounded query")
				}
				if calls == wantCalls && mode == "last_batch_error" {
					return nil, errors.New("private owner failure")
				}
				batch := &authorizationfacts.AuthorizationFactsBatch{}
				for _, fp := range fps {
					batch.Data = append(batch.Data, authorizationfacts.AuthorizationFactsResult{ItemFingerprint: fp})
				}
				if calls == wantCalls && mode == "last_batch_reordered" {
					if len(batch.Data) > 1 {
						batch.Data[0], batch.Data[1] = batch.Data[1], batch.Data[0]
					} else {
						batch.Data = nil
					}
				}
				if mode == "cancel" {
					cancel()
				}
				return batch, nil
			}))
			bindings, err := svc.resolveMetadataBindings(ctx, 7, paths)
			if mode == "absent" {
				if err != nil || len(bindings) != len(paths) || calls != wantCalls {
					t.Fatalf("one RPC per item returned: calls=%d err=%v", calls, err)
				}
				for _, b := range bindings {
					if b.ItemFingerprint != "" {
						t.Fatal("absent item became visible")
					}
				}
			} else if err == nil || bindings != nil || (mode == "cancel" && calls != 1) {
				t.Fatalf("failure returned partial facts: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestMetadataBindingReadsEngineModelOncePerRequestWithoutCachingPermit(t *testing.T) {
	db := productionBindingDB(t)
	reads := 0
	if err := db.Callback().Query().After("gorm:query").Register("test:count-engine-model", func(tx *gorm.DB) {
		if tx.Statement.Table == "system.engines" || tx.Statement.Table == "engines" {
			reads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("test:count-engine-model")
	paths := make([]plugin.EngineCatalogPath, 200)
	facts := map[string]authorizationfacts.AuthorizationFacts{}
	for i := range paths {
		paths[i] = plugin.TabularItemPath(2, "schema", "public", fmt.Sprint(i))
		f := factstest.Single(paths[i])
		facts[f.ItemFingerprint] = f
	}
	svc := NewService(NewRepository(db), nil).WithAuthorizationFactsReader(metadataBatchReaderFunc(func(ctx context.Context, tenant uint, fps []string) (*authorizationfacts.AuthorizationFactsBatch, error) {
		batch := &authorizationfacts.AuthorizationFactsBatch{}
		for _, fp := range fps {
			f := facts[fp]
			batch.Data = append(batch.Data, authorizationfacts.AuthorizationFactsResult{ItemFingerprint: fp, Facts: &f})
		}
		return batch, nil
	}))
	bindings, err := svc.resolveMetadataBindings(t.Context(), 7, paths)
	if err != nil || len(bindings) != 200 {
		t.Fatal(err)
	}
	for _, b := range bindings {
		if b.ItemFingerprint == "" {
			t.Fatal("lost proven target")
		}
	}
	if reads != 1 {
		t.Fatalf("engine model queried %d times for one request", reads)
	}
	if err := db.Table("system.engines").Where("tenant_id=7 AND id=2").Update("engine_type", "nfs").Error; err != nil {
		t.Fatal(err)
	}
	bindings, err = svc.resolveMetadataBindings(t.Context(), 7, paths[:1])
	if err != nil || len(bindings) != 1 || bindings[0].ItemFingerprint != "" || reads != 2 {
		t.Fatal("a previous request's engine model or proof survived", err, reads)
	}
}
