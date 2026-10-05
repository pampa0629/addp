package image

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func gpsClockTags(order binary.ByteOrder, date string, fractions ...uint32) []gpsFixtureTag {
	raw := make([]byte, 24)
	for i, value := range fractions {
		order.PutUint32(raw[i*4:], value)
	}
	return append(gpsFixtureTags(order), gpsFixtureTag{7, 5, 3, raw}, gpsFixtureTag{29, 2, uint32(len(date) + 1), []byte(date + "\x00")})
}

func TestJPEGGPSUTCClockBothByteOrders(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, tc := range []struct {
			date, want string
			fractions  []uint32
			values     []float64
		}{
			{"2024:02:29", "2024-02-29T12:34:56.789Z", []uint32{12, 1, 34, 1, 56789, 1000}, []float64{12, 34, 56.789}},
			{"0001:01:01", "0001-01-01T00:00:00Z", []uint32{0, 1, 0, 1, 0, 1}, []float64{0, 0, 0}},
			{"9999:12:31", "9999-12-31T23:59:59.999999357Z", []uint32{23, 1, 59, 1, 4294967294, 71582789}, []float64{23, 59, float64(4294967294) / 71582789}},
			{"2026:10:05", "2026-10-05T00:30:30.333333333Z", []uint32{1, 2, 1, 2, 1, 3}, []float64{0.5, 0.5, float64(1) / 3}},
			{"2026:10:05", "2026-10-05T00:00:00Z", []uint32{0, 1, 0, 1, 1, 4294967295}, []float64{0, 0, float64(1) / 4294967295}},
		} {
			t.Run(order.String()+"/"+tc.want, func(t *testing.T) {
				result := describeGPSJPEG(t, gpsFixture(order, gpsClockTags(order, tc.date, tc.fractions...)))
				gps := result.FormatInfo["exif"].(map[string]interface{})["gps"].(map[string]interface{})
				if result.FormatInfo["exif_status"] != "parsed" || gps["date_stamp"] != tc.date || gps["date_time_utc"] != tc.want || !reflect.DeepEqual(gps["time_hms"], tc.values) || result.Spatial == nil {
					t.Fatalf("UTC clock/source facts: %#v", result)
				}
			})
		}
	}
}

func TestJPEGGPSClockMissingInterruptedAndLeapSecond(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		mutate                       func([]gpsFixtureTag) []gpsFixtureTag
		date, clock, combined, point bool
	}{
		{"date only", func(tags []gpsFixtureTag) []gpsFixtureTag { return append(tags[:9], tags[10]) }, true, false, false, true},
		{"time only", func(tags []gpsFixtureTag) []gpsFixtureTag { return tags[:10] }, false, true, false, true},
		{"interrupted", func(tags []gpsFixtureTag) []gpsFixtureTag { tags[7].value[0] = 'V'; return tags }, true, true, true, false},
		{"leap second", func(tags []gpsFixtureTag) []gpsFixtureTag {
			for i, v := range []uint32{23, 1, 59, 1, 121, 2} {
				binary.LittleEndian.PutUint32(tags[9].value[i*4:], v)
			}
			return tags
		}, true, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := describeGPSJPEG(t, gpsFixture(binary.LittleEndian, tc.mutate(gpsClockTags(binary.LittleEndian, "2026:10:05", 12, 1, 34, 1, 56, 1))))
			gps := result.FormatInfo["exif"].(map[string]interface{})["gps"].(map[string]interface{})
			if result.FormatInfo["exif_status"] != "parsed" || (gps["date_stamp"] != nil) != tc.date || (gps["time_hms"] != nil) != tc.clock || (gps["date_time_utc"] != nil) != tc.combined || (result.Spatial != nil) != tc.point {
				t.Fatalf("invented/missing facts: %#v", result)
			}
		})
	}
}

func TestJPEGGPSInvalidClockKeepsOtherSourceFacts(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, tc := range []struct {
			name, omitted string
			mutate        func([]gpsFixtureTag)
		}{
			{"date type", "date_stamp", func(tags []gpsFixtureTag) { tags[10].typ = 7 }},
			{"date count", "date_stamp", func(tags []gpsFixtureTag) { tags[10].count = 12; tags[10].value = append(tags[10].value, 0) }},
			{"date termination", "date_stamp", func(tags []gpsFixtureTag) { tags[10].value[10] = 'X' }},
			{"calendar", "date_stamp", func(tags []gpsFixtureTag) { tags[10].value = []byte("2023:02:29\x00") }},
			{"zero year", "date_stamp", func(tags []gpsFixtureTag) { tags[10].value = []byte("0000:01:01\x00") }},
			{"date format", "date_stamp", func(tags []gpsFixtureTag) { tags[10].value = []byte("2026-10-05\x00") }},
			{"unknown date", "date_stamp", func(tags []gpsFixtureTag) { tags[10].value = []byte("    :  :  \x00") }},
			{"time type", "time_hms", func(tags []gpsFixtureTag) { tags[9].typ = 10 }},
			{"time count", "time_hms", func(tags []gpsFixtureTag) { tags[9].count = 2 }},
			{"denominator", "time_hms", func(tags []gpsFixtureTag) { order.PutUint32(tags[9].value[4:], 0) }},
			{"hour", "time_hms", func(tags []gpsFixtureTag) { order.PutUint32(tags[9].value, 24) }},
			{"minute", "time_hms", func(tags []gpsFixtureTag) { order.PutUint32(tags[9].value[8:], 60) }},
			{"second", "time_hms", func(tags []gpsFixtureTag) { order.PutUint32(tags[9].value[16:], 61) }},
			{"misplaced leap", "time_hms", func(tags []gpsFixtureTag) { order.PutUint32(tags[9].value[16:], 60) }},
			{"total over day", "time_hms", func(tags []gpsFixtureTag) {
				for i, v := range []uint32{47, 2, 59, 1, 59, 1} {
					order.PutUint32(tags[9].value[i*4:], v)
				}
			}},
			{"missing version", "version_id", func(tags []gpsFixtureTag) { tags[0].id = 65000 }},
		} {
			t.Run(order.String()+"/"+tc.name, func(t *testing.T) {
				tags := gpsClockTags(order, "2026:10:05", 12, 1, 34, 1, 56, 1)
				tc.mutate(tags)
				result := describeGPSJPEG(t, gpsFixture(order, tags))
				exif := result.FormatInfo["exif"].(map[string]interface{})
				gps := exif["gps"].(map[string]interface{})
				if result.FormatInfo["exif_status"] != "invalid" || gps[tc.omitted] != nil || gps["date_time_utc"] != nil || gps["latitude_ref"] != "S" || exif["orientation"] != 6 || result.Spatial != nil {
					t.Fatalf("invalid clock leaked normalized facts or erased other facts: %#v", result)
				}
			})
		}
	}
}
