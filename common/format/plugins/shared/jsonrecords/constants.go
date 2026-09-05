package jsonrecords

import "errors"

const DefaultGeometryField = "geometry"

var ErrNotRecordCollection = errors.New("json table: root object is not a record collection")

const (
	StructureDocument          = "document"
	StructureGeoJSONFeatureSet = "geojson_feature_collection"
	StructureJSONLines         = "json_lines"
	StructureObjectArray       = "object_array"
)
