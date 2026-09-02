package service

import (
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	systemModels "github.com/addp/system/internal/models"
)

func TestBuildCapabilitiesViewIncludesTableSpatialEncoding(t *testing.T) {
	caps := engineplugin.NewTabularCapabilities("postgresql", "schema", engineplugin.TabularCapabilityOptions{
		TableReadSession: true,
		TableSpatialEncoding: &engineplugin.NativeTableSpatialEncodingCapability{
			GeometryReadEncodings:  []string{"ewkb", "geojson"},
			GeometryWriteEncodings: []string{"ewkb"},
			ReadTransform:          true,
			NativeSpatialFunctions: true,
		},
	})
	payload, err := engineplugin.MarshalEngineCapabilities(caps)
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities failed: %v", err)
	}
	jsonValue := systemModels.JSONString(payload)

	view := BuildCapabilitiesView(&jsonValue, "postgresql")
	if view == nil {
		t.Fatal("BuildCapabilitiesView returned nil")
	}
	item := findCapabilityItem(view, "storage", "table_io")
	if item == nil {
		t.Fatalf("table_io item not found in view: %#v", view.Sections)
	}
	assertCapabilityTag(t, *item, "table_read_session")
	assertCapabilityTag(t, *item, "geometry_read_encoding_ewkb")
	assertCapabilityTag(t, *item, "geometry_write_encoding_ewkb")
	assertCapabilityTag(t, *item, "spatial_read_transform")
	assertCapabilityTag(t, *item, "native_spatial_functions")
}

func TestBuildCapabilitiesViewIncludesEncodedRecordFormats(t *testing.T) {
	caps := engineplugin.NewDynamicSchemaCapabilities("mongodb")
	caps.Storage.Store.EncodedRecordReadSession = &engineplugin.EncodedRecordReadSessionCapability{
		Formats: []string{"mongodb_extended_jsonl"},
	}
	payload, err := engineplugin.MarshalEngineCapabilities(caps)
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities failed: %v", err)
	}
	jsonValue := systemModels.JSONString(payload)
	view := BuildCapabilitiesView(&jsonValue, "mongodb")
	item := findCapabilityItem(view, "storage", "content_read")
	if item == nil {
		t.Fatalf("content_read item not found: %#v", view)
	}
	assertCapabilityTag(t, *item, "record_read_session")
	assertCapabilityTag(t, *item, "encoded_record_read_session")
	assertCapabilityTag(t, *item, "record_format_mongodb_extended_jsonl")
}

func TestBuildCapabilitiesViewIncludesQueryParameters(t *testing.T) {
	caps := engineplugin.NewTabularCapabilities("postgresql", "schema", engineplugin.TabularCapabilityOptions{
		SupportsParameters: true,
	})
	payload, err := engineplugin.MarshalEngineCapabilities(caps)
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities failed: %v", err)
	}
	jsonValue := systemModels.JSONString(payload)

	view := BuildCapabilitiesView(&jsonValue, "postgresql")
	if view == nil {
		t.Fatal("BuildCapabilitiesView returned nil")
	}
	item := findCapabilityItem(view, "compute", "query")
	if item == nil {
		t.Fatalf("query item not found in view: %#v", view.Sections)
	}
	assertCapabilityTag(t, *item, "query_parameters")
	assertCapabilityTag(t, *item, "parameter_type_string")
	assertCapabilityTag(t, *item, "parameter_type_integer")
	assertCapabilityTag(t, *item, "parameter_type_number")
	assertCapabilityTag(t, *item, "parameter_type_boolean")
}

func TestBuildCapabilitiesViewFormatsPostgreSQLExtensions(t *testing.T) {
	caps := engineplugin.NewTabularCapabilities("postgresql", "schema", engineplugin.TabularCapabilityOptions{})
	caps.Extensions = map[string]interface{}{
		"postgresql": map[string]interface{}{
			"server_version":     "15.8 (Debian 15.8-1.pgdg120+1)",
			"server_version_num": 150008,
			"postgis": map[string]interface{}{
				"installed":    true,
				"available":    true,
				"schema":       "public",
				"version":      "3.4.3",
				"st_extent":    true,
				"st_transform": true,
			},
			"postgis_topology": map[string]interface{}{
				"installed": true,
				"available": true,
				"schema":    "topology",
				"version":   "3.4.3",
			},
			"postgis_tiger_geocoder": map[string]interface{}{
				"installed": true,
				"available": true,
				"schema":    "tiger",
				"version":   "3.4.3",
			},
			"pgvector": map[string]interface{}{
				"installed":      false,
				"available":      false,
				"type_available": false,
			},
		},
	}
	payload, err := engineplugin.MarshalEngineCapabilities(caps)
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities failed: %v", err)
	}
	jsonValue := systemModels.JSONString(payload)

	view := BuildCapabilitiesView(&jsonValue, "postgresql")
	if view == nil {
		t.Fatal("BuildCapabilitiesView returned nil")
	}
	server := findCapabilityItem(view, "extensions", "postgresql_server")
	if server == nil {
		t.Fatalf("postgresql_server item not found in view: %#v", view.Sections)
	}
	if server.Value != "15.8 (Debian 15.8-1.pgdg120+1)" {
		t.Fatalf("postgresql_server value = %q", server.Value)
	}
	postgis := findCapabilityItem(view, "extensions", "postgis")
	if postgis == nil {
		t.Fatalf("postgis item not found in view: %#v", view.Sections)
	}
	if postgis.Value != "3.4.3" {
		t.Fatalf("postgis value = %q", postgis.Value)
	}
	assertCapabilityTag(t, *postgis, "installed")
	assertCapabilityTag(t, *postgis, "schema")
	if findCapabilityItem(view, "extensions", "postgis_topology") != nil {
		t.Fatalf("postgis_topology should be grouped into postgresql_extra_extensions")
	}
	if findCapabilityItem(view, "extensions", "postgis_tiger_geocoder") != nil {
		t.Fatalf("postgis_tiger_geocoder should be grouped into postgresql_extra_extensions")
	}
	extraExtensions := findCapabilityItem(view, "extensions", "postgresql_extra_extensions")
	if extraExtensions == nil {
		t.Fatalf("postgresql_extra_extensions item not found in view: %#v", view.Sections)
	}
	assertCapabilityTag(t, *extraExtensions, "postgis_topology")
	assertCapabilityTag(t, *extraExtensions, "postgis_tiger_geocoder")
	pgvector := findCapabilityItem(view, "extensions", "pgvector")
	if pgvector == nil {
		t.Fatalf("pgvector item not found in view: %#v", view.Sections)
	}
	if pgvector.Value != "" {
		t.Fatalf("pgvector value = %q, want empty instead of raw JSON", pgvector.Value)
	}
	if pgvector.Status != capabilityStatusNotInstalled {
		t.Fatalf("pgvector status = %q, want %q", pgvector.Status, capabilityStatusNotInstalled)
	}
	if len(pgvector.Tags) != 0 {
		t.Fatalf("pgvector tags = %#v, want no duplicated not-installed tag", pgvector.Tags)
	}
}

func TestBuildCapabilitiesViewFormatsSpatialWorkspaces(t *testing.T) {
	boundRuntimeID := uint(33)
	caps := engineplugin.NewTabularCapabilities("postgresql", "schema", engineplugin.TabularCapabilityOptions{})
	caps.Extensions = map[string]interface{}{
		engineplugin.EngineExtensionSpatialWorkspaces: []engineplugin.SpatialWorkspaceFact{
			{
				Ecosystem:            "supermap",
				Kind:                 engineplugin.SpatialWorkspaceSuperMapSDXPostGIS,
				State:                engineplugin.SpatialWorkspaceStateNotDetected,
				BackendEngineType:    "postgresql",
				BoundRuntimeEngineID: &boundRuntimeID,
				CanEnable:            true,
				RiskLevel:            engineplugin.SpatialWorkspaceRiskHigh,
				Evidence: map[string]interface{}{
					"postgis_ready":               true,
					"supermap_system_table_count": 0,
				},
			},
			{
				Ecosystem:         "arcgis",
				Kind:              "sde",
				State:             engineplugin.SpatialWorkspaceStateUnavailable,
				BackendEngineType: "postgresql",
				CanEnable:         false,
				RiskLevel:         engineplugin.SpatialWorkspaceRiskHigh,
				Evidence: map[string]interface{}{
					"sde_schema_count": 0,
					"sde_table_count":  0,
				},
			},
		},
	}
	payload, err := engineplugin.MarshalEngineCapabilities(caps)
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities failed: %v", err)
	}
	jsonValue := systemModels.JSONString(payload)

	view := BuildCapabilitiesView(&jsonValue, "postgresql")
	if view == nil {
		t.Fatal("BuildCapabilitiesView returned nil")
	}
	item := findCapabilityItem(view, "extensions", "spatial_workspace_supermap_sdxPostgis_0")
	if item == nil {
		t.Fatalf("spatial workspace item not found in view: %#v", view.Sections)
	}
	if item.Status != capabilityStatusNotInstalled {
		t.Fatalf("spatial workspace status = %q, want %q", item.Status, capabilityStatusNotInstalled)
	}
	if item.LabelKey != "system.engine.capabilityView.extensions.supermapSdxPostgis" {
		t.Fatalf("supermap label key = %q", item.LabelKey)
	}
	assertCapabilityTag(t, *item, "ecosystem_supermap")
	assertCapabilityTag(t, *item, "kind_sdxPostgis")
	assertCapabilityTag(t, *item, "state_notDetected")
	assertCapabilityTag(t, *item, "bound_runtime_engine_id")
	assertCapabilityTag(t, *item, "risk_level_high")
	assertCapabilityTag(t, *item, "can_enable")
	assertCapabilityTag(t, *item, "evidence_postgisReady")

	arcgis := findCapabilityItem(view, "extensions", "spatial_workspace_arcgis_sde_1")
	if arcgis == nil {
		t.Fatalf("ArcGIS spatial workspace item not found in view: %#v", view.Sections)
	}
	if arcgis.LabelKey != "system.engine.capabilityView.extensions.arcgisSde" {
		t.Fatalf("arcgis label key = %q", arcgis.LabelKey)
	}
	assertCapabilityTag(t, *arcgis, "ecosystem_arcgis")
	assertCapabilityTag(t, *arcgis, "kind_sde")
	assertCapabilityTag(t, *arcgis, "evidence_sdeSchemaCount")
	assertCapabilityTag(t, *arcgis, "evidence_sdeTableCount")
}

func TestBuildCapabilitiesViewRendersOracleArcGISSDEAsReadOnlyWorkspace(t *testing.T) {
	caps := engineplugin.NewTabularCapabilities("oracle", "schema", engineplugin.TabularCapabilityOptions{})
	engineplugin.SetSpatialWorkspacesExtension(&caps, []engineplugin.SpatialWorkspaceFact{{
		Ecosystem:         "arcgis",
		Kind:              engineplugin.SpatialWorkspaceArcGISSDE,
		State:             engineplugin.SpatialWorkspaceStateNotDetected,
		BackendEngineType: "oracle",
		CanEnable:         false,
		RiskLevel:         engineplugin.SpatialWorkspaceRiskHigh,
		Evidence: map[string]interface{}{
			"required_registry_count": 0,
		},
	}})
	payload, err := engineplugin.MarshalEngineCapabilities(caps)
	if err != nil {
		t.Fatal(err)
	}
	value := systemModels.JSONString(payload)
	view := BuildCapabilitiesView(&value, "oracle")
	item := findCapabilityItem(view, "extensions", "spatial_workspace_arcgis_sde_0")
	if item == nil || item.LabelKey != "system.engine.capabilityView.extensions.arcgisSde" || item.Status != capabilityStatusNotInstalled {
		t.Fatalf("oracle ArcGIS SDE item = %#v", item)
	}
	assertCapabilityTag(t, *item, "backend_engine_type")
	assertCapabilityTag(t, *item, "state_notDetected")
}

func findCapabilityItem(view *commonModels.CapabilitiesView, sectionID, itemID string) *commonModels.CapabilityViewItem {
	for _, section := range view.Sections {
		if section.ID != sectionID {
			continue
		}
		for i := range section.Items {
			if section.Items[i].ID == itemID {
				return &section.Items[i]
			}
		}
	}
	return nil
}

func assertCapabilityTag(t *testing.T, item commonModels.CapabilityViewItem, tagID string) {
	t.Helper()
	for _, tag := range item.Tags {
		if tag.ID == tagID {
			return
		}
	}
	t.Fatalf("tag %q not found in item %#v", tagID, item)
}
