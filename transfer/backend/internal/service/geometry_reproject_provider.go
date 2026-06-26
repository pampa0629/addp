package service

import (
	"context"
	"fmt"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	commonSpatial "github.com/addp/common/spatial"
)

type workflowGeometryBatchReprojectProvider struct {
	engine       commonModels.Engine
	operatorName string
}

func newWorkflowGeometryBatchReprojectProvider(engine commonModels.Engine, operatorName string) *workflowGeometryBatchReprojectProvider {
	return &workflowGeometryBatchReprojectProvider{
		engine:       engine,
		operatorName: operatorName,
	}
}

func (p *workflowGeometryBatchReprojectProvider) ReprojectGeometryBatch(ctx context.Context, geometries [][]byte, sourceCRS, targetCRS, geometryColumn string) ([][]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("geometry batch reproject provider is nil")
	}
	payload, err := commonSpatial.EncodeGeometryBatchArrow(geometries, commonSpatial.GeometryBatchArrowOptions{
		GeometryColumn:   geometryColumn,
		GeometryEncoding: commonSpatial.GeometryBatchArrowEncodingEWKB,
		SourceCRS:        sourceCRS,
		TargetCRS:        targetCRS,
	})
	if err != nil {
		return nil, fmt.Errorf("encode geometry batch: %w", err)
	}

	result, err := dbbridge.InvokeOperator(ctx, &p.engine, p.operatorName, plugin.OperatorInvokeRequest{
		Params: map[string]interface{}{
			"source_crs": sourceCRS,
			"target_crs": targetCRS,
		},
		BinaryPayload: &plugin.BinaryPayload{
			ContentType: "application/vnd.apache.arrow.stream",
			Encoding:    "arrow",
			Name:        "geometry_batch",
			Data:        payload,
			Metadata: map[string]interface{}{
				"geometry_column":   geometryColumn,
				"geometry_encoding": "ewkb",
				"source_crs":        sourceCRS,
				"target_crs":        targetCRS,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if result == nil || result.BinaryPayload == nil {
		return nil, fmt.Errorf("vector_reproject returned empty binary payload")
	}
	decoded, err := commonSpatial.DecodeGeometryBatchArrow(result.BinaryPayload.Data)
	if err != nil {
		return nil, err
	}
	if decoded.GeometryEncoding != commonSpatial.GeometryBatchArrowEncodingEWKB {
		return nil, fmt.Errorf("vector_reproject returned unsupported geometry encoding %q", decoded.GeometryEncoding)
	}
	return commonSpatial.EncodeGeometryBytesAsEWKB(decoded.Geometries, commonSpatial.ParseSRID(targetCRS))
}
