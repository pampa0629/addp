package planner

import (
	"strings"
	"testing"

	commonmodels "github.com/addp/common/models"
)

func TestSystemEngineResolverResolveEngine(t *testing.T) {
	client := fakeSystemEngineClient{
		engines: map[uint]*commonmodels.Engine{
			7: {
				ID:             7,
				EngineType:     "postgresql",
				ConnectionInfo: commonmodels.ConnectionInfo{"host": "localhost", "database": "gis"},
				LifecycleState: "active",
			},
		},
	}

	binding, err := NewSystemEngineResolver(client).ResolveEngine(EngineRef{ID: 7, Type: "postgresql"})
	if err != nil {
		t.Fatalf("ResolveEngine failed: %v", err)
	}
	if binding.Type != "postgresql" || binding.EngineID != 7 {
		t.Fatalf("binding = %#v, want postgresql engine 7", binding)
	}
	if binding.ConnInfo["database"] != "gis" {
		t.Fatalf("database = %#v, want gis", binding.ConnInfo["database"])
	}
}

func TestSystemEngineResolverRejectsTypeMismatch(t *testing.T) {
	client := fakeSystemEngineClient{
		engines: map[uint]*commonmodels.Engine{
			7: {ID: 7, EngineType: "postgresql", LifecycleState: "active"},
		},
	}

	_, err := NewSystemEngineResolver(client).ResolveEngine(EngineRef{ID: 7, Type: "mysql"})
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

	_, err := NewSystemEngineResolver(client).ResolveEngine(EngineRef{ID: 7})
	if err == nil {
		t.Fatal("ResolveEngine succeeded, want inactive error")
	}
	if !strings.Contains(err.Error(), "inactive") {
		t.Fatalf("error = %q, want inactive", err)
	}
}

type fakeSystemEngineClient struct {
	engines map[uint]*commonmodels.Engine
}

func (c fakeSystemEngineClient) GetEngine(engineID uint) (*commonmodels.Engine, error) {
	return c.engines[engineID], nil
}
