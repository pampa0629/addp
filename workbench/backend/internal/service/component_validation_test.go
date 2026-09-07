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

func TestValidateRendererFieldPresentations(t *testing.T) {
	descriptor := testDescriptor(false)
	descriptor.InputContract.Fields = append(descriptor.InputContract.Fields, models.ConsumerQueryField{Name: "created_at", Type: datatype.FieldTypeTimestamp, Selectable: true})
	descriptor.OutputContract.Fields = append(descriptor.OutputContract.Fields, models.ConsumerOutputField{Name: "created_at", Type: datatype.FieldTypeTimestamp})
	component := models.ComponentConfiguration{
		Name: "Orders", ServiceRef: &models.ServiceReference{ServiceType: "query", ServiceID: 23},
		QueryTemplate:  models.ComponentQueryTemplate{Select: []string{"id", "amount", "created_at"}, PageLimit: 50, Format: "json"},
		RendererType:   models.RendererTypeTable,
		RendererConfig: json.RawMessage(`{"columns":["id","amount","created_at"],"field_presentations":[{"field":"id","label":"Order","width":160},{"field":"amount","label":"Amount","unit":"USD","precision":2,"state_rules":[{"operator":"gt","operand":100,"label":"High","tone":"warning"}]},{"field":"created_at","label":"Created","temporal_format":"datetime","state_rules":[{"operator":"eq","operand":"2026-09-07T00:00:00Z","label":"Today","tone":"info"}]}]}`),
	}
	if err := validateComponentConfiguration(component, descriptor); err != nil {
		t.Fatalf("validateComponentConfiguration() error = %v", err)
	}

	invalidConfigs := []string{
		`{"columns":["id"],"field_presentations":[{"field":"id","label":"A"},{"field":"id","label":"B"}]}`,
		`{"columns":["id"],"field_presentations":[{"field":"amount","label":"Amount"}]}`,
		`{"columns":["amount"],"field_presentations":[{"field":"amount","label":"Amount","precision":9}]}`,
		`{"columns":["id"],"field_presentations":[{"field":"id","label":"Order","unit":"items"}]}`,
		`{"columns":["created_at"],"field_presentations":[{"field":"created_at","label":"Created","temporal_format":"time"}]}`,
		`{"columns":["id"],"field_presentations":[{"field":"id","label":"Order","width":79}]}`,
		`{"columns":["amount"],"field_presentations":[{"field":"amount","label":"Amount","state_rules":[{"operator":"contains","operand":10,"label":"Bad","tone":"warning"}]}]}`,
		`{"columns":["id"],"field_presentations":[{"field":"id","label":"Order","state_rules":[{"operator":"gt","operand":"A","label":"Bad","tone":"warning"}]}]}`,
		`{"columns":["amount"],"field_presentations":[{"field":"amount","label":"Amount","state_rules":[{"operator":"gt","operand":"100","label":"Bad","tone":"warning"}]}]}`,
		`{"columns":["amount"],"field_presentations":[{"field":"amount","label":"Amount","state_rules":[{"operator":"gt","operand":100,"label":"","tone":"warning"}]}]}`,
		`{"columns":["amount"],"field_presentations":[{"field":"amount","label":"Amount","state_rules":[{"operator":"gt","operand":100,"label":"High","tone":"purple"}]}]}`,
	}
	for _, raw := range invalidConfigs {
		component.RendererConfig = json.RawMessage(raw)
		if err := validateComponentConfiguration(component, descriptor); err == nil {
			t.Fatalf("invalid field presentation must be rejected: %s", raw)
		}
	}
}

func TestRejectsTableOnlyWidthInChartAndMapFieldPresentations(t *testing.T) {
	descriptor := testDescriptor(true)
	chart := models.ComponentConfiguration{
		Name: "Chart", ServiceRef: &models.ServiceReference{ServiceType: "query", ServiceID: 23},
		QueryTemplate:  models.ComponentQueryTemplate{Select: []string{"id", "amount"}, PageLimit: 50, Format: "json"},
		RendererType:   models.RendererTypeChart,
		RendererConfig: json.RawMessage(`{"chart_type":"bar","dimension":"id","measures":["amount"],"field_presentations":[{"field":"amount","label":"Amount","width":120}]}`),
	}
	if err := validateComponentConfiguration(chart, descriptor); err == nil {
		t.Fatal("chart field presentation width must be rejected")
	}
	chart.RendererConfig = json.RawMessage(`{"chart_type":"bar","dimension":"id","measures":["amount"],"field_presentations":[{"field":"id","label":"Order"},{"field":"amount","label":"Amount","unit":"USD","precision":2}]}`)
	if err := validateComponentConfiguration(chart, descriptor); err != nil {
		t.Fatalf("valid chart field presentations rejected: %v", err)
	}

	mapComponent := models.ComponentConfiguration{
		Name: "Map", ServiceRef: &models.ServiceReference{ServiceType: "query", ServiceID: 23},
		QueryTemplate:  models.ComponentQueryTemplate{Select: []string{"id", "amount", "shape"}, PageLimit: 50, Format: "json"},
		RendererType:   models.RendererTypeMap,
		RendererConfig: json.RawMessage(`{"geometry_field":"shape","label_field":"id","tooltip_fields":["amount"],"style":{"mode":"uniform","palette":"primary"},"field_presentations":[{"field":"amount","label":"Amount","width":120}]}`),
	}
	if err := validateComponentConfiguration(mapComponent, descriptor); err == nil {
		t.Fatal("map field presentation width must be rejected")
	}
	mapComponent.RendererConfig = json.RawMessage(`{"geometry_field":"shape","label_field":"id","tooltip_fields":["amount"],"style":{"mode":"uniform","palette":"primary"},"field_presentations":[{"field":"id","label":"Order"},{"field":"amount","label":"Amount","unit":"USD","precision":2}]}`)
	if err := validateComponentConfiguration(mapComponent, descriptor); err != nil {
		t.Fatalf("valid map field presentations rejected: %v", err)
	}
}

func TestValidateStatePresentationRuleBoundaries(t *testing.T) {
	validNumeric := []models.StatePresentationRule{
		{Operator: "lt", Operand: json.RawMessage(`60`), Label: "Low", Tone: "danger"},
		{Operator: "gte", Operand: json.RawMessage(`60`), Label: "Ready", Tone: "success"},
	}
	if err := validateStatePresentationRules(validNumeric, datatype.FieldTypeDecimal); err != nil {
		t.Fatalf("valid numeric state rules rejected: %v", err)
	}
	if err := validateStatePresentationRules([]models.StatePresentationRule{{Operator: "eq", Operand: json.RawMessage(`true`), Label: "Enabled", Tone: "info"}}, datatype.FieldTypeBool); err != nil {
		t.Fatalf("valid boolean state rule rejected: %v", err)
	}

	invalid := []struct {
		name      string
		fieldType datatype.FieldType
		rules     []models.StatePresentationRule
	}{
		{name: "comparison on string", fieldType: datatype.FieldTypeString, rules: []models.StatePresentationRule{{Operator: "gt", Operand: json.RawMessage(`"a"`), Label: "Bad", Tone: "warning"}}},
		{name: "wrong operand type", fieldType: datatype.FieldTypeDecimal, rules: []models.StatePresentationRule{{Operator: "eq", Operand: json.RawMessage(`"1"`), Label: "Bad", Tone: "warning"}}},
		{name: "fractional integer", fieldType: datatype.FieldTypeInt, rules: []models.StatePresentationRule{{Operator: "eq", Operand: json.RawMessage(`1.5`), Label: "Bad", Tone: "warning"}}},
		{name: "duplicate", fieldType: datatype.FieldTypeInt, rules: []models.StatePresentationRule{{Operator: "eq", Operand: json.RawMessage(`1`), Label: "One", Tone: "info"}, {Operator: "eq", Operand: json.RawMessage(`1`), Label: "Again", Tone: "danger"}}},
	}
	tooMany := make([]models.StatePresentationRule, 9)
	for index := range tooMany {
		tooMany[index] = models.StatePresentationRule{Operator: "eq", Operand: json.RawMessage([]byte{byte('1' + index)}), Label: "State", Tone: "info"}
	}
	invalid = append(invalid, struct {
		name      string
		fieldType datatype.FieldType
		rules     []models.StatePresentationRule
	}{name: "too many", fieldType: datatype.FieldTypeInt, rules: tooMany})

	for _, testCase := range invalid {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateStatePresentationRules(testCase.rules, testCase.fieldType); err == nil {
				t.Fatal("invalid state presentation rules must be rejected")
			}
		})
	}
}
