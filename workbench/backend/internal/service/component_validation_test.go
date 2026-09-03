package service

import (
	"encoding/json"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/workbench/internal/models"
)

func TestValidateComponentBindsRequiredServiceNamedParameters(t *testing.T) {
	descriptor := testDescriptor(false)
	descriptor.InputContract.NamedParameters = []models.ConsumerNamedParameter{
		{Name: "person_id_a", Type: datatype.FieldTypeString, Required: true, Description: "First person"},
		{Name: "minimum_count", Type: datatype.FieldTypeInt, Required: false, Default: float64(1)},
	}
	component := models.ComponentConfiguration{
		Name:       "Overlap",
		ServiceRef: &models.ServiceReference{ServiceType: "query", ServiceID: 23},
		ParameterDefinitions: []models.ComponentParameterDefinition{
			{Key: "first_person", Label: "First person", ControlType: "text", Required: true},
		},
		QueryTemplate: models.ComponentQueryTemplate{
			Select: []string{"id", "amount"},
			NamedParameterBindings: []models.ComponentNamedParameterBinding{
				{ParameterKey: "first_person", Name: "person_id_a"},
			},
			PageLimit: 1, Format: "json",
		},
		DefaultParameterValues: map[string]json.RawMessage{"first_person": json.RawMessage(`"person-1"`)},
		RendererType:           models.RendererTypeTable,
		RendererConfig:         json.RawMessage(`{"columns":["id","amount"]}`),
	}
	if err := validateComponentConfiguration(component, descriptor); err != nil {
		t.Fatalf("validateComponentConfiguration() error = %v", err)
	}

	component.QueryTemplate.NamedParameterBindings = nil
	if err := validateComponentConfiguration(component, descriptor); err == nil {
		t.Fatal("unbound required service named parameter must be rejected")
	}
	component.QueryTemplate.NamedParameterBindings = []models.ComponentNamedParameterBinding{{ParameterKey: "first_person", Name: "person_id_a"}}
	component.ParameterDefinitions[0].ControlType = "number"
	if err := validateComponentConfiguration(component, descriptor); err == nil {
		t.Fatal("incompatible named parameter control must be rejected")
	}
}

func TestValidateSelectionBindingTargetsServiceNamedParameter(t *testing.T) {
	const sourceID = "source"
	const targetID = "target"
	components := map[string]models.DataApplicationComponent{
		sourceID: {ID: sourceID, QueryTemplate: models.ComponentQueryTemplate{Select: []string{"person_id"}}},
		targetID: {
			ID: targetID,
			QueryTemplate: models.ComponentQueryTemplate{NamedParameterBindings: []models.ComponentNamedParameterBinding{
				{ParameterKey: "first_person", Name: "person_id_a"},
			}},
		},
	}
	descriptors := map[string]*models.ConsumerDescriptor{
		sourceID: {OutputContract: models.TabularOutputContract{Fields: []models.ConsumerOutputField{{Name: "person_id", Type: datatype.FieldTypeString, Nullable: false}}}},
		targetID: {InputContract: models.StructuredQueryInputContract{NamedParameters: []models.ConsumerNamedParameter{{Name: "person_id_a", Type: datatype.FieldTypeString, Required: true}}}},
	}
	parameters := []models.DataApplicationParameter{{Key: "person_a", Required: true}}
	parameterBindings := []models.DataApplicationParameterBinding{{ApplicationParameterKey: "person_a", ComponentID: targetID, ComponentParameterKey: "first_person"}}
	selectionBindings := []models.DataApplicationSelectionBinding{{
		SourceComponentID: sourceID,
		Assignments:       []models.DataApplicationSelectionAssignment{{SourceField: "person_id", ApplicationParameterKey: "person_a"}},
	}}
	if err := validateSelectionBindings(selectionBindings, parameters, parameterBindings, components, descriptors); err != nil {
		t.Fatalf("validateSelectionBindings() error = %v", err)
	}

	descriptors[targetID].InputContract.NamedParameters[0].Type = datatype.FieldTypeBigInt
	if err := validateSelectionBindings(selectionBindings, parameters, parameterBindings, components, descriptors); err == nil {
		t.Fatal("mismatched named parameter type must be rejected")
	}
}

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
