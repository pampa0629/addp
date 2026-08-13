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

func TestIsSuperMapSDXPostgreSQLTableScopesWorkspaceToSDXSchema(t *testing.T) {
	boundRuntimeID := uint(42)
	capabilities := plugin.EngineCapabilities{
		SchemaVersion: plugin.CapabilitiesSchemaVersion,
		EngineType:    "postgresql",
		Extensions: map[string]interface{}{
			plugin.EngineExtensionSpatialWorkspaces: []plugin.SpatialWorkspaceFact{{
				Ecosystem: "supermap", Kind: plugin.SpatialWorkspaceSuperMapSDXPostgreSQL,
				State: plugin.SpatialWorkspaceStateEnabled, BoundRuntimeEngineID: &boundRuntimeID,
			}},
		},
	}
	payload, err := plugin.MarshalEngineCapabilities(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	engine := &models.Engine{ID: 10, EngineType: "postgresql", Capabilities: (*models.JSONString)(&payload)}
	if IsSuperMapSDXPostgreSQLTable(engine, "public", "roads") {
		t.Fatal("public table must remain on the PostgreSQL/PostGIS provider")
	}
	if !IsSuperMapSDXPostgreSQLTable(engine, "sdx", "roads") {
		t.Fatal("sdx table should use the SuperMap SDX+ for PostgreSQL provider")
	}
}

func TestArcGISSDEWorkspaceRequiresDetectedState(t *testing.T) {
	engine := engineWithSpatialWorkspace(t, 11, "oracle", plugin.SpatialWorkspaceFact{
		Ecosystem: "arcgis", Kind: plugin.SpatialWorkspaceArcGISSDE,
		State: plugin.SpatialWorkspaceStateNotDetected, BackendEngineType: "oracle",
	})
	if _, ready, err := ArcGISSDEWorkspace(engine); err != nil || ready {
		t.Fatalf("not-detected ArcGIS SDE workspace ready=%v error=%v", ready, err)
	}

	engine = engineWithSpatialWorkspace(t, 12, "oracle", plugin.SpatialWorkspaceFact{
		Ecosystem: "arcgis", Kind: plugin.SpatialWorkspaceArcGISSDE,
		State: plugin.SpatialWorkspaceStateDetected, BackendEngineType: "oracle",
	})
	workspace, ready, err := ArcGISSDEWorkspace(engine)
	if err != nil || !ready || workspace.BackendEngineType != "oracle" {
		t.Fatalf("detected ArcGIS SDE workspace=%#v ready=%v error=%v", workspace, ready, err)
	}
}

func TestSpatialWorkspaceRejectsInvalidCapabilities(t *testing.T) {
	invalid := models.JSONString(`{"extensions":`)
	_, _, err := SpatialWorkspace(&models.Engine{ID: 13, EngineType: "oracle", Capabilities: &invalid}, "arcgis", plugin.SpatialWorkspaceArcGISSDE)
	if err == nil {
		t.Fatal("SpatialWorkspace() error = nil, want invalid capability error")
	}
}

func engineWithSpatialWorkspace(t *testing.T, id uint, engineType string, workspace plugin.SpatialWorkspaceFact) *models.Engine {
	t.Helper()
	capabilities := plugin.EngineCapabilities{
		SchemaVersion: plugin.CapabilitiesSchemaVersion,
		EngineType:    engineType,
		Storage:       &plugin.StorageCapabilities{Store: &plugin.StoreCapability{}},
	}
	plugin.SetSpatialWorkspacesExtension(&capabilities, []plugin.SpatialWorkspaceFact{workspace})
	payload, err := plugin.MarshalEngineCapabilities(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	value := models.JSONString(payload)
	return &models.Engine{ID: id, EngineType: engineType, Capabilities: &value}
}
