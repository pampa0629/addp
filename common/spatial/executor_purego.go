package spatial

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/twpayne/go-geom"
	geomjson "github.com/twpayne/go-geom/encoding/geojson"
)

type pureGoExecutor struct{}

func (pureGoExecutor) Name() string {
	return "pure_go"
}

func (pureGoExecutor) CanTransform(sourceCRS, targetCRS CRS) bool {
	return canUsePureGoTransform(sourceCRS.SRID, targetCRS.SRID)
}

func (pureGoExecutor) TransformGeoJSON(_ context.Context, payload interface{}, sourceCRS, targetCRS CRS) (interface{}, error) {
	return transformGeoJSONNode(payload, func(geometry map[string]interface{}) (map[string]interface{}, error) {
		return transformGeometryPureGo(geometry, sourceCRS.SRID, targetCRS.SRID)
	})
}

func canUsePureGoTransform(sourceSRID, targetSRID int) bool {
	return (sourceSRID == SRIDWGS84 && targetSRID == SRIDWebMercator) ||
		(sourceSRID == SRIDWebMercator && targetSRID == SRIDWGS84)
}

func transformGeometryPureGo(geometryMap map[string]interface{}, sourceSRID, targetSRID int) (map[string]interface{}, error) {
	raw, err := json.Marshal(geometryMap)
	if err != nil {
		return nil, err
	}

	var geometry geom.T
	if err := geomjson.Unmarshal(raw, &geometry); err != nil {
		return nil, err
	}

	if sourceSRID > 0 && geometry.SRID() == 0 {
		if updated, setErr := geom.SetSRID(geometry, sourceSRID); setErr == nil {
			geometry = updated
		}
	}

	transformer, err := getCoordTransformer(sourceSRID, targetSRID)
	if err != nil {
		return nil, err
	}

	transformed, err := transformGeomCoordinates(geometry, transformer)
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

type coordTransformer interface {
	Transform(x, y float64) (float64, float64, error)
}

type wgs84ToWebMercator struct{}

func (t *wgs84ToWebMercator) Transform(lon, lat float64) (float64, float64, error) {
	if lon < -180 || lon > 180 {
		return 0, 0, fmt.Errorf("longitude out of range: %f", lon)
	}
	if lat < -85.05112878 || lat > 85.05112878 {
		return 0, 0, fmt.Errorf("latitude out of range: %f", lat)
	}
	x := lon * 20037508.34 / 180.0
	y := math.Log(math.Tan((90.0+lat)*math.Pi/360.0)) / (math.Pi / 180.0)
	y = y * 20037508.34 / 180.0
	return x, y, nil
}

type webMercatorToWGS84 struct{}

func (t *webMercatorToWGS84) Transform(x, y float64) (float64, float64, error) {
	maxExtent := 20037508.34
	if x < -maxExtent || x > maxExtent {
		return 0, 0, fmt.Errorf("x out of range: %f", x)
	}
	if y < -maxExtent || y > maxExtent {
		return 0, 0, fmt.Errorf("y out of range: %f", y)
	}
	lon := (x / 20037508.34) * 180.0
	lat := (y / 20037508.34) * 180.0
	lat = 180.0 / math.Pi * (2.0*math.Atan(math.Exp(lat*math.Pi/180.0)) - math.Pi/2.0)
	return lon, lat, nil
}

func getCoordTransformer(sourceSRID, targetSRID int) (coordTransformer, error) {
	switch {
	case sourceSRID == SRIDWGS84 && targetSRID == SRIDWebMercator:
		return &wgs84ToWebMercator{}, nil
	case sourceSRID == SRIDWebMercator && targetSRID == SRIDWGS84:
		return &webMercatorToWGS84{}, nil
	default:
		return nil, fmt.Errorf("unsupported transformation EPSG:%d -> EPSG:%d", sourceSRID, targetSRID)
	}
}

func transformGeomCoordinates(input geom.T, transformer coordTransformer) (geom.T, error) {
	switch g := input.(type) {
	case *geom.Point:
		coord := g.Coords()
		x, y, err := transformer.Transform(coord.X(), coord.Y())
		if err != nil {
			return nil, err
		}
		return geom.NewPoint(g.Layout()).MustSetCoords(geom.Coord{x, y}), nil
	case *geom.LineString:
		coords := make([]geom.Coord, g.NumCoords())
		for i := 0; i < g.NumCoords(); i++ {
			coord := g.Coord(i)
			x, y, err := transformer.Transform(coord.X(), coord.Y())
			if err != nil {
				return nil, err
			}
			coords[i] = geom.Coord{x, y}
		}
		return geom.NewLineString(g.Layout()).MustSetCoords(coords), nil
	case *geom.Polygon:
		rings := make([][]geom.Coord, g.NumLinearRings())
		for i := 0; i < g.NumLinearRings(); i++ {
			ring := g.LinearRing(i)
			coords := make([]geom.Coord, ring.NumCoords())
			for j := 0; j < ring.NumCoords(); j++ {
				coord := ring.Coord(j)
				x, y, err := transformer.Transform(coord.X(), coord.Y())
				if err != nil {
					return nil, err
				}
				coords[j] = geom.Coord{x, y}
			}
			rings[i] = coords
		}
		return geom.NewPolygon(g.Layout()).MustSetCoords(rings), nil
	case *geom.MultiPoint:
		coords := make([]geom.Coord, g.NumPoints())
		for i := 0; i < g.NumPoints(); i++ {
			coord := g.Point(i).Coords()
			x, y, err := transformer.Transform(coord.X(), coord.Y())
			if err != nil {
				return nil, err
			}
			coords[i] = geom.Coord{x, y}
		}
		return geom.NewMultiPoint(g.Layout()).MustSetCoords(coords), nil
	case *geom.MultiLineString:
		lines := make([][]geom.Coord, g.NumLineStrings())
		for i := 0; i < g.NumLineStrings(); i++ {
			line := g.LineString(i)
			coords := make([]geom.Coord, line.NumCoords())
			for j := 0; j < line.NumCoords(); j++ {
				coord := line.Coord(j)
				x, y, err := transformer.Transform(coord.X(), coord.Y())
				if err != nil {
					return nil, err
				}
				coords[j] = geom.Coord{x, y}
			}
			lines[i] = coords
		}
		return geom.NewMultiLineString(g.Layout()).MustSetCoords(lines), nil
	case *geom.MultiPolygon:
		polygons := make([][][]geom.Coord, g.NumPolygons())
		for i := 0; i < g.NumPolygons(); i++ {
			polygon := g.Polygon(i)
			rings := make([][]geom.Coord, polygon.NumLinearRings())
			for j := 0; j < polygon.NumLinearRings(); j++ {
				ring := polygon.LinearRing(j)
				coords := make([]geom.Coord, ring.NumCoords())
				for k := 0; k < ring.NumCoords(); k++ {
					coord := ring.Coord(k)
					x, y, err := transformer.Transform(coord.X(), coord.Y())
					if err != nil {
						return nil, err
					}
					coords[k] = geom.Coord{x, y}
				}
				rings[j] = coords
			}
			polygons[i] = rings
		}
		return geom.NewMultiPolygon(g.Layout()).MustSetCoords(polygons), nil
	case *geom.GeometryCollection:
		items := make([]geom.T, g.NumGeoms())
		for i := 0; i < g.NumGeoms(); i++ {
			transformed, err := transformGeomCoordinates(g.Geom(i), transformer)
			if err != nil {
				return nil, err
			}
			items[i] = transformed
		}
		collection := geom.NewGeometryCollection()
		if err := collection.Push(items...); err != nil {
			return nil, err
		}
		return collection, nil
	default:
		return nil, fmt.Errorf("unsupported geometry type: %T", input)
	}
}
