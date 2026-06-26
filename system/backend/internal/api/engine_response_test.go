package api

import (
	"testing"

	"github.com/addp/system/internal/models"
)

func TestEngineListResponseIncludesCapabilitiesView(t *testing.T) {
	capabilities := models.JSONString(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"python_workflow",
		"engine_family":"workflow",
		"compute":{
			"workflow":{
				"supported":true,
				"runtime_api":"addp.workflow/v1",
				"dynamic_operators":true
			}
		}
	}`)
	engine := models.Engine{
		ID:           1,
		Name:         "Python Workflow",
		EngineType:   "python_workflow",
		Capabilities: &capabilities,
	}

	responses := toEngineResponses([]models.Engine{engine})

	if len(responses) != 1 {
		t.Fatalf("responses length = %d, want 1", len(responses))
	}
	view := responses[0].CapabilitiesView
	if view == nil {
		t.Fatal("CapabilitiesView is nil")
	}
	if !hasCapabilityViewSection(view, "compute") {
		t.Fatalf("CapabilitiesView sections = %#v, want compute section", view.Sections)
	}
	if !hasCapabilityViewBadge(view, "workflow") {
		t.Fatalf("CapabilitiesView summary = %#v, want workflow badge", view.Summary)
	}
	if len(view.JSONView) == 0 {
		t.Fatal("CapabilitiesView JSONView is empty")
	}
}

func TestEngineListResponseSkipsCapabilitiesViewForLegacyCapabilities(t *testing.T) {
	legacy := models.JSONString(`{"compute":[{"dev_modes":["workflow"]}]}`)
	engine := models.Engine{
		ID:           1,
		Name:         "Legacy Workflow",
		EngineType:   "legacy_workflow",
		Capabilities: &legacy,
	}

	response := toEngineResponse(&engine)

	if response.CapabilitiesView != nil {
		t.Fatalf("CapabilitiesView = %#v, want nil", response.CapabilitiesView)
	}
}

func TestEngineListResponseSkipsCapabilitiesViewForUnsupportedSchemaVersion(t *testing.T) {
	unsupported := models.JSONString(`{
		"schema_version":"engine.capabilities/v0",
		"engine_type":"python_workflow",
		"engine_family":"workflow",
		"compute":{
			"workflow":{
				"supported":true,
				"runtime_api":"addp.workflow/v1",
				"dynamic_operators":true
			}
		}
	}`)
	engine := models.Engine{
		ID:           1,
		Name:         "Unsupported Workflow",
		EngineType:   "python_workflow",
		Capabilities: &unsupported,
	}

	response := toEngineResponse(&engine)

	if response.CapabilitiesView != nil {
		t.Fatalf("CapabilitiesView = %#v, want nil", response.CapabilitiesView)
	}
}

func hasCapabilityViewSection(view *models.CapabilitiesView, id string) bool {
	for _, section := range view.Sections {
		if section.ID == id {
			return true
		}
	}
	return false
}

func hasCapabilityViewBadge(view *models.CapabilitiesView, id string) bool {
	for _, badge := range view.Summary {
		if badge.ID == id {
			return true
		}
	}
	return false
}
