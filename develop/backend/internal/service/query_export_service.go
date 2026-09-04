package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/exportartifact"
	"github.com/addp/common/format"
	"github.com/addp/common/resourcetree"
	"github.com/addp/develop/backend/internal/models"
)

var (
	ErrQueryExportInvalid     = errors.New("invalid query export")
	ErrQueryExportNotFound    = errors.New("query execution not found")
	ErrQueryExportUnavailable = errors.New("query export unavailable")
)

type QueryExportService struct {
	executor  *DevExecutor
	artifacts *exportartifact.Service
}

func NewQueryExportService(executor *DevExecutor, artifacts *exportartifact.Service) *QueryExportService {
	return &QueryExportService{executor: executor, artifacts: artifacts}
}

func (s *QueryExportService) Create(ctx context.Context, executionID string, req models.CreateQueryExportRequest, tenantID, userID uint) (*models.CreateQueryExportResponse, error) {
	if s == nil || s.executor == nil || s.executor.taskExecutionRepo == nil || s.artifacts == nil {
		return nil, fmt.Errorf("%w: service is not configured", ErrQueryExportUnavailable)
	}
	execution, err := s.executor.taskExecutionRepo.GetByExecutionID(ctx, strings.TrimSpace(executionID), int(tenantID))
	if err != nil || execution.Module != commonExecution.ModuleDevelop || execution.TaskType != commonExecution.TaskTypeQuery {
		return nil, ErrQueryExportNotFound
	}
	if execution.Status != commonExecution.ExecutionStatusSuccess {
		return nil, fmt.Errorf("%w: only successful query executions can be exported", ErrQueryExportInvalid)
	}
	result, ok := mapValue(execution.Metadata["result"])
	if !ok || strings.TrimSpace(interfaceString(result["result_kind"])) != "table" {
		return nil, fmt.Errorf("%w: only tabular query results can be exported", ErrQueryExportInvalid)
	}
	columns := stringSliceValue(result["columns"])
	if len(columns) == 0 {
		return nil, fmt.Errorf("%w: query result has no columns", ErrQueryExportInvalid)
	}
	content, ok := mapValue(execution.ExecutionConfig["content"])
	if !ok {
		return nil, fmt.Errorf("%w: query execution content is unavailable", ErrQueryExportInvalid)
	}
	query := strings.TrimSpace(interfaceString(content["query"]))
	language := strings.ToLower(strings.TrimSpace(interfaceString(content["query_type"])))
	if query == "" || language == "" {
		return nil, fmt.Errorf("%w: query execution snapshot is incomplete", ErrQueryExportInvalid)
	}
	inputs, _ := mapValue(execution.ExecutionConfig["inputs"])
	effectiveParameters, _ := mapValue(inputs["effective_parameters"])
	effectiveInputs, _ := mapValue(inputs["effective_inputs"])
	task := &models.DevTask{DevType: commonExecution.TaskTypeQuery, Content: models.DevTaskContent(content), ExecutionConfig: models.DevTaskContent(execution.ExecutionConfig)}
	if execution.SourceTaskID == nil {
		task, err = s.executor.compileRelationQueryPreview(ctx, task, effectiveInputs, tenantID)
		if err != nil {
			return nil, fmt.Errorf("%w: compile frozen query inputs: %v", ErrQueryExportInvalid, err)
		}
		query = strings.TrimSpace(interfaceString(task.Content["query"]))
	}
	queryInputs := queryExportInputs(effectiveInputs)
	sourceLocator := queryExportSourceLocator(content, queryInputs)
	if sourceLocator == "" {
		return nil, fmt.Errorf("%w: query execution source locator is unavailable", ErrQueryExportInvalid)
	}
	queryEngineID, ok := positiveInt(execution.ExecutionConfig["engine_id"])
	parsedSource, parseErr := resourcetree.ParseURI(sourceLocator)
	if !ok || parseErr != nil || parsedSource.EngineID != uint(queryEngineID) {
		return nil, fmt.Errorf("%w: federated or unresolved query engine", ErrQueryExportInvalid)
	}
	formatType := format.FormatType(strings.ToLower(strings.TrimSpace(req.Format)))
	if formatType == "" {
		formatType = format.FormatCSV
	}
	if formatType != format.FormatCSV {
		return nil, fmt.Errorf("%w: unsupported query export format", ErrQueryExportInvalid)
	}
	created, err := s.artifacts.Create(ctx, exportartifact.CreateRequest{
		TenantID: tenantID, UserID: userID, SourceRef: execution.ExecutionID, Format: formatType,
		FileName: strings.TrimSpace(req.FileName), ExecutionName: "develop_query_export_" + execution.ExecutionID,
		ExecutionConfig: commonClient.TransferExecutionConfig{
			Runtime: commonClient.TransferExecutionRuntime{Boundary: commonExecution.ExecutionBoundaryBounded},
			Load:    commonClient.TransferExecutionLoad{Mode: "snapshot"},
			Source: commonClient.TransferExecutionEndpoint{
				Locator: sourceLocator, DataType: "table", Representation: "native",
				Query: &commonClient.TransferExecutionQuery{Language: language, Statement: query, Parameters: effectiveParameters, Inputs: queryInputs},
			},
			Target:     commonClient.TransferExecutionEndpoint{DataType: "table", Representation: "encoded", Format: string(formatType), Policy: map[string]interface{}{"apply_mode": "replace"}},
			Transforms: queryExportIdentityProjection(columns),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrQueryExportUnavailable, err)
	}
	return queryExportResponse(created), nil
}

func queryExportIdentityProjection(columns []string) []commonClient.TransferExecutionTransform {
	fields := make([]commonClient.TransferExecutionFieldMapping, 0, len(columns))
	for _, column := range columns {
		name := strings.TrimSpace(column)
		fields = append(fields, commonClient.TransferExecutionFieldMapping{
			Source: name, Target: name, TargetType: "unknown", Nullable: true,
		})
	}
	return []commonClient.TransferExecutionTransform{{
		Type: "field_mapping", Version: "v1", Mode: "project", Fields: fields,
	}}
}

func (s *QueryExportService) Get(ctx context.Context, id, tenantID, userID uint) (*models.CreateQueryExportResponse, error) {
	response, err := s.artifacts.Get(ctx, id, tenantID, userID)
	if err != nil {
		return nil, err
	}
	return queryExportResponse(response), nil
}

func (s *QueryExportService) Open(ctx context.Context, id, tenantID, userID uint) (*exportartifact.File, error) {
	return s.artifacts.Open(ctx, id, tenantID, userID)
}

func queryExportResponse(response *exportartifact.SessionResponse) *models.CreateQueryExportResponse {
	if response == nil {
		return nil
	}
	return &models.CreateQueryExportResponse{
		ID: response.ID, Format: response.Format, FileName: response.FileName,
		TransferExecutionID: response.TransferExecutionID, Status: response.Status,
		ErrorMessage: response.ErrorMessage, DownloadURL: response.DownloadURL,
		CreatedAt: response.CreatedAt, UpdatedAt: response.UpdatedAt,
	}
}

func queryExportInputs(effectiveInputs map[string]interface{}) []commonClient.TransferExecutionQueryInput {
	inputs := make([]commonClient.TransferExecutionQueryInput, 0, len(effectiveInputs))
	for name := range effectiveInputs {
		relation, ok := mapValue(effectiveInputs[name])
		if !ok {
			continue
		}
		locator := strings.TrimSpace(interfaceString(relation["locator"]))
		if locator == "" {
			continue
		}
		inputs = append(inputs, commonClient.TransferExecutionQueryInput{Name: strings.TrimSpace(name), Locator: locator})
	}
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].Name == inputs[j].Name {
			return inputs[i].Locator < inputs[j].Locator
		}
		return inputs[i].Name < inputs[j].Name
	})
	return inputs
}

func queryExportSourceLocator(content map[string]interface{}, inputs []commonClient.TransferExecutionQueryInput) string {
	if locator := strings.TrimSpace(interfaceString(content["target_locator"])); locator != "" {
		return locator
	}
	if len(inputs) > 0 {
		return inputs[0].Locator
	}
	return ""
}

func stringSliceValue(value interface{}) []string {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...)
	case []interface{}:
		result := make([]string, 0, len(items))
		for _, item := range items {
			if text := strings.TrimSpace(interfaceString(item)); text != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func interfaceString(value interface{}) string {
	text, _ := value.(string)
	return text
}
