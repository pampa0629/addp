package service

import (
	"encoding/json"
	"testing"

	"github.com/addp/workbench/internal/models"
)

func TestValidateValueRendererUsesExplicitNumericServiceFields(t *testing.T) {
	descriptor := testDescriptor(false)
	component := models.ComponentConfiguration{
		Name:       "Summary",
		ServiceRef: &models.ServiceReference{ServiceType: "query", ServiceID: 23},
		QueryTemplate: models.ComponentQueryTemplate{
			Select: []string{"amount"}, PageLimit: 1, Format: "json",
		},
		RendererType:   models.RendererTypeValue,
		RendererConfig: json.RawMessage(`{"items":[{"field":"amount","label":"Total","unit":"items","precision":2}]}`),
	}
	if err := validateComponentConfiguration(component, descriptor); err != nil {
		t.Fatalf("validateComponentConfiguration() error = %v", err)
	}

	component.RendererConfig = json.RawMessage(`{"items":[{"field":"status","label":"Status","unit":"","precision":0}]}`)
	component.QueryTemplate.Select = []string{"status"}
	if err := validateComponentConfiguration(component, descriptor); err == nil {
		t.Fatal("non-numeric value field must be rejected")
	}
}

func TestValidateValueRendererRejectsDuplicatedAndUnselectedFields(t *testing.T) {
	descriptor := testDescriptor(false)
	component := models.ComponentConfiguration{
		Name:           "Summary",
		ServiceRef:     &models.ServiceReference{ServiceType: "query", ServiceID: 23},
		QueryTemplate:  models.ComponentQueryTemplate{Select: []string{"id"}, PageLimit: 1, Format: "json"},
		RendererType:   models.RendererTypeValue,
		RendererConfig: json.RawMessage(`{"items":[{"field":"amount","label":"Total","unit":"","precision":0}]}`),
	}
	if err := validateComponentConfiguration(component, descriptor); err == nil {
		t.Fatal("unselected value field must be rejected")
	}

	component.QueryTemplate.Select = []string{"amount"}
	component.RendererConfig = json.RawMessage(`{"items":[{"field":"amount","label":"A","unit":"","precision":0},{"field":"amount","label":"B","unit":"","precision":0}]}`)
	if err := validateComponentConfiguration(component, descriptor); err == nil {
		t.Fatal("duplicated value fields must be rejected")
	}
}

func TestValidateMapRendererUsesExplicitSpatialAndThematicFields(t *testing.T) {
	descriptor := testDescriptor(true)
	component := models.ComponentConfiguration{
		Name:       "Spatial distribution",
		ServiceRef: &models.ServiceReference{ServiceType: "query", ServiceID: 23},
		QueryTemplate: models.ComponentQueryTemplate{
			Select: []string{"id", "amount", "shape"}, PageLimit: 1000, Format: "json",
			OrderBy: []models.QueryOrder{{Field: "id", Direction: "asc"}},
		},
		RendererType:   models.RendererTypeMap,
		RendererConfig: json.RawMessage(`{"geometry_field":"shape","label_field":"id","tooltip_fields":["amount"],"style":{"mode":"continuous","field":"amount","palette":"primary","legend_title":"Amount"}}`),
	}
	if err := validateComponentConfiguration(component, descriptor); err != nil {
		t.Fatalf("validateComponentConfiguration() error = %v", err)
	}

	component.RendererConfig = json.RawMessage(`{"geometry_field":"shape","label_field":"id","tooltip_fields":[],"style":{"mode":"continuous","field":"id","palette":"primary","legend_title":"ID"}}`)
	if err := validateComponentConfiguration(component, descriptor); err == nil {
		t.Fatal("continuous map style must reject a non-numeric field")
	}
}
