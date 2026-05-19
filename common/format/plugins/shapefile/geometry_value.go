package shapefile

import (
	"fmt"

	"github.com/addp/common/format"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/jonas-p/go-shp"
)

func shapeToRowValue(shape shp.Shape, opts *format.ParseOptions, srid int) (interface{}, error) {
	geometry, err := shapeToGeom(shape)
	if err != nil {
		return nil, err
	}

	switch geometryEncoding(opts) {
	case format.GeometryEncodingWKT:
		return commonSpatial.GeomToWKT(geometry)
	case format.GeometryEncodingWKB:
		return commonSpatial.GeomToWKB(geometry)
	case format.GeometryEncodingEWKB:
		return commonSpatial.GeomToEWKB(geometry, srid)
	default:
		return nil, fmt.Errorf("unsupported geometry encoding: %s", opts.GeometryEncoding)
	}
}

func geometryEncoding(opts *format.ParseOptions) format.GeometryEncoding {
	if opts == nil || opts.GeometryEncoding == "" {
		return format.GeometryEncodingWKT
	}
	return opts.GeometryEncoding
}

func spatialSRID(info *format.TableInfo) int {
	if info == nil || info.SpatialInfo == nil {
		return 0
	}
	return info.SpatialInfo.SRID
}

func sridFromParseOptions(opts *format.ParseOptions) int {
	if opts == nil || opts.SpatialRefSys == "" {
		return 0
	}
	return commonSpatial.ParseSRID(opts.SpatialRefSys)
}
