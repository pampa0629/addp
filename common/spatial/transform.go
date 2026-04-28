package spatial

import (
	"context"
	"fmt"
)

const (
	SRIDWGS84       = 4326
	SRIDWebMercator = 3857
)

type TransformStatus string

const (
	TransformStatusNoop           TransformStatus = "noop"
	TransformStatusTransformed    TransformStatus = "transformed"
	TransformStatusUnknownCRS     TransformStatus = "unknown_crs"
	TransformStatusUnsupportedCRS TransformStatus = "unsupported_crs"
)

type GeoJSONTransformRequest struct {
	GeoJSON    interface{}
	SourceSRID int
	SourceCRS  string
	TargetSRID int
}

type GeoJSONTransformResult struct {
	GeoJSON     interface{}     `json:"geojson,omitempty"`
	SourceSRID  int             `json:"source_srid,omitempty"`
	SourceCRS   string          `json:"source_crs,omitempty"`
	TargetSRID  int             `json:"target_srid,omitempty"`
	Status      TransformStatus `json:"status"`
	Engine      string          `json:"engine,omitempty"`
	Message     string          `json:"message,omitempty"`
	BoundingBox []float64       `json:"bbox,omitempty"`
}

func TransformGeoJSONToWGS84(ctx context.Context, geojson interface{}, sourceSRID int, sourceCRS string) (*GeoJSONTransformResult, error) {
	return TransformGeoJSON(ctx, GeoJSONTransformRequest{
		GeoJSON:    geojson,
		SourceSRID: sourceSRID,
		SourceCRS:  sourceCRS,
		TargetSRID: SRIDWGS84,
	})
}

func TransformGeoJSON(ctx context.Context, req GeoJSONTransformRequest) (*GeoJSONTransformResult, error) {
	targetCRS := normalizeTargetCRS(req.TargetSRID)

	normalized, err := normalizeGeoJSONPayload(req.GeoJSON)
	if err != nil {
		return nil, fmt.Errorf("normalize geojson failed: %w", err)
	}

	sourceCRS := normalizeSourceCRS(req.SourceCRS, req.SourceSRID)
	result := &GeoJSONTransformResult{
		SourceSRID: req.SourceSRID,
		SourceCRS:  sourceCRS.Text,
		TargetSRID: targetCRS.SRID,
	}

	if isNoopTransform(sourceCRS, targetCRS) {
		result.Status = TransformStatusNoop
		result.Engine = "none"
		result.GeoJSON = normalized
		result.BoundingBox = extractGeoJSONBoundingBox(normalized)
		return result, nil
	}

	if sourceCRS.IsZero() {
		result.Status = TransformStatusUnknownCRS
		result.Message = "源坐标系未知，已跳过地图渲染"
		return result, nil
	}

	executor := resolveTransformExecutor(sourceCRS, targetCRS)
	if executor == nil {
		result.Status = TransformStatusUnsupportedCRS
		result.Message = fmt.Sprintf("暂不支持将 %s 转为 %s", sourceCRS.Label(), targetCRS.Label())
		return result, nil
	}

	transformed, transformErr := executor.TransformGeoJSON(ctx, normalized, sourceCRS, targetCRS)
	if transformErr != nil {
		result.Status = TransformStatusUnsupportedCRS
		result.Engine = executor.Name()
		result.Message = fmt.Sprintf("坐标转换失败: %v", transformErr)
		return result, nil
	}

	result.Status = TransformStatusTransformed
	result.Engine = executor.Name()
	result.GeoJSON = transformed
	result.BoundingBox = extractGeoJSONBoundingBox(transformed)
	return result, nil
}
