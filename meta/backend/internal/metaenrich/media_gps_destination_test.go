package metaenrich

import (
	"context"
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
)

func TestEnrichJPEGGPSDestinationSnapshotReplacement(t *testing.T) {
	rational := func(id uint16, values ...uint32) gpsAdditionalTag {
		raw := make([]byte, len(values)*4)
		for i, value := range values {
			binary.LittleEndian.PutUint32(raw[i*4:], value)
		}
		return gpsAdditionalTag{id, 5, uint32(len(values) / 2), raw}
	}
	latitudeRef := gpsAdditionalTag{19, 2, 2, []byte{'N', 0}}
	latitude := rational(20, 10, 1, 30, 1, 0, 1)
	longitudeRef := gpsAdditionalTag{21, 2, 2, []byte{'E', 0}}
	longitude := rational(22, 20, 1, 15, 1, 0, 1)
	bearingRef := gpsAdditionalTag{23, 2, 2, []byte{'T', 0}}
	bearing := rational(24, 35999, 100)
	distanceRef := gpsAdditionalTag{25, 2, 2, []byte{'N', 0}}
	distance := rational(26, 125, 100)
	clock := []gpsAdditionalTag{{29, 2, 11, []byte("2024:02:29\x00")}, {7, 5, 3, make([]byte, 24)}}
	for i, value := range []uint32{12, 1, 34, 1, 56, 1} {
		binary.LittleEndian.PutUint32(clock[1].raw[i*4:], value)
	}
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Layout: format.LayoutSingle, DataType: datatype.Media, Format: "jpeg", PrimaryContentPath: "images/gps-destination.jpg"}, PhysicalPath: "images/gps-destination.jpg"}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
	metaattr.ReplaceCapabilityNamespace(attrs, "extraction", map[string]interface{}{"status": "ready"})
	for _, tc := range []struct {
		name  string
		tags  []gpsAdditionalTag
		want  map[string]interface{}
		valid bool
	}{
		{"complete", []gpsAdditionalTag{latitudeRef, latitude, longitudeRef, longitude, bearingRef, bearing, distanceRef, distance}, map[string]interface{}{"destination_latitude_ref": "N", "destination_latitude_dms": []float64{10, 30, 0}, "destination_longitude_ref": "E", "destination_longitude_dms": []float64{20, 15, 0}, "destination_bearing_ref": "T", "destination_bearing_degrees": 359.99, "destination_distance_ref": "N", "destination_distance": 1.25}, true},
		{"refs removed", []gpsAdditionalTag{latitude, longitude, bearing, distance}, map[string]interface{}{"destination_latitude_dms": []float64{10, 30, 0}, "destination_longitude_dms": []float64{20, 15, 0}, "destination_bearing_degrees": 359.99, "destination_distance": 1.25}, true},
		{"values removed", []gpsAdditionalTag{latitudeRef, longitudeRef, bearingRef, distanceRef}, map[string]interface{}{"destination_latitude_ref": "N", "destination_longitude_ref": "E", "destination_bearing_ref": "T", "destination_distance_ref": "N"}, true},
		{"zero distance and bearing", []gpsAdditionalTag{rational(24, 0, 1), rational(26, 0, 1)}, map[string]interface{}{"destination_bearing_degrees": float64(0), "destination_distance": float64(0)}, true},
		{"invalid bearing", []gpsAdditionalTag{bearingRef, rational(24, 360, 1), distanceRef, distance}, map[string]interface{}{"destination_bearing_ref": "T", "destination_distance_ref": "N", "destination_distance": 1.25}, false},
		{"all removed", nil, map[string]interface{}{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tags := append(append([]gpsAdditionalTag{}, clock...), tc.tags...)
			_, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{ContentReader: bytesContentReader{content: gpsExtendedJPEGContent(t, tags)}, Item: item, PhysicalPath: item.PhysicalPath, EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) }})
			if err != nil {
				t.Fatal(err)
			}
			gps := commonJSON.Section(attrs, "format_info.jpeg.exif.gps")
			for _, key := range []string{"destination_latitude_ref", "destination_latitude_dms", "destination_longitude_ref", "destination_longitude_dms", "destination_bearing_ref", "destination_bearing_degrees", "destination_distance_ref", "destination_distance"} {
				if !reflect.DeepEqual(gps[key], tc.want[key]) {
					t.Fatalf("stale or missing %s: got %#v want %#v", key, gps[key], tc.want[key])
				}
			}
			status := "parsed"
			if !tc.valid {
				status = "invalid"
			}
			if commonJSON.String(attrs, "format_info.jpeg", "exif_status") != status || (len(commonJSON.Section(attrs, "capabilities.spatial.capture_location")) != 0) != tc.valid || (gps["date_time_utc"] != nil) != tc.valid {
				t.Fatalf("diagnostic/normalized facts differ: %#v", attrs)
			}
			if commonJSON.String(attrs, "capabilities.extraction", "status") != "ready" || len(commonJSON.Section(attrs, "capabilities.temporal")) != 0 || commonJSON.Section(attrs, "capabilities.spatial")["extent"] != nil || commonJSON.Section(attrs, "type_info.media")["destination_distance"] != nil || commonJSON.Section(attrs, "capabilities.spatial")["destination_location"] != nil {
				t.Fatalf("destination leaked inferred/foreign facts: %#v", attrs)
			}
		})
	}
}
