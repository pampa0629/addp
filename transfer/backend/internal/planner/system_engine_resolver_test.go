package planner

import (
	"context"
	"strings"
	"testing"

	commonmodels "github.com/addp/common/models"
)

func TestSystemEngineResolverResolveEngine(t *testing.T) {
	client := fakeSystemEngineClient{
		engines: map[uint]*commonmodels.Engine{
			7: {
				ID:               7,
				Version:          41,
				EngineType:       "postgresql",
				ConnectionInfo:   commonmodels.ConnectionInfo{"host": "localhost", "database": "gis"},
				LifecycleState:   "active",
				ConnectionStatus: commonmodels.EngineConnectionOnline,
			},
		},
	}

	binding, err := BindEngineResolver(NewSystemEngineResolver(client), 3).ResolveEngine(EngineRef{ID: 7, Type: "postgresql"})
	if err != nil {
		t.Fatalf("ResolveEngine failed: %v", err)
	}
	if binding.Type != "postgresql" || binding.EngineID != 7 || binding.EngineVersion != 41 {
		t.Fatalf("binding = %#v, want postgresql engine 7", binding)
	}
	if binding.ConnInfo["database"] != "gis" {
		t.Fatalf("database = %#v, want gis", binding.ConnInfo["database"])
	}
}

func TestSystemEngineResolverRejectsTypeMismatch(t *testing.T) {
	client := fakeSystemEngineClient{
		engines: map[uint]*commonmodels.Engine{
			7: {ID: 7, EngineType: "postgresql", LifecycleState: "active", ConnectionStatus: commonmodels.EngineConnectionOnline},
		},
	}

	_, err := BindEngineResolver(NewSystemEngineResolver(client), 3).ResolveEngine(EngineRef{ID: 7, Type: "mysql"})
	if err == nil {
		t.Fatal("ResolveEngine succeeded, want type mismatch error")
	}
	if !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("error = %q, want type mismatch", err)
	}
}

func TestSystemEngineResolverRejectsInactiveEngine(t *testing.T) {
	client := fakeSystemEngineClient{
		engines: map[uint]*commonmodels.Engine{
			7: {ID: 7, EngineType: "postgresql", LifecycleState: "disabled"},
		},
	}

	_, err := BindEngineResolver(NewSystemEngineResolver(client), 3).ResolveEngine(EngineRef{ID: 7})
	if err == nil {
		t.Fatal("ResolveEngine succeeded, want inactive error")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("error = %q, want unavailable", err)
	}
}

func TestSystemEngineResolverRejectsOfflineEngine(t *testing.T) {
	client := fakeSystemEngineClient{
		engines: map[uint]*commonmodels.Engine{
			7: {ID: 7, EngineType: "postgresql", LifecycleState: "active", ConnectionStatus: commonmodels.EngineConnectionOffline},
		},
	}

	_, err := BindEngineResolver(NewSystemEngineResolver(client), 3).ResolveEngine(EngineRef{ID: 7})
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("ResolveEngine() error = %v, want offline rejection", err)
	}
}

type fakeSystemEngineClient struct {
	engines map[uint]*commonmodels.Engine
}

func (c fakeSystemEngineClient) GetEngineForTenant(_ context.Context, _ uint, engineID uint) (*commonmodels.Engine, error) {
	return c.engines[engineID], nil
}

func TestAuthorizedEngineBindingPreservesWriteVersion(t *testing.T) {
	binding, err := EngineBindingFromEngine(&commonmodels.Engine{ID: 7, Version: 53, EngineType: "postgresql"})
	if err != nil || binding.EngineVersion != 53 {
		t.Fatalf("engine version lost: %+v %v", binding, err)
	}
}

func TestDescriptorEngineResolverUsesOnlyTaskValidationFacts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &commonmodels.EngineRuntimeDescriptor{
		ID: 7, Version: 41, EngineType: "postgresql", LifecycleState: "active", ConnectionStatus: "online",
		RuntimeEndpoint: &commonmodels.EngineRuntimeEndpoint{Host: "runtime.example"},
	}
	r := DescriptorEngineResolver{Context: ctx, TenantID: 3, Lookup: func(request context.Context, tenant, engine uint) (*commonmodels.EngineRuntimeDescriptor, error) {
		if request != ctx || tenant != 3 || engine != 7 {
			t.Fatal("incorrect lookup context, tenant or engine")
		}
		return d, nil
	}}
	b, err := r.ResolveEngine(EngineRef{ID: 7, Type: "postgresql"})
	if err != nil || b.EngineID != 7 || b.EngineVersion != 41 || b.Type != "postgresql" || b.ConnInfo != nil {
		t.Fatalf("unexpected task validation binding %+v, error %v", b, err)
	}
	if _, err := r.ResolveEngine(EngineRef{ID: 7, Type: "mysql"}); err == nil {
		t.Fatal("accepted mismatched engine type")
	}
	d.ID = 8
	if _, err := r.ResolveEngine(EngineRef{ID: 7}); err == nil {
		t.Fatal("accepted descriptor for another engine")
	}
}
