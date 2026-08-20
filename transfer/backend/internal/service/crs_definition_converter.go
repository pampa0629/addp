package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/addp/common/datatype"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

type directWorkflowOperatorResolver func(context.Context) (commonModels.Engine, commonModels.OperatorDescriptor, error)

type workflowCRSDefinitionConverter struct {
	resolve directWorkflowOperatorResolver

	mu           sync.Mutex
	engine       *commonModels.Engine
	operatorName string
}

func newWorkflowCRSDefinitionConverter(resolve directWorkflowOperatorResolver) *workflowCRSDefinitionConverter {
	return &workflowCRSDefinitionConverter{resolve: resolve}
}

func (p *workflowCRSDefinitionConverter) ConvertCRSDefinition(
	ctx context.Context,
	crsRef string,
	source *datatype.CRSDefinition,
	targetEncoding string,
) (*datatype.CRSDefinition, error) {
	if p == nil || p.resolve == nil {
		return nil, fmt.Errorf("CRS definition workflow converter is not configured")
	}
	if !strings.EqualFold(strings.TrimSpace(targetEncoding), datatype.CRSDefinitionEncodingPROJJSON) {
		return nil, fmt.Errorf("crs_to_projjson does not support target encoding %q", targetEncoding)
	}
	crsRef = strings.TrimSpace(crsRef)
	if crsRef == "" {
		return nil, fmt.Errorf("CRS ref is required")
	}
	params := map[string]interface{}{"crs_ref": crsRef}
	if source != nil {
		if sourceID := strings.TrimSpace(source.ID); sourceID != "" && !strings.EqualFold(sourceID, crsRef) {
			return nil, fmt.Errorf("source CRS definition id %q does not match %q", source.ID, crsRef)
		}
		params["definition_encoding"] = strings.TrimSpace(source.DefinitionEncoding)
		params["definition"] = strings.TrimSpace(source.Definition)
	}

	engine, operatorName, err := p.workflowOperator(ctx)
	if err != nil {
		return nil, err
	}
	result, err := dbbridge.InvokeOperator(ctx, &engine, operatorName, plugin.OperatorInvokeRequest{Params: params})
	if err != nil {
		return nil, err
	}
	definition, err := crsDefinitionFromOperatorResult(result)
	if err != nil {
		return nil, err
	}
	definition.Source = datatype.CRSDefinitionSourceNormalizationRuntime
	return definition, nil
}

func (p *workflowCRSDefinitionConverter) workflowOperator(ctx context.Context) (commonModels.Engine, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.engine != nil && p.operatorName != "" {
		return *p.engine, p.operatorName, nil
	}
	engine, operator, err := p.resolve(ctx)
	if err != nil {
		return commonModels.Engine{}, "", err
	}
	operatorName := strings.TrimSpace(operator.Name)
	if operatorName == "" {
		operatorName = strings.TrimSpace(operator.ID)
	}
	if operatorName == "" {
		return commonModels.Engine{}, "", fmt.Errorf("resolved CRS definition workflow operator has no name")
	}
	p.engine = &engine
	p.operatorName = operatorName
	return engine, operatorName, nil
}

func crsDefinitionFromOperatorResult(result *plugin.OperatorInvokeResult) (*datatype.CRSDefinition, error) {
	if result == nil || result.Result == nil {
		return nil, fmt.Errorf("crs_to_projjson returned empty result")
	}
	raw, ok := result.Result["result"]
	if !ok || raw == nil {
		return nil, fmt.Errorf("crs_to_projjson returned empty result")
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal crs_to_projjson result: %w", err)
	}
	var output struct {
		CRSRef             string `json:"crs_ref"`
		DefinitionEncoding string `json:"definition_encoding"`
		Definition         string `json:"definition"`
	}
	if err := json.Unmarshal(payload, &output); err != nil {
		return nil, fmt.Errorf("decode crs_to_projjson result: %w", err)
	}
	output.CRSRef = strings.TrimSpace(output.CRSRef)
	output.DefinitionEncoding = strings.TrimSpace(output.DefinitionEncoding)
	output.Definition = strings.TrimSpace(output.Definition)
	if output.CRSRef == "" || output.DefinitionEncoding == "" || output.Definition == "" {
		return nil, fmt.Errorf("crs_to_projjson returned incomplete CRS definition")
	}
	var projJSON map[string]interface{}
	if err := json.Unmarshal([]byte(output.Definition), &projJSON); err != nil || len(projJSON) == 0 {
		return nil, fmt.Errorf("crs_to_projjson returned invalid PROJJSON object")
	}
	if value, ok := projJSON["type"].(string); !ok || strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("crs_to_projjson returned PROJJSON without type")
	}
	return &datatype.CRSDefinition{
		ID:                 output.CRSRef,
		DefinitionEncoding: output.DefinitionEncoding,
		Definition:         output.Definition,
	}, nil
}
