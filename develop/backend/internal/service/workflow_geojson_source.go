package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
)

const maxWorkflowGeoJSONBytes = 8 << 20

func (s *WorkflowEngineService) readWorkflowGeoJSON(ctx context.Context, tenant uint, node string, target plugin.EngineCatalogPath, resolver *workflowEngineAccessResolver) (json.RawMessage, error) {
	lease, ok := execution.LeaseFromContext(ctx)
	if !ok || lease.ExecutionID != resolver.executionID.String() || lease.TenantID != int(tenant) {
		return nil, errors.New("GeoJSON source requires the current workflow execution lease")
	}
	request := client.WorkflowGeoJSONAccessRequest{ExecutionID: lease.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token, NodeID: node}
	observe := func() (*client.ManagerFileAccessObservation, error) {
		return s.systemService.WithTenantID(tenant).CheckWorkflowGeoJSONAccess(ctx, request)
	}
	first, err := observe()
	if err != nil {
		return nil, err
	}
	if first.Engine.ID != target.EngineID || first.Engine.EngineType != "minio" {
		return nil, errors.New("GeoJSON source observation mismatch")
	}
	registered, err := plugin.Get("minio")
	if err != nil {
		return nil, err
	}
	provider, ok := registered.(interface {
		plugin.ContentReadableProvider
		plugin.EngineCatalogFactsProvider
	})
	if !ok {
		return nil, errors.New("GeoJSON source provider unavailable")
	}
	end, err := s.protectionGate.BeginCatalogPath(ctx, tenant, registered, target)
	if err != nil {
		return nil, err
	}
	defer end()
	version, err := provider.DescribeEngineCatalogFacts(ctx, plugin.ConnectionInfo(first.Engine.ConnectionInfo), target, plugin.EngineCatalogFactsOptions{})
	if err != nil || version == nil || version.Storage == nil || version.Storage.SizeBytes == nil || *version.Storage.SizeBytes <= 0 || *version.Storage.SizeBytes > maxWorkflowGeoJSONBytes || version.Storage.ETag == "" {
		return nil, errors.New("GeoJSON source size or revision unavailable")
	}
	reader, err := provider.OpenContent(ctx, plugin.ConnectionInfo(first.Engine.ConnectionInfo), target, plugin.ReadOptions{})
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, maxWorkflowGeoJSONBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(data)) != *version.Storage.SizeBytes {
		return nil, errors.New("GeoJSON source read incomplete")
	}
	second, err := observe()
	if err != nil {
		return nil, err
	}
	if second.Engine.ID != first.Engine.ID || second.Engine.Version != first.Engine.Version {
		return nil, errors.New("GeoJSON source engine changed")
	}
	current, err := provider.DescribeEngineCatalogFacts(ctx, plugin.ConnectionInfo(second.Engine.ConnectionInfo), target, plugin.EngineCatalogFactsOptions{})
	if err != nil || !reflect.DeepEqual(current, version) {
		return nil, errors.New("GeoJSON source changed during read")
	}
	var collection struct {
		Type     string            `json:"type"`
		Features []json.RawMessage `json:"features"`
		CRS      json.RawMessage   `json:"crs"`
	}
	if json.Unmarshal(data, &collection) != nil || collection.Type != "FeatureCollection" || collection.Features == nil || len(collection.CRS) != 0 {
		return nil, errors.New("GeoJSON source must be a WGS84 FeatureCollection")
	}
	return json.RawMessage(data), nil
}
