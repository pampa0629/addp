package shapefile

import (
	"fmt"

	"github.com/jonas-p/go-shp"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/wkt"
)

// ShapeToWKT 将 shapefile 几何转换为 WKT 格式。
func ShapeToWKT(shape shp.Shape) (string, error) {
	geom, err := ShapeToGeom(shape)
	if err != nil {
		return "", err
	}

	wkt, err := geomToWKT(geom)
	if err != nil {
		return "", fmt.Errorf("failed to convert geometry to WKT: %w", err)
	}

	return wkt, nil
}

func geomToWKT(geometry geom.T) (string, error) {
	return wkt.Marshal(geometry)
}

func determineShapefileGeometryType(shapeType shp.ShapeType) string {
	switch shapeType {
	case shp.POINT, shp.POINTZ, shp.POINTM:
		return "Point"
	case shp.POLYLINE, shp.POLYLINEZ, shp.POLYLINEM:
		return "LineString"
	case shp.POLYGON, shp.POLYGONZ, shp.POLYGONM:
		return "Polygon"
	case shp.MULTIPOINT, shp.MULTIPOINTZ, shp.MULTIPOINTM:
		return "MultiPoint"
	default:
		return "Geometry"
	}
}

func determineShapeGeometryType(shape shp.Shape) string {
	switch shape.(type) {
	case *shp.Point, *shp.PointZ, *shp.PointM:
		return "Point"
	case *shp.PolyLine, *shp.PolyLineZ, *shp.PolyLineM:
		return "LineString"
	case *shp.Polygon, *shp.PolygonZ, *shp.PolygonM:
		return "Polygon"
	case *shp.MultiPoint, *shp.MultiPointZ, *shp.MultiPointM:
		return "MultiPoint"
	default:
		return "Geometry"
	}
}
