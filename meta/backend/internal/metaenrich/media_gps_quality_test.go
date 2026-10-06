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

func TestEnrichJPEGGPSQualitySnapshotReplacement(t *testing.T) {
	scalar := func(id uint16, numerator uint32) gpsAdditionalTag {
		raw := make([]byte, 8)
		binary.LittleEndian.PutUint32(raw, numerator)
		binary.LittleEndian.PutUint32(raw[4:], 100)
		return gpsAdditionalTag{id, 5, 1, raw}
	}
	mode := gpsAdditionalTag{10, 2, 2, []byte{'2', 0}}
	dop := scalar(11, 125)
	correction := gpsAdditionalTag{30, 3, 1, []byte{0, 0}}
	errorMeters := scalar(31, 250)
	clock := []gpsAdditionalTag{{29, 2, 11, []byte("2024:02:29\x00")}, {7, 5, 3, make([]byte, 24)}}
	for i, value := range []uint32{12, 1, 34, 1, 56, 1} {
		binary.LittleEndian.PutUint32(clock[1].raw[i*4:], value)
	}
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Layout: format.LayoutSingle, DataType: datatype.Media, Format: "jpeg", PrimaryContentPath: "images/gps-quality.jpg"}, PhysicalPath: "images/gps-quality.jpg"}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
	metaattr.ReplaceCapabilityNamespace(attrs, "extraction", map[string]interface{}{"status": "ready"})
	for _, tc := range []struct {
		name  string
		tags  []gpsAdditionalTag
		want  map[string]interface{}
		valid bool
	}{
		{"complete", []gpsAdditionalTag{mode, dop, correction, errorMeters}, map[string]interface{}{"measure_mode": "2", "dop": 1.25, "differential": 0, "horizontal_positioning_error_meters": 2.5}, true},
		{"mode removed", []gpsAdditionalTag{dop, correction, errorMeters}, map[string]interface{}{"dop": 1.25, "differential": 0, "horizontal_positioning_error_meters": 2.5}, true},
		{"values removed", []gpsAdditionalTag{mode, correction}, map[string]interface{}{"measure_mode": "2", "differential": 0}, true},
		{"3D corrected zero", []gpsAdditionalTag{{10, 2, 2, []byte{'3', 0}}, scalar(11, 0), {30, 3, 1, []byte{1, 0}}, scalar(31, 0)}, map[string]interface{}{"measure_mode": "3", "dop": float64(0), "differential": 1, "horizontal_positioning_error_meters": float64(0)}, true},
		{"invalid mode", []gpsAdditionalTag{{10, 2, 2, []byte{'4', 0}}, dop, correction, errorMeters}, map[string]interface{}{"dop": 1.25, "differential": 0, "horizontal_positioning_error_meters": 2.5}, false},
		{"all removed", nil, map[string]interface{}{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tags := append(append([]gpsAdditionalTag{}, clock...), tc.tags...)
			_, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{ContentReader: bytesContentReader{content: gpsExtendedJPEGContent(t, tags)}, Item: item, PhysicalPath: item.PhysicalPath, EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) }})
			if err != nil {
				t.Fatal(err)
			}
			gps := commonJSON.Section(attrs, "format_info.jpeg.exif.gps")
			for _, key := range []string{"measure_mode", "dop", "differential", "horizontal_positioning_error_meters"} {
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
			if commonJSON.String(attrs, "capabilities.extraction", "status") != "ready" || len(commonJSON.Section(attrs, "capabilities.temporal")) != 0 || commonJSON.Section(attrs, "capabilities.spatial")["extent"] != nil || commonJSON.Section(attrs, "type_info.media")["dop"] != nil || gps["hdop"] != nil || gps["rtk_status"] != nil {
				t.Fatalf("quality leaked inferred/foreign facts: %#v", attrs)
			}
		})
	}
}
