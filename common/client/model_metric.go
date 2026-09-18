package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"net/http"
)

// ModelMetricParameter is a read-only presentation of a frozen plan parameter.
type ModelMetricParameter struct {
	Presentation *commonquery.ParameterPresentation `json:"presentation,omitempty"`
	Options      []commonquery.ParameterOption      `json:"options,omitempty"`
	Name         string                             `json:"name"`
	Type         datatype.FieldType                 `json:"type"`
	Required     bool                               `json:"required"`
}

// ModelMetricPlan carries a Model-owned frozen artifact and its publication identity.
type ModelMetricPlan struct {
	ResultKind                 string                                       `json:"result_kind,omitempty"`
	ImplementationID           int64                                        `json:"implementation_id"`
	RevisionID                 int64                                        `json:"revision_id"`
	MetricDefinitionID         int64                                        `json:"metric_definition_id"`
	MetricDefinitionRevisionID int64                                        `json:"metric_definition_revision_id"`
	DependencyHash             string                                       `json:"dependency_hash"`
	ExecutionPlan              plugin.AnalyticalPlanPackage                 `json:"execution_plan"`
	ParameterLabels            map[string][]commonquery.ParameterOption     `json:"parameter_labels"`
	ParameterPresentation      map[string]commonquery.ParameterPresentation `json:"parameter_presentation,omitempty"`
}

func (p *ModelMetricPlan) UnmarshalJSON(data []byte) error {
	type exact ModelMetricPlan
	var value exact
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&value); err != nil {
		return err
	}
	*p = ModelMetricPlan(value)
	return nil
}
func (p ModelMetricPlan) Parameters() []ModelMetricParameter {
	result := make([]ModelMetricParameter, 0, len(p.ExecutionPlan.Plan.Parameters))
	for _, v := range p.ExecutionPlan.Plan.Parameters {
		parameter := ModelMetricParameter{Name: v.Name, Type: v.Type, Required: v.Required, Options: p.ParameterLabels[v.Name]}
		if presentation, ok := p.ParameterPresentation[v.Name]; ok {
			parameter.Presentation = &presentation
		}
		result = append(result, parameter)
	}
	return result
}
func (p ModelMetricPlan) Validate() error {
	if (p.ResultKind != "" && p.ResultKind != "details") || p.ImplementationID <= 0 || p.RevisionID <= 0 || p.MetricDefinitionID <= 0 || p.MetricDefinitionRevisionID <= 0 || len(p.DependencyHash) != 64 || p.ExecutionPlan.EngineID == 0 || p.ExecutionPlan.SchemaVersion != plugin.AnalyticalPlanSchemaVersion || len(p.ExecutionPlan.PackageHash) != 64 {
		return errors.New("Model returned an invalid metric plan")
	}
	if err := p.ExecutionPlan.Validate(); err != nil {
		return err
	}
	known := map[string]bool{}
	for _, param := range p.ExecutionPlan.Plan.Parameters {
		known[param.Name] = true
		options := p.ParameterLabels[param.Name]
		if len(options) != len(param.Allowed) {
			return errors.New("metric labels do not match parameter values")
		}
		if err := commonquery.ValidateParameterOptions(options, func(any) error { return nil }); err != nil {
			return err
		}
		allowed := map[string]bool{}
		for _, v := range param.Allowed {
			allowed[v.Text] = true
		}
		for _, v := range options {
			text, ok := v.Value.(string)
			if !ok || !allowed[text] {
				return errors.New("invalid metric parameter labels")
			}
		}
	}
	for name := range p.ParameterLabels {
		if !known[name] {
			return errors.New("unknown metric parameter label")
		}
	}
	if len(p.ParameterPresentation) > 0 && len(p.ParameterPresentation) != len(known) {
		return errors.New("incomplete metric parameter presentation")
	}
	for name, presentation := range p.ParameterPresentation {
		if !known[name] {
			return errors.New("unknown metric parameter presentation")
		}
		if err := presentation.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c *ModelClient) GetMetricPlan(ctx context.Context, id, revisionID int64, input map[string]interface{}, resultKind string) (*ModelMetricPlan, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || id <= 0 || revisionID <= 0 {
		return nil, errors.New("metric plan requires tenant and revision identity")
	}
	var plan ModelMetricPlan
	request := map[string]any{"input": input, "result_kind": resultKind}
	path := fmt.Sprintf("/api/v1/model/metric-implementations/%d/revisions/%d/plan", id, revisionID)
	if err := c.doJSON(ctx, http.MethodPost, path, request, &plan); err != nil {
		return nil, err
	}
	if plan.ImplementationID != id || plan.RevisionID != revisionID || plan.ResultKind != resultKind {
		return nil, errors.New("Model returned a different metric revision")
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	return &plan, nil
}
