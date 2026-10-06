package metaenrich

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/jpeg"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
)

func gpsJPEGContent(t *testing.T, datum string, status byte) []byte {
	t.Helper()
	order := binary.LittleEndian
	metadata := make([]byte, 128)
	copy(metadata, []byte{'I', 'I', 42, 0, 8, 0, 0, 0})
	entry := func(at int, tag, typ uint16, count, value uint32) {
		order.PutUint16(metadata[at:at+2], tag)
		order.PutUint16(metadata[at+2:at+4], typ)
		order.PutUint32(metadata[at+4:at+8], count)
		order.PutUint32(metadata[at+8:at+12], value)
	}
	order.PutUint16(metadata[8:10], 2)
	entry(10, 274, 3, 1, 6)
	entry(22, 34853, 4, 1, 38)
	order.PutUint16(metadata[38:40], 7)
	entry(40, 0, 1, 4, 0x00000302)
	entry(52, 1, 2, 2, 'N')
	entry(64, 2, 5, 3, 128)
	entry(76, 3, 2, 2, 'W')
	entry(88, 4, 5, 3, 152)
	entry(100, 9, 2, 2, uint32(status))
	datumValue := uint32(176)
	if len(datum)+1 <= 4 {
		datumValue = 0
		for i, char := range []byte(datum) {
			datumValue |= uint32(char) << uint(i*8)
		}
	}
	entry(112, 18, 2, uint32(len(datum)+1), datumValue)
	var coords bytes.Buffer
	_ = binary.Write(&coords, order, []uint32{0, 1, 0, 1, 0, 1, 120, 1, 15, 1, 0, 1})
	metadata = append(metadata, coords.Bytes()...)
	if len(datum)+1 > 4 {
		metadata = append(metadata, []byte(datum+"\x00")...)
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 3)), nil); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte("Exif\x00\x00"), metadata...)
	header := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(header[2:], uint16(len(payload)+2))
	content := append([]byte{}, encoded.Bytes()[:2]...)
	content = append(content, header...)
	content = append(content, payload...)
	return append(content, encoded.Bytes()[2:]...)
}

func TestEnrichJPEGGPSPersistsCapturePointWithoutRasterCoverage(t *testing.T) {
	for _, datum := range []string{"WGS-84", "TOKYO", ""} {
		for _, status := range []byte{'A', 'V', 'X'} {
			t.Run(datum+"/"+string(status), func(t *testing.T) {
				content := gpsJPEGContent(t, datum, status)
				size := int64(len(content))
				item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Layout: format.LayoutSingle, DataType: datatype.Media, Format: string(format.FormatJPEG), PrimaryContentPath: "images/gps.jpg", SizeBytes: &size}, PhysicalPath: "images/gps.jpg"}
				attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
				_, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{ContentReader: bytesContentReader{content: content}, Item: item, PhysicalPath: item.PhysicalPath, SizeBytes: size, EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) }})
				if err != nil {
					t.Fatal(err)
				}
				attrs = metaattr.Normalize(attrs)
				spatial := commonJSON.Section(attrs, "capabilities.spatial")
				point := commonJSON.Section(attrs, "capabilities.spatial.capture_location")
				gps := commonJSON.Section(attrs, "format_info.jpeg.exif.gps")
				if item.DataType != datatype.Media || item.Format != "jpeg" || commonJSON.Int64(attrs, "type_info.media", "width") != 2 || commonJSON.Int64(attrs, "type_info.media", "height") != 3 {
					t.Fatalf("GPS changed image identity or dimensions: %#v", attrs)
				}
				if status == 'A' {
					if point["latitude"] != float64(0) || point["longitude"] != -120.25 || commonJSON.InterfaceString(point["datum"]) != datum {
						t.Fatalf("capture point not persisted: %#v", attrs)
					}
					if (point["srid"] != nil) != (datum == "WGS-84") || datum == "WGS-84" && commonJSON.InterfaceInt64(point["srid"]) != 4326 {
						t.Fatalf("invented capture CRS: %#v", point)
					}
				} else if len(spatial) != 0 {
					t.Fatalf("invalid/interrupted fix created point: %#v", spatial)
				}
				if spatial["extent"] != nil || spatial["geometry_columns"] != nil || spatial["srid"] != nil || commonJSON.Section(attrs, "type_info.media")["gps"] != nil {
					t.Fatalf("GPS became raster/type facts: %#v", attrs)
				}
				if gps["latitude_ref"] != "N" || gps["longitude_ref"] != "W" || commonJSON.InterfaceString(gps["map_datum"]) != datum {
					t.Fatalf("native GPS facts lost: %#v", gps)
				}
				if got := commonJSON.String(attrs, "format_info.jpeg", "exif_status"); (got == "invalid") != (status == 'X') {
					t.Fatalf("GPS diagnosis: %s", got)
				}
			})
		}
	}
}

func TestEnrichJPEGRefreshReplacesMediaSnapshot(t *testing.T) {
	var plain bytes.Buffer
	if err := jpeg.Encode(&plain, image.NewRGBA(image.Rect(0, 0, 7, 5)), nil); err != nil {
		t.Fatal(err)
	}
	withoutGPS := gpsJPEGContent(t, "WGS-84", 'A')
	// APP1 metadata starts at byte 12; make the GPS pointer an unrelated tag.
	binary.LittleEndian.PutUint16(withoutGPS[12+22:], 65000)
	for _, tt := range []struct {
		name    string
		content []byte
		point   bool
		srid    bool
		gps     bool
		exif    bool
		err     bool
	}{
		{"known datum", gpsJPEGContent(t, "WGS-84", 'A'), true, true, true, true, false},
		{"datum removed", gpsJPEGContent(t, "", 'A'), true, false, true, true, false},
		{"interrupted", gpsJPEGContent(t, "WGS-84", 'V'), false, false, true, true, false},
		{"invalid", gpsJPEGContent(t, "WGS-84", 'X'), false, false, true, true, false},
		{"GPS removed", withoutGPS, false, false, false, true, false},
		{"EXIF removed", plain.Bytes(), false, false, false, false, false},
		{"description failed", []byte{0xff, 0xd8, 0xff, 0xd9}, true, true, true, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{
				Layout: format.LayoutSingle, DataType: datatype.Media, Format: "jpeg", PrimaryContentPath: "images/gps.jpg",
			}, PhysicalPath: "images/gps.jpg"}
			attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
			enrich := func(content []byte) error {
				_, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
					ContentReader: bytesContentReader{content: content}, Item: item, PhysicalPath: item.PhysicalPath,
					EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) },
				})
				return err
			}
			if err := enrich(gpsJPEGContent(t, "WGS-84", 'A')); err != nil {
				t.Fatal(err)
			}
			metaattr.SetStorage(attrs, "source_modified_at", "2026-01-01T00:00:00Z")
			metaattr.ReplaceCapabilityNamespace(attrs, "extraction", map[string]interface{}{"status": "ready"})
			commonJSON.Section(attrs, "type_info.media")["duration_ms"] = 77
			before, err := json.Marshal(attrs)
			if err != nil {
				t.Fatal(err)
			}
			err = enrich(tt.content)
			if (err != nil) != tt.err {
				t.Fatalf("refresh error = %v, want error=%v", err, tt.err)
			}
			if tt.err {
				after, err := json.Marshal(attrs)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("failed description replaced prior facts: %s -> %s (%v)", before, after, err)
				}
				return
			}
			attrs = metaattr.Normalize(attrs)
			point := commonJSON.Section(attrs, "capabilities.spatial.capture_location")
			if (len(point) > 0) != tt.point || (point["srid"] != nil) != tt.srid ||
				(len(commonJSON.Section(attrs, "format_info.jpeg.exif.gps")) > 0) != tt.gps ||
				(len(commonJSON.Section(attrs, "format_info.jpeg.exif")) > 0) != tt.exif {
				t.Fatalf("stale or missing refreshed facts: %#v", attrs)
			}
			media := commonJSON.Section(attrs, "type_info.media")
			if media["duration_ms"] != nil || !tt.exif && (commonJSON.InterfaceInt64(media["width"]) != 7 || commonJSON.InterfaceInt64(media["height"]) != 5) {
				t.Fatalf("stale media snapshot: %#v", media)
			}
			if commonJSON.String(attrs, "storage", "source_modified_at") != "2026-01-01T00:00:00Z" || commonJSON.String(attrs, "capabilities.extraction", "status") != "ready" {
				t.Fatalf("refresh removed facts owned by other namespaces: %#v", attrs)
			}
		})
	}
}

// Extend the independent GPS IFD while preserving rational offsets and Orientation.
type gpsAdditionalTag struct {
	id, typ uint16
	count   uint32
	raw     []byte
}

func gpsExtendedJPEGContent(t *testing.T, tags []gpsAdditionalTag) []byte {
	t.Helper()
	content := gpsJPEGContent(t, "WGS-84", 'A')
	end := 4 + int(binary.BigEndian.Uint16(content[4:6]))
	metadata := append([]byte{}, content[12:end]...)
	count := 7 + len(tags)
	newIFD := len(metadata)
	metadata = append(metadata, make([]byte, 2+count*12+4)...)
	order := binary.LittleEndian
	order.PutUint32(metadata[30:34], uint32(newIFD))
	order.PutUint16(metadata[newIFD:], uint16(count))
	copy(metadata[newIFD+2:], metadata[40:124])
	at := newIFD + 2 + 7*12
	for _, tag := range tags {
		order.PutUint16(metadata[at:], tag.id)
		order.PutUint16(metadata[at+2:], tag.typ)
		order.PutUint32(metadata[at+4:], tag.count)
		if len(tag.raw) <= 4 {
			copy(metadata[at+8:at+12], tag.raw)
		} else {
			order.PutUint32(metadata[at+8:], uint32(len(metadata)))
			metadata = append(metadata, tag.raw...)
		}
		at += 12
	}
	payload := append([]byte("Exif\x00\x00"), metadata...)
	header := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(header[2:], uint16(len(payload)+2))
	result := append(append([]byte{}, content[:2]...), header...)
	result = append(result, payload...)
	return append(result, content[end:]...)
}

func gpsClockJPEGContent(t *testing.T, date string, clock []uint32) []byte {
	t.Helper()
	var tags []gpsAdditionalTag
	if clock != nil {
		var raw bytes.Buffer
		_ = binary.Write(&raw, binary.LittleEndian, clock)
		tags = append(tags, gpsAdditionalTag{7, 5, 3, raw.Bytes()})
	}
	if date != "" {
		tags = append(tags, gpsAdditionalTag{29, 2, 11, []byte(date + "\x00")})
	}
	return gpsExtendedJPEGContent(t, tags)
}

func TestEnrichJPEGGPSUTCClockReplacement(t *testing.T) {
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{
		Layout: format.LayoutSingle, DataType: datatype.Media, Format: "jpeg", PrimaryContentPath: "images/gps-clock.jpg",
	}, PhysicalPath: "images/gps-clock.jpg"}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
	clock := []uint32{12, 1, 34, 1, 56789, 1000}
	for _, tc := range []struct {
		name, date, timestamp string
		clock                 []uint32
		status                string
	}{
		{"complete", "2024:02:29", "2024-02-29T12:34:56.789Z", clock, "parsed"},
		{"date removed", "", "", clock, "parsed"},
		{"restored", "2024:02:29", "2024-02-29T12:34:56.789Z", clock, "parsed"},
		{"time removed", "2024:02:29", "", nil, "parsed"},
		{"invalid date", "2023:02:29", "", clock, "invalid"},
		{"all removed", "", "", nil, "parsed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := gpsClockJPEGContent(t, tc.date, tc.clock)
			_, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
				ContentReader: bytesContentReader{content: content}, Item: item, PhysicalPath: item.PhysicalPath,
				EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) },
			})
			if err != nil {
				t.Fatal(err)
			}
			gps := commonJSON.Section(attrs, "format_info.jpeg.exif.gps")
			if commonJSON.InterfaceString(gps["date_time_utc"]) != tc.timestamp || commonJSON.String(attrs, "format_info.jpeg", "exif_status") != tc.status ||
				(gps["date_stamp"] != nil) != (tc.date != "" && tc.status == "parsed") || (gps["time_hms"] != nil) != (tc.clock != nil) {
				t.Fatalf("stale or missing UTC snapshot: %#v", attrs)
			}
			if len(commonJSON.Section(attrs, "capabilities.temporal")) != 0 || commonJSON.Section(attrs, "type_info.media")["date_time_utc"] != nil {
				t.Fatalf("receiver clock became a media/temporal capability: %#v", attrs)
			}
		})
	}
}
