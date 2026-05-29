package metaattr

import (
	"testing"

	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
)

func TestMediaInfoAttributesWritesTypeInfoAndSpatial(t *testing.T) {
	t.Parallel()

	duration := int64(1234)
	size := int64(4567)
	srid := 4326
	hasSpatialIndex := false
	extent := datatype.BoundingBox{100, 180, 120, 200}
	attrs := MediaInfoAttributes(&datatype.MediaInfo{
		Kind:       datatype.MediaKindImage,
		MIMEType:   "image/tiff",
		Width:      800,
		Height:     600,
		DurationMS: &duration,
		Encoding:   "tiff",
		ColorSpace: "RGB",
		SizeBytes:  &size,
	}, &datatype.SpatialInfo{
		GeometryColumns: []datatype.GeometryColumnInfo{{SRID: &srid}},
		Extent:          &extent,
		HasSpatialIndex: &hasSpatialIndex,
	})

	media := commonJSON.Section(attrs, "type_info.media")
	if media["kind"] != "image" || media["width"] != 800 || media["height"] != 600 {
		t.Fatalf("type_info.media = %#v", media)
	}
	if media["duration_ms"] != duration || media["encoding"] != "tiff" || media["mime_type"] != "image/tiff" {
		t.Fatalf("type_info.media = %#v", media)
	}
	if _, ok := media["srid"]; ok {
		t.Fatalf("type_info.media should not contain spatial facts: %#v", media)
	}
	if _, ok := media["extent"]; ok {
		t.Fatalf("type_info.media should not contain spatial facts: %#v", media)
	}
	capabilities := attrs["capabilities"].(map[string]interface{})
	spatial := capabilities["spatial"].(map[string]interface{})
	if spatial["srid"] != 4326 || spatial["has_spatial_index"] != false {
		t.Fatalf("capabilities.spatial = %#v", spatial)
	}
	if attrs["spatial"] != nil {
		t.Fatalf("flat spatial attr should not be written: %#v", attrs)
	}
}
