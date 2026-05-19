package shapefile

import (
	"fmt"

	commonSpatial "github.com/addp/common/spatial"
	"github.com/jonas-p/go-shp"
)

func shapeToWKT(shape shp.Shape) (string, error) {
	geom, err := shapeToGeom(shape)
	if err != nil {
		return "", err
	}

	wkt, err := commonSpatial.GeomToWKT(geom)
	if err != nil {
		return "", fmt.Errorf("failed to convert geometry to WKT: %w", err)
	}

	return wkt, nil
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
