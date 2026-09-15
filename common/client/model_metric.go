package client

import (
	"context"
	"errors"
	"fmt"
	"github.com/addp/common/datatype"
	commonquery "github.com/addp/common/query"
	"net/http"
)

// ModelMetricPlan is a Model-owned compiled artifact, not a second metric definition.
type ModelMetricParameter struct {
	Options  []commonquery.ParameterOption `json:"options,omitempty"`
	Name     string                        `json:"name"`
	Type     datatype.FieldType            `json:"type"`
	Required bool                          `json:"required"`
}

type ModelMetricPlan struct {
	Parameters                 []ModelMetricParameter `json:"parameters"`
	Fields                     []datatype.FieldInfo   `json:"fields"`
	StableKey                  []string               `json:"stable_key"`
	ImplementationID           int64                  `json:"implementation_id"`
	RevisionID                 int64                  `json:"revision_id"`
	MetricDefinitionID         int64                  `json:"metric_definition_id"`
	MetricDefinitionRevisionID int64                  `json:"metric_definition_revision_id"`
	DependencyHash             string                 `json:"dependency_hash"`
	EngineID                   uint                   `json:"engine_id"`
	SQL                        string                 `json:"sql"`
}

func (c *ModelClient) GetMetricPlan(ctx context.Context, id, revisionID int64, input map[string]interface{}) (*ModelMetricPlan, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || id <= 0 || revisionID <= 0 {
		return nil, errors.New("metric plan requires tenant and revision identity")
	}
	var plan ModelMetricPlan
	request := map[string]any{"input": input}
	path := fmt.Sprintf("/api/v1/model/metric-implementations/%d/revisions/%d/plan", id, revisionID)
	if err := c.doJSON(ctx, http.MethodPost, path, request, &plan); err != nil {
		return nil, err
	}
	if plan.ImplementationID != id || plan.RevisionID != revisionID || plan.MetricDefinitionID <= 0 || plan.MetricDefinitionRevisionID <= 0 || plan.EngineID == 0 || len(plan.DependencyHash) != 64 || plan.SQL == "" || len(plan.Parameters) == 0 || len(plan.Fields) == 0 || len(plan.StableKey) == 0 {
		return nil, errors.New("Model returned an invalid metric plan")
	}
	return &plan, nil
}
