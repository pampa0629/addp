package service

import (
	"encoding/json"
	"github.com/addp/common/datatype"
	q "github.com/addp/common/query"
	"github.com/addp/workbench/internal/models"
	"testing"
)

func TestApplicationParameterOptionsValidateAllBoundTargets(t *testing.T) {
	option := func(v string) q.ParameterOption {
		return q.ParameterOption{Value: v, Labels: map[string]string{"zh-cn": v, "en": v}}
	}
	components := map[string]models.DataApplicationComponent{}
	descriptors := map[string]*models.ConsumerDescriptor{}
	definitions := map[string]map[string]models.ComponentParameterDefinition{}
	bindings := []models.DataApplicationParameterBinding{}
	for _, id := range []string{"a", "b"} {
		components[id] = models.DataApplicationComponent{ID: id, Title: id, QueryTemplate: models.ComponentQueryTemplate{NamedParameterBindings: []models.ComponentNamedParameterBinding{{Name: "mode", ParameterKey: "mode"}}}}
		descriptors[id] = &models.ConsumerDescriptor{InputContract: models.StructuredQueryInputContract{NamedParameters: []models.ConsumerNamedParameter{{Name: "mode", Type: datatype.FieldTypeString, Required: true, Options: []q.ParameterOption{option("total"), option("month")}}}}}
		definitions[id] = map[string]models.ComponentParameterDefinition{"mode": {Key: "mode", ControlType: "text"}}
		bindings = append(bindings, models.DataApplicationParameterBinding{ApplicationParameterKey: "shared", ComponentID: id, ComponentParameterKey: "mode"})
	}
	params := []models.DataApplicationParameter{{Key: "shared", Label: "Shared", ControlType: "text", DefaultValue: json.RawMessage(`"month"`)}}
	check := func() error {
		return validateApplicationParameters(params, bindings, components, definitions, descriptors)
	}
	if err := check(); err != nil {
		t.Fatal(err)
	}
	descriptors["b"].InputContract.NamedParameters[0].Options = []q.ParameterOption{option("month")}
	if err := check(); err != nil {
		t.Fatal(err)
	}
	presets := []models.DataApplicationParameterPreset{{Key: "invalid", Name: "Invalid", ParameterValues: map[string]json.RawMessage{"shared": json.RawMessage(`"total"`)}}}
	if err := validateApplicationParameterPresets(presets, params, bindings, components, definitions, descriptors); err == nil {
		t.Fatal("preset outside target intersection accepted")
	}
	params[0].DefaultValue = json.RawMessage(`"total"`)
	if check() == nil {
		t.Fatal("default not accepted by every target")
	}
	params[0].DefaultValue = nil
	descriptors["b"].InputContract.NamedParameters[0].Options = []q.ParameterOption{option("week")}
	if check() == nil {
		t.Fatal("empty domain accepted without a default")
	}
	descriptors["b"].InputContract.NamedParameters[0].Options = []q.ParameterOption{{Value: "month", Labels: map[string]string{"zh-cn": "different", "en": "month"}}}
	if check() == nil {
		t.Fatal("label conflict accepted")
	}
	target := componentParameterTarget{Type: datatype.FieldTypeString, Operator: "eq", Options: []q.ParameterOption{option("month")}}
	if validateRawParameterTarget(json.RawMessage(`"total"`), target, 1) == nil {
		t.Fatal("preset value outside allowed options accepted")
	}
}
