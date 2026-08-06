package instanceprovider

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

type descriptorClient struct {
	descriptor *models.EngineRuntimeDescriptor
}

func (c descriptorClient) GetEngineRuntimeDescriptor(context.Context, uint) (*models.EngineRuntimeDescriptor, error) {
	return c.descriptor, nil
}

func TestResolveRejectsSDXPostgreSQLWithoutBoundRuntime(t *testing.T) {
	capabilities := plugin.EngineCapabilities{
		SchemaVersion: plugin.CapabilitiesSchemaVersion,
		EngineType:    "postgresql",
		Storage:       &plugin.StorageCapabilities{Store: &plugin.StoreCapability{}},
	}
	plugin.SetSpatialWorkspacesExtension(&capabilities, []plugin.SpatialWorkspaceFact{{
		Ecosystem: "supermap", Kind: plugin.SpatialWorkspaceSuperMapSDXPostgreSQL,
		State: plugin.SpatialWorkspaceStateEnabled,
	}})
	payload, err := plugin.MarshalEngineCapabilities(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	engine := &models.Engine{ID: 7, EngineType: "postgresql", Capabilities: (*models.JSONString)(&payload)}
	_, err = Resolve(context.Background(), descriptorClient{}, engine)
	if err == nil {
		t.Fatal("Resolve() error = nil, want missing bound runtime")
	}
}

func TestResolveUsesRegisteredPluginForPlainPostgreSQL(t *testing.T) {
	engine := &models.Engine{ID: 8, EngineType: "postgresql"}
	resolved, err := Resolve(context.Background(), nil, engine)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved == nil || resolved.Type() != "postgresql" {
		t.Fatalf("resolved provider = %#v, want registered PostgreSQL provider", resolved)
	}
}

func TestResolveRejectsInactiveBoundRuntime(t *testing.T) {
	capabilities := plugin.EngineCapabilities{
		SchemaVersion: plugin.CapabilitiesSchemaVersion,
		EngineType:    "postgresql",
		Storage:       &plugin.StorageCapabilities{Store: &plugin.StoreCapability{}},
	}
	boundRuntimeID := uint(42)
	plugin.SetSpatialWorkspacesExtension(&capabilities, []plugin.SpatialWorkspaceFact{{
		Ecosystem: "supermap", Kind: plugin.SpatialWorkspaceSuperMapSDXPostgreSQL,
		State: plugin.SpatialWorkspaceStateEnabled, BoundRuntimeEngineID: &boundRuntimeID,
	}})
	payload, err := plugin.MarshalEngineCapabilities(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	engine := &models.Engine{ID: 9, EngineType: "postgresql", Capabilities: (*models.JSONString)(&payload)}
	_, err = Resolve(context.Background(), descriptorClient{descriptor: &models.EngineRuntimeDescriptor{
		ID: boundRuntimeID, EngineType: "supermap_workflow", LifecycleState: models.EngineLifecycleDisabled,
	}}, engine)
	if err == nil {
		t.Fatal("Resolve() error = nil, want inactive bound runtime rejection")
	}
}
