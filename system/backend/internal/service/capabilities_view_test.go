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
