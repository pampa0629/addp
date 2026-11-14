package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/exportartifact"
	"github.com/addp/common/format"
	"github.com/addp/develop/backend/internal/models"
)

func (s *QueryExportService) createWorkflowExport(ctx context.Context, id string, req models.CreateQueryExportRequest, tenant, user uint) (*models.CreateQueryExportResponse, error) {
	if f := strings.ToLower(strings.TrimSpace(req.Format)); f != "" && f != "csv" {
		return nil, ErrQueryExportInvalid
	}
	bearer, _ := ctx.Value(exportUserTokenKey{}).(string)
	response, err := s.executor.GetQueryResult(ctx, id, tenant, bearer)
	if err != nil {
		return nil, ErrQueryExportNotFound
	}
	facts, _ := executionUserFacts(ctx)
	_, actor, _, _ := executionActorIDs(facts)
	entry := s.executor.workflowResults.get(id, response.Attempt, tenant, actor)
	if entry == nil {
		return nil, ErrQueryExportNotFound
	}
	var table plugin.WorkflowTableResult
	decoder := json.NewDecoder(strings.NewReader(string(entry.data)))
	decoder.UseNumber()
	if decoder.Decode(&table) != nil {
		return nil, ErrQueryExportInvalid
	}
	data, err := client.WorkflowResultExportRows(&table)
	if err != nil {
		return nil, ErrQueryExportInvalid
	}
	targets := make([]dataprotection.ResourceReference, 0, len(entry.sources))
	for _, source := range entry.sources {
		provider, ok := source.Provider.(plugin.EngineCatalogModelProvider)
		if !ok {
			return nil, ErrQueryExportInvalid
		}
		target, err := dataprotection.DataItemTargetFromCatalogPath(provider.EngineCatalogModel(), source.Target)
		if err != nil {
			return nil, ErrQueryExportInvalid
		}
		targets = append(targets, target)
	}
	binding := &client.WorkflowResultExportSource{ExecutionID: id, Attempt: response.Attempt, ExpiresAt: entry.expires, SHA256: client.WorkflowResultRowsDigest(data), Columns: table.Columns, RowCount: table.RowCount, Targets: targets}
	created, err := s.artifacts.Create(ctx, exportartifact.CreateRequest{TenantID: tenant, UserID: user, SourceRef: id, Format: format.FormatCSV, FileName: strings.TrimSpace(req.FileName), ExecutionName: "develop_workflow_export_" + id, WorkflowTable: &table,
		ExecutionConfig: client.TransferExecutionConfig{WorkflowResult: binding, Runtime: client.TransferExecutionRuntime{Boundary: "bounded"}, Load: client.TransferExecutionLoad{Mode: "snapshot"}, Target: client.TransferExecutionEndpoint{DataType: "table", Representation: "encoded", Format: "csv", Policy: map[string]interface{}{"apply_mode": "replace"}}, Transforms: queryExportIdentityProjection(table.Columns)}})
	if err != nil {
		return nil, fmt.Errorf("%w: workflow export could not be created", ErrQueryExportUnavailable)
	}
	return queryExportResponse(created), nil
}

type workflowExportDeadlineReader struct {
	io.ReadCloser
	expires time.Time
}

func (r workflowExportDeadlineReader) Read(p []byte) (int, error) {
	if !time.Now().Before(r.expires) {
		return 0, ErrWorkflowResultUnavailable
	}
	return r.ReadCloser.Read(p)
}
