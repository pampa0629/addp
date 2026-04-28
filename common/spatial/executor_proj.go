//go:build proj

package spatial

import (
	"context"
	"encoding/json"
	"fmt"

	projbridge "github.com/addp/common/spatial/internal/proj"
	"github.com/twpayne/go-geom"
	geomjson "github.com/twpayne/go-geom/encoding/geojson"
)

type projExecutor struct{}

func (projExecutor) Name() string {
	return "proj"
}

func (projExecutor) CanTransform(sourceCRS, targetCRS CRS) bool {
	return sourceCRS.Text != "" && targetCRS.Text != ""
}

func (projExecutor) TransformGeoJSON(_ context.Context, payload interface{}, sourceCRS, targetCRS CRS) (interface{}, error) {
	transformer, err := projbridge.NewTransformer(sourceCRS.Text, targetCRS.Text)
	if err != nil {
		return nil, err
	}
	defer transformer.Close()

	return transformGeoJSONNode(payload, func(geometry map[string]interface{}) (map[string]interface{}, error) {
		return transformGeometryWithPROJ(transformer, geometry, targetCRS.SRID)
	})
}

func transformGeometryWithPROJ(transformer *projbridge.Transformer, geometryMap map[string]interface{}, targetSRID int) (map[string]interface{}, error) {
	raw, err := json.Marshal(geometryMap)
	if err != nil {
		return nil, err
	}

	var geometry geom.T
	if err := geomjson.Unmarshal(raw, &geometry); err != nil {
		return nil, err
	}

	transformed, err := transformGeomWithFlatCoords(geometry, func(flatCoords []float64, stride int) ([]float64, error) {
		return transformer.TransformFlatCoords(flatCoords, stride)
	})
	if err != nil {
		return nil, err
	}

	if targetSRID > 0 {
		if updated, setErr := geom.SetSRID(transformed, targetSRID); setErr == nil {
			transformed = updated
		}
	}

	output, err := geomjson.Marshal(transformed)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func transformGeomWithFlatCoords(input geom.T, transformFlatCoords func(flatCoords []float64, stride int) ([]float64, error)) (geom.T, error) {
	switch g := input.(type) {
	case *geom.Point:
		flat, err := transformFlatCoords(g.FlatCoords(), g.Stride())
		if err != nil {
			return nil, err
		}
		return geom.NewPointFlat(g.Layout(), flat), nil
	case *geom.LineString:
		flat, err := transformFlatCoords(g.FlatCoords(), g.Stride())
		if err != nil {
			return nil, err
		}
		return geom.NewLineStringFlat(g.Layout(), flat), nil
	case *geom.Polygon:
		flat, err := transformFlatCoords(g.FlatCoords(), g.Stride())
		if err != nil {
			return nil, err
		}
		return geom.NewPolygonFlat(g.Layout(), flat, append([]int(nil), g.Ends()...)), nil
	case *geom.MultiPoint:
		flat, err := transformFlatCoords(g.FlatCoords(), g.Stride())
		if err != nil {
			return nil, err
		}
		return geom.NewMultiPointFlat(g.Layout(), flat, geom.NewMultiPointFlatOptionWithEnds(append([]int(nil), g.Ends()...))), nil
	case *geom.MultiLineString:
		flat, err := transformFlatCoords(g.FlatCoords(), g.Stride())
		if err != nil {
			return nil, err
		}
		return geom.NewMultiLineStringFlat(g.Layout(), flat, append([]int(nil), g.Ends()...)), nil
	case *geom.MultiPolygon:
		flat, err := transformFlatCoords(g.FlatCoords(), g.Stride())
		if err != nil {
			return nil, err
		}
		return geom.NewMultiPolygonFlat(g.Layout(), flat, cloneEndss(g.Endss())), nil
	case *geom.GeometryCollection:
		items := make([]geom.T, g.NumGeoms())
		for i := 0; i < g.NumGeoms(); i++ {
			transformed, err := transformGeomWithFlatCoords(g.Geom(i), transformFlatCoords)
			if err != nil {
				return nil, err
			}
			items[i] = transformed
		}
		collection := geom.NewGeometryCollection()
		if layout := g.Layout(); layout != geom.NoLayout {
			if err := collection.SetLayout(layout); err != nil {
				return nil, err
			}
		}
		if err := collection.Push(items...); err != nil {
			return nil, err
		}
		return collection, nil
	default:
		return nil, fmt.Errorf("unsupported geometry type: %T", input)
	}
}
