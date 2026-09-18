package service

import (
	"encoding/json"
	"testing"

	"github.com/addp/common/datatype"
	commonquery "github.com/addp/common/query"
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

	descriptor.InputContract.NamedParameters[0].Options = []commonquery.ParameterOption{{Value: "person-2", Labels: map[string]string{"zh-cn": "Person two", "en": "Person two"}}}
	if err := validateComponentConfiguration(component, descriptor); err == nil {
		t.Fatal("component default outside published options accepted")
	}
	descriptor.InputContract.NamedParameters[0].Options = nil

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

func TestValidateFieldValueLabels(t *testing.T) {
	label := func(raw string) models.FieldValueLabel {
		return models.FieldValueLabel{Value: json.RawMessage(raw), Label: "Name"}
	}
	for _, tc := range []struct {
		name      string
		fieldType datatype.FieldType
		labels    []models.FieldValueLabel
		valid     bool
	}{
		{"exact strings", datatype.FieldTypeString, []models.FieldValueLabel{label(`"a"`), label(`"A"`), label(`" a"`), label(`""`)}, true},
		{"booleans", datatype.FieldTypeBool, []models.FieldValueLabel{label(`true`), label(`false`)}, true},
		{"escaped duplicate", datatype.FieldTypeString, []models.FieldValueLabel{label(`"a"`), label(`"\u0061"`)}, false},
		{"boolean duplicate", datatype.FieldTypeBool, []models.FieldValueLabel{label(`true`), label(`true`)}, false},
		{"null", datatype.FieldTypeString, []models.FieldValueLabel{label(`null`)}, false},
		{"structured", datatype.FieldTypeString, []models.FieldValueLabel{label(`{}`)}, false},
		{"wrong type", datatype.FieldTypeBool, []models.FieldValueLabel{label(`"true"`)}, false},
		{"numeric field", datatype.FieldTypeInt, []models.FieldValueLabel{label(`1`)}, false},
		{"blank label", datatype.FieldTypeString, []models.FieldValueLabel{{Value: json.RawMessage(`"a"`), Label: " "}}, false},
		{"too many", datatype.FieldTypeString, make([]models.FieldValueLabel, 33), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateFieldValueLabels(tc.labels, tc.fieldType); (err == nil) != tc.valid {
				t.Fatalf("valid=%v, err=%v", tc.valid, err)
			}
		})
	}
	descriptor := testDescriptor(false)
	component := models.ComponentConfiguration{
		Name: "Orders", ServiceRef: &models.ServiceReference{ServiceType: "query", ServiceID: 23},
		QueryTemplate:  models.ComponentQueryTemplate{Select: []string{"id"}, PageLimit: 50, Format: "json"},
		RendererType:   models.RendererTypeTable,
		RendererConfig: json.RawMessage(`{"columns":["id"],"field_presentations":[{"field":"id","label":"Order","value_labels":[{"value":"a","label":"Alpha"}]}]}`),
	}
	if err := validateComponentConfiguration(component, descriptor); err != nil {
		t.Fatal(err)
	}
	component.RendererConfig = json.RawMessage(`{"columns":["id"],"field_presentations":[{"field":"id","label":"Order","value_labels":[{"value":1,"label":"Alpha"}]}]}`)
	if err := validateComponentConfiguration(component, descriptor); err == nil {
		t.Fatal("typed labels must be validated through renderer configuration")
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

func TestPeriodPresentationRequiresExplicitTypedServiceParameters(t *testing.T) {
	descriptor := testDescriptor(false)
	descriptor.InputContract.NamedParameters = []models.ConsumerNamedParameter{
		{Name: "g", Type: datatype.FieldTypeString, Required: true, Options: []commonquery.ParameterOption{{Value: "total"}, {Value: "month"}}},
		{Name: "s", Type: datatype.FieldTypeDate, Required: true},
		{Name: "e", Type: datatype.FieldTypeDate, Required: true},
	}
	fields := map[string]models.ConsumerOutputField{"arbitrary_date": {Name: "arbitrary_date", Type: datatype.FieldTypeDate}}
	binding := models.PeriodPresentation{GrainParameter: "g", StartParameter: "s", EndParameter: "e"}
	presentation := models.FieldPresentation{Field: "arbitrary_date", Label: "Period", TemporalFormat: "period", Period: &binding}
	validate := func() error {
		return validateFieldPresentations([]models.FieldPresentation{presentation}, []string{"arbitrary_date"}, fields, true, descriptor)
	}
	if err := validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(){
		func() { presentation.Period = nil },
		func() { binding.GrainParameter = "missing" },
		func() { binding.StartParameter = "missing" },
		func() { binding.EndParameter = "s" },
		func() { presentation.TemporalFormat = "date" },
		func() { descriptor.InputContract.NamedParameters[0].Options[1].Value = "day" },
		func() { descriptor.InputContract.NamedParameters[1].Required = false },
		func() { descriptor.InputContract.NamedParameters[1].Type = datatype.FieldTypeTimestamp },
		func() {
			fields["arbitrary_date"] = models.ConsumerOutputField{Name: "arbitrary_date", Type: datatype.FieldTypeString}
		},
	} {
		binding = models.PeriodPresentation{GrainParameter: "g", StartParameter: "s", EndParameter: "e"}
		presentation.Period = &binding
		presentation.TemporalFormat = "period"
		descriptor.InputContract.NamedParameters[0].Options[1].Value = "month"
		descriptor.InputContract.NamedParameters[1].Required = true
		descriptor.InputContract.NamedParameters[1].Type = datatype.FieldTypeDate
		fields["arbitrary_date"] = models.ConsumerOutputField{Name: "arbitrary_date", Type: datatype.FieldTypeDate}
		change()
		if err := validate(); err == nil {
			t.Fatal("invalid period binding accepted")
		}
	}
}

func TestTotalValueChartRequiresPeriodAndExplicitPrecisions(t *testing.T) {
	descriptor := testDescriptor(false)
	descriptor.InputContract.NamedParameters = []models.ConsumerNamedParameter{
		{Name: "g", Type: datatype.FieldTypeString, Required: true, Options: []commonquery.ParameterOption{{Value: "total"}, {Value: "month"}}},
		{Name: "s", Type: datatype.FieldTypeDate, Required: true},
		{Name: "e", Type: datatype.FieldTypeDate, Required: true},
	}
	fields := map[string]models.ConsumerOutputField{"d": {Name: "d", Type: datatype.FieldTypeDate}, "v": {Name: "v", Type: datatype.FieldTypeInt}}
	selected := map[string]struct{}{"d": {}, "v": {}}
	precision := 0
	base := models.ChartRendererConfig{ChartType: "bar", Dimension: "d", Measures: []string{"v"}, TotalAsValue: true, FieldPresentations: []models.FieldPresentation{
		{Field: "d", Label: "Period", TemporalFormat: "period", Period: &models.PeriodPresentation{GrainParameter: "g", StartParameter: "s", EndParameter: "e"}},
		{Field: "v", Label: "Count", Precision: &precision},
	}}
	for _, tc := range []struct {
		name   string
		change func(*models.ChartRendererConfig)
		valid  bool
	}{
		{"valid", func(c *models.ChartRendererConfig) {}, true},
		{"no period", func(c *models.ChartRendererConfig) {
			c.FieldPresentations[0].TemporalFormat = "date"
			c.FieldPresentations[0].Period = nil
		}, false},
		{"no precision", func(c *models.ChartRendererConfig) { c.FieldPresentations[1].Precision = nil }, false},
		{"five measures", func(c *models.ChartRendererConfig) {
			for _, f := range []string{"a", "b", "c", "e"} {
				fields[f] = models.ConsumerOutputField{Name: f, Type: datatype.FieldTypeInt}
				selected[f] = struct{}{}
				c.Measures = append(c.Measures, f)
				c.FieldPresentations = append(c.FieldPresentations, models.FieldPresentation{Field: f, Label: f, Precision: &precision})
			}
		}, false},
		{"ordinary chart", func(c *models.ChartRendererConfig) { c.TotalAsValue = false; c.FieldPresentations = nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := base
			config.FieldPresentations = append([]models.FieldPresentation{}, base.FieldPresentations...)
			tc.change(&config)
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			err = validateRenderer(models.RendererTypeChart, raw, descriptor, fields, selected, nil)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
}

func TestChartResultNameRequiresSelectedStringOutput(t *testing.T) {
	descriptor := testDescriptor(false)
	fields := map[string]models.ConsumerOutputField{"d": {Name: "d", Type: datatype.FieldTypeDate}, "v": {Name: "v", Type: datatype.FieldTypeInt}, "name": {Name: "name", Type: datatype.FieldTypeString}}
	for _, tc := range []struct {
		name       string
		field      string
		selectName bool
		valid      bool
	}{
		{"explicit name", "name", true, true}, {"absent", "", false, true}, {"unknown", "missing", true, false}, {"numeric", "v", true, false}, {"not selected", "name", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected := map[string]struct{}{"d": {}, "v": {}}
			if tc.selectName {
				selected["name"] = struct{}{}
			}
			config := models.ChartRendererConfig{ChartType: "bar", Dimension: "d", Measures: []string{"v"}, ResultNameField: tc.field}
			if tc.field != "" {
				config.FieldPresentations = []models.FieldPresentation{{Field: tc.field, Label: "Person"}}
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			err = validateRenderer(models.RendererTypeChart, raw, descriptor, fields, selected, nil)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
}

func TestParameterDisplaySourcesValidateExplicitLookupContract(t *testing.T) {
	makeFixture := func() ([]models.DataApplicationParameter, []models.DataApplicationSelectionBinding, map[string]models.DataApplicationComponent, map[string]*models.ConsumerDescriptor) {
		parameters := []models.DataApplicationParameter{{Key: "person", DisplaySource: &models.ApplicationParameterDisplaySource{SourceComponentID: "directory", LabelField: "name"}}}
		bindings := []models.DataApplicationSelectionBinding{{SourceComponentID: "directory", Assignments: []models.DataApplicationSelectionAssignment{{SourceField: "key", ApplicationParameterKey: "person"}}}}
		components := map[string]models.DataApplicationComponent{"directory": {ID: "directory", ContractFingerprint: "current", QueryTemplate: models.ComponentQueryTemplate{Select: []string{"key", "name"}}}}
		descriptors := map[string]*models.ConsumerDescriptor{"directory": {ContractFingerprint: "current", InputContract: models.StructuredQueryInputContract{Fields: []models.ConsumerQueryField{
			{Name: "key", Type: datatype.FieldTypeString, Selectable: true, Filterable: true, Operators: []string{"eq"}},
			{Name: "name", Type: datatype.FieldTypeString, Selectable: true},
		}}, OutputContract: models.TabularOutputContract{Fields: []models.ConsumerOutputField{{Name: "key", Type: datatype.FieldTypeString}, {Name: "name", Type: datatype.FieldTypeString}}}}}
		return parameters, bindings, components, descriptors
	}
	p, b, c, d := makeFixture()
	if err := validateParameterDisplaySources(p, b, c, d); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"missing-source", "missing-binding", "unselected-label", "non-string", "not-selectable", "no-eq", "stale-contract", "duplicate-assignment"} {
		t.Run(scenario, func(t *testing.T) {
			p, b, c, d := makeFixture()
			switch scenario {
			case "missing-source":
				p[0].DisplaySource.SourceComponentID = "missing"
			case "missing-binding":
				b = nil
			case "unselected-label":
				component := c["directory"]
				component.QueryTemplate.Select = []string{"key"}
				c["directory"] = component
			case "non-string":
				d["directory"].OutputContract.Fields[1].Type = datatype.FieldTypeInt
			case "not-selectable":
				d["directory"].InputContract.Fields[1].Selectable = false
			case "no-eq":
				d["directory"].InputContract.Fields[0].Operators = []string{"contains"}
			case "stale-contract":
				d["directory"].ContractFingerprint = "changed"
			case "duplicate-assignment":
				b[0].Assignments = append(b[0].Assignments, b[0].Assignments[0])
			}
			if err := validateParameterDisplaySources(p, b, c, d); err == nil {
				t.Fatal("invalid display source accepted")
			}
		})
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []models.DataApplicationParameter
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded[0].DisplaySource.LabelField != "name" {
		t.Fatalf("display source did not roundtrip: %v", err)
	}
}
