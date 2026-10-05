package image

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"reflect"
	"testing"

	"github.com/addp/common/format"
)

type gpsFixtureTag struct {
	id, typ uint16
	count   uint32
	value   []byte
}

func gpsFixtureTags(order binary.ByteOrder) []gpsFixtureTag {
	rationals := func(values ...uint32) []byte {
		var raw bytes.Buffer
		_ = binary.Write(&raw, order, values)
		return raw.Bytes()
	}
	return []gpsFixtureTag{
		{0, 1, 4, []byte{2, 3, 0, 0}},
		{1, 2, 2, []byte{'S', 0}},
		{2, 5, 3, rationals(30, 1, 30, 1, 0, 1)},
		{3, 2, 2, []byte{'W', 0}},
		{4, 5, 3, rationals(120, 1, 15, 1, 0, 1)},
		{5, 1, 1, []byte{1}},
		{6, 5, 1, rationals(125, 10)},
		{9, 2, 2, []byte{'A', 0}},
		{18, 2, 7, []byte("WGS-84\x00")},
	}
}

// IFD0 contains Orientation and GPSInfoIFDPointer, without an Exif IFD.
func gpsFixture(order binary.ByteOrder, tags []gpsFixtureTag) []byte {
	data := make([]byte, 38+2+len(tags)*12+4)
	copy(data, "II")
	if order == binary.BigEndian {
		copy(data, "MM")
	}
	order.PutUint16(data[2:4], 42)
	order.PutUint32(data[4:8], 8)
	order.PutUint16(data[8:10], 2)
	order.PutUint16(data[10:12], 274)
	order.PutUint16(data[12:14], 3)
	order.PutUint32(data[14:18], 1)
	order.PutUint16(data[18:20], 6)
	order.PutUint16(data[22:24], 34853)
	order.PutUint16(data[24:26], 4)
	order.PutUint32(data[26:30], 1)
	order.PutUint32(data[30:34], 38)
	order.PutUint16(data[38:40], uint16(len(tags)))
	for i, tag := range tags {
		entry := data[40+i*12 : 52+i*12]
		order.PutUint16(entry[:2], tag.id)
		order.PutUint16(entry[2:4], tag.typ)
		order.PutUint32(entry[4:8], tag.count)
		if len(tag.value) <= 4 {
			copy(entry[8:], tag.value)
		} else {
			order.PutUint32(entry[8:], uint32(len(data)))
			data = append(data, tag.value...)
		}
	}
	return data
}

func describeGPSJPEG(t *testing.T, metadata []byte) *format.MediaDescribeResult {
	t.Helper()
	provider, err := format.GetMediaInfoProvider(format.FormatJPEG)
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.DescribeMedia(context.Background(), bytes.NewBuffer(jpegFixture(t, append([]byte("Exif\x00\x00"), metadata...))), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Media.Width != 2 || result.Media.Height != 3 {
		t.Fatalf("GPS changed coded dimensions: %#v", result.Media)
	}
	if result.Spatial != nil && (result.Spatial.SRID != nil || result.Spatial.Extent != nil || len(result.Spatial.GeometryColumns) != 0 || result.Spatial.CRSRef != "" || result.Spatial.IsSpatial()) {
		t.Fatalf("capture point became raster/table spatial facts: %#v", result.Spatial)
	}
	return result
}

func TestJPEGGPSBothByteOrdersAndIndependentIFD(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, refs := range [][2]byte{{'N', 'E'}, {'S', 'W'}} {
			tags := gpsFixtureTags(order)
			tags[1].value[0], tags[3].value[0] = refs[0], refs[1]
			result := describeGPSJPEG(t, gpsFixture(order, tags))
			exif := result.FormatInfo["exif"].(map[string]interface{})
			gps := exif["gps"].(map[string]interface{})
			if result.FormatInfo["exif_status"] != "parsed" || exif["orientation"] != 6 || len(gps) != 9 ||
				!reflect.DeepEqual(gps["version_id"], []int{2, 3, 0, 0}) || !reflect.DeepEqual(gps["latitude_dms"], []float64{30, 30, 0}) ||
				gps["altitude_meters"] != 12.5 || gps["altitude_ref"] != 1 {
				t.Fatalf("source GPS facts: %#v", result.FormatInfo)
			}
			if result.Spatial == nil || result.Spatial.CaptureLocation == nil {
				t.Fatal("missing capture point")
			}
			point := result.Spatial.CaptureLocation
			lat, lon := 30.5, 120.25
			if refs[0] == 'S' {
				lat, lon = -lat, -lon
			}
			if point.Latitude != lat || point.Longitude != lon || point.Datum != "WGS-84" || point.SRID == nil || *point.SRID != 4326 {
				t.Fatalf("capture point: %#v", point)
			}
		}
	}
}

func TestJPEGGPSUnknownDatumInterruptedAndMissingFacts(t *testing.T) {
	for _, datum := range []string{"", "TOKYO", " WGS-84 "} {
		tags := gpsFixtureTags(binary.LittleEndian)
		if datum == "" {
			tags = tags[:8]
		} else {
			tags[8].value = []byte(datum + "\x00")
			tags[8].count = uint32(len(tags[8].value))
		}
		result := describeGPSJPEG(t, gpsFixture(binary.LittleEndian, tags))
		if result.Spatial == nil {
			t.Fatal("unknown datum lost source coordinate")
		}
		point := result.Spatial.CaptureLocation
		if point.Datum != datum || (point.SRID != nil) != (datum == " WGS-84 ") {
			t.Fatalf("invented CRS: %#v", point)
		}
	}
	for _, id := range []uint16{1, 2, 3, 4, 9} {
		tags := gpsFixtureTags(binary.LittleEndian)
		for i, tag := range tags {
			if tag.id == id {
				tags = append(tags[:i], tags[i+1:]...)
				break
			}
		}
		result := describeGPSJPEG(t, gpsFixture(binary.LittleEndian, tags))
		if result.FormatInfo["exif_status"] != "parsed" || (result.Spatial != nil) != (id == 9) {
			t.Fatalf("missing tag %d invented fact: %#v", id, result)
		}
	}
	tags := gpsFixtureTags(binary.LittleEndian)
	tags[7].value[0] = 'V'
	result := describeGPSJPEG(t, gpsFixture(binary.LittleEndian, tags))
	if result.Spatial != nil || result.FormatInfo["exif_status"] != "parsed" {
		t.Fatalf("interrupted GPS became capture point: %#v", result)
	}
}

func TestJPEGGPSZeroCoordinatesFractionalMinutesAndLimits(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, values := range [][3]uint32{{0, 0, 0}, {90, 0, 0}, {30, 3050, 0}} {
			tags := gpsFixtureTags(order)
			for i, value := range values {
				order.PutUint32(tags[2].value[i*8:], value)
				order.PutUint32(tags[2].value[i*8+4:], 1)
			}
			if values[0] == 30 {
				order.PutUint32(tags[2].value[12:], 100)
			}
			result := describeGPSJPEG(t, gpsFixture(order, tags))
			want := -float64(values[0])
			if values[0] == 30 {
				want -= 30.5 / 60
			}
			if result.Spatial == nil || math.Abs(result.Spatial.CaptureLocation.Latitude-want) > 1e-12 {
				t.Fatalf("coordinate lost: %#v", result)
			}
		}
	}
}

func TestJPEGGPSInvalidFieldsOmitOnlyTheirSourceFacts(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, tc := range []struct {
			name, key string
			mutate    func([]gpsFixtureTag) []gpsFixtureTag
		}{
			{"missing-version", "version_id", func(tags []gpsFixtureTag) []gpsFixtureTag { return tags[1:] }},
			{"version-type", "version_id", func(tags []gpsFixtureTag) []gpsFixtureTag { tags[0].typ = 7; return tags }},
			{"ref", "latitude_ref", func(tags []gpsFixtureTag) []gpsFixtureTag { tags[1].value[0] = 'X'; return tags }},
			{"ref-count", "longitude_ref", func(tags []gpsFixtureTag) []gpsFixtureTag { tags[3].count = 1; return tags }},
			{"latitude-type", "latitude_dms", func(tags []gpsFixtureTag) []gpsFixtureTag { tags[2].typ = 10; return tags }},
			{"latitude-count", "latitude_dms", func(tags []gpsFixtureTag) []gpsFixtureTag { tags[2].count = 2; return tags }},
			{"zero-denominator", "latitude_dms", func(tags []gpsFixtureTag) []gpsFixtureTag { order.PutUint32(tags[2].value[4:], 0); return tags }},
			{"minutes", "latitude_dms", func(tags []gpsFixtureTag) []gpsFixtureTag { order.PutUint32(tags[2].value[8:], 60); return tags }},
			{"seconds", "longitude_dms", func(tags []gpsFixtureTag) []gpsFixtureTag { order.PutUint32(tags[4].value[16:], 60); return tags }},
			{"latitude-range", "latitude_dms", func(tags []gpsFixtureTag) []gpsFixtureTag { order.PutUint32(tags[2].value, 90); return tags }},
			{"longitude-range", "longitude_dms", func(tags []gpsFixtureTag) []gpsFixtureTag { order.PutUint32(tags[4].value, 181); return tags }},
			{"altitude-ref", "altitude_ref", func(tags []gpsFixtureTag) []gpsFixtureTag { tags[5].value[0] = 2; return tags }},
			{"altitude-denominator", "altitude_meters", func(tags []gpsFixtureTag) []gpsFixtureTag { order.PutUint32(tags[6].value[4:], 0); return tags }},
			{"status", "status", func(tags []gpsFixtureTag) []gpsFixtureTag { tags[7].value[0] = 'X'; return tags }},
			{"datum-type", "map_datum", func(tags []gpsFixtureTag) []gpsFixtureTag { tags[8].typ = 7; return tags }},
			{"datum-unterminated", "map_datum", func(tags []gpsFixtureTag) []gpsFixtureTag { tags[8].value[6] = 'X'; return tags }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				result := describeGPSJPEG(t, gpsFixture(order, tc.mutate(gpsFixtureTags(order))))
				exif := result.FormatInfo["exif"].(map[string]interface{})
				gps := exif["gps"].(map[string]interface{})
				if result.FormatInfo["exif_status"] != "invalid" || exif["orientation"] != 6 || gps[tc.key] != nil || result.Spatial != nil {
					t.Fatalf("bad field %s: %#v", tc.key, result)
				}
			})
		}
	}
}

func TestJPEGGPSInvalidDirectoryAndOffsets(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, tc := range []struct {
			name   string
			mutate func([]byte) []byte
		}{
			{"root-cycle", func(data []byte) []byte { order.PutUint32(data[30:34], 8); return data }},
			{"pointer-type", func(data []byte) []byte { order.PutUint16(data[24:26], 3); return data }},
			{"pointer-count", func(data []byte) []byte { order.PutUint32(data[26:30], 2); return data }},
			{"pointer-overflow", func(data []byte) []byte { order.PutUint32(data[30:34], 0xffffffff); return data }},
			{"duplicate-tag", func(data []byte) []byte { order.PutUint16(data[52:54], 0); return data }},
			{"rational-offset", func(data []byte) []byte { order.PutUint32(data[72:76], 0xffffffff); return data }},
			{"truncated", func(data []byte) []byte { return data[:43] }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				result := describeGPSJPEG(t, tc.mutate(gpsFixture(order, gpsFixtureTags(order))))
				if result.FormatInfo["exif_status"] != "invalid" || result.Spatial != nil || result.FormatInfo["exif"].(map[string]interface{})["orientation"] != 6 {
					t.Fatalf("invalid directory: %#v", result)
				}
			})
		}
	}
}

func TestJPEGGPSAndExifSiblingDirectories(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		data := gpsFixture(order, gpsFixtureTags(order))
		// Replace the root Orientation entry with an Exif IFD pointer.
		order.PutUint16(data[10:12], 34665)
		order.PutUint16(data[12:14], 4)
		order.PutUint32(data[18:22], uint32(len(data)))
		data = append(data, make([]byte, 6)...)
		result := describeGPSJPEG(t, data)
		if result.Spatial == nil || result.FormatInfo["exif_status"] != "parsed" {
			t.Fatalf("independent siblings failed: %#v", result)
		}
		order.PutUint32(data[18:22], 38)
		result = describeGPSJPEG(t, data)
		if result.Spatial != nil || result.FormatInfo["exif_status"] != "invalid" {
			t.Fatalf("aliased siblings accepted: %#v", result)
		}
	}
}

func TestJPEGGPSDuplicateAPP1AndUnfinishedMarkersDoNotPublishPoint(t *testing.T) {
	payload := append([]byte("Exif\x00\x00"), gpsFixture(binary.LittleEndian, gpsFixtureTags(binary.LittleEndian))...)
	provider, err := format.GetMediaInfoProvider(format.FormatJPEG)
	if err != nil {
		t.Fatal(err)
	}
	complete := jpegFixture(t, payload)
	at := bytes.Index(complete, []byte{0xff, 0xda})
	if at < 0 {
		t.Fatal("fixture has no scan marker")
	}
	truncated := append(append([]byte{}, complete[:at]...), 0xff, 0xe0, 0, 8)
	for _, data := range [][]byte{jpegFixture(t, payload, payload), truncated} {
		result, err := provider.DescribeMedia(context.Background(), bytes.NewBuffer(data), nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Spatial != nil || result.FormatInfo["exif_status"] != "invalid" {
			t.Fatalf("ambiguous/incomplete metadata published location: %#v", result)
		}
	}
}
