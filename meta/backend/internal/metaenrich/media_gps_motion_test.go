package metaenrich

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
)

func TestEnrichJPEGGPSMotionSnapshotReplacement(t *testing.T) {
	scalar := func(id uint16, numerator uint32) gpsAdditionalTag {
		raw := make([]byte, 8)
		binary.LittleEndian.PutUint32(raw, numerator)
		binary.LittleEndian.PutUint32(raw[4:], 100)
		return gpsAdditionalTag{id, 5, 1, raw}
	}
	refs := []gpsAdditionalTag{{12, 2, 2, []byte{'N', 0}}, {14, 2, 2, []byte{'M', 0}}, {16, 2, 2, []byte{'T', 0}}}
	values := []gpsAdditionalTag{scalar(13, 0), scalar(15, 1250), scalar(17, 35999)}
	invalidValues := []gpsAdditionalTag{scalar(13, 0), scalar(15, 36000), scalar(17, 35999)}
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{
		Layout: format.LayoutSingle, DataType: datatype.Media, Format: "jpeg", PrimaryContentPath: "images/gps-motion.jpg",
	}, PhysicalPath: "images/gps-motion.jpg"}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
	metaattr.ReplaceCapabilityNamespace(attrs, "extraction", map[string]interface{}{"status": "ready"})
	for _, tc := range []struct {
		name  string
		tags  []gpsAdditionalTag
		want  map[string]interface{}
		valid bool
	}{
		{"complete", append(append([]gpsAdditionalTag{}, refs...), values...), map[string]interface{}{"speed_ref": "N", "speed": float64(0), "track_ref": "M", "track_degrees": 12.5, "image_direction_ref": "T", "image_direction_degrees": 359.99}, true},
		{"references removed", values, map[string]interface{}{"speed": float64(0), "track_degrees": 12.5, "image_direction_degrees": 359.99}, true},
		{"values removed", refs, map[string]interface{}{"speed_ref": "N", "track_ref": "M", "image_direction_ref": "T"}, true},
		{"invalid direction", append(append([]gpsAdditionalTag{}, refs...), invalidValues...), map[string]interface{}{"speed_ref": "N", "speed": float64(0), "track_ref": "M", "image_direction_ref": "T", "image_direction_degrees": 359.99}, false},
		{"all removed", nil, map[string]interface{}{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
				ContentReader: bytesContentReader{content: gpsExtendedJPEGContent(t, tc.tags)}, Item: item, PhysicalPath: item.PhysicalPath,
				EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) },
			})
			if err != nil {
				t.Fatal(err)
			}
			gps := commonJSON.Section(attrs, "format_info.jpeg.exif.gps")
			for _, key := range []string{"speed_ref", "speed", "track_ref", "track_degrees", "image_direction_ref", "image_direction_degrees"} {
				if gps[key] != tc.want[key] {
					t.Fatalf("stale or missing %s: got %#v want %#v", key, gps[key], tc.want[key])
				}
			}
			status := "parsed"
			if !tc.valid {
				status = "invalid"
			}
			if commonJSON.String(attrs, "format_info.jpeg", "exif_status") != status || (len(commonJSON.Section(attrs, "capabilities.spatial.capture_location")) != 0) != tc.valid {
				t.Fatalf("invalid motion leaked point or wrong diagnostic: %#v", attrs)
			}
			if commonJSON.String(attrs, "capabilities.extraction", "status") != "ready" || len(commonJSON.Section(attrs, "capabilities.temporal")) != 0 || commonJSON.Section(attrs, "type_info.media")["speed"] != nil {
				t.Fatalf("motion changed other owners/capabilities: %#v", attrs)
			}
		})
	}
}
