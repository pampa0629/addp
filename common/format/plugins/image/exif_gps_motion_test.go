package image

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func gpsMotionTags(order binary.ByteOrder, unit, trackRef, imageRef byte, speed, track, image uint32) []gpsFixtureTag {
	scalar := func(tag uint16, value uint32) gpsFixtureTag {
		raw := make([]byte, 8)
		order.PutUint32(raw, value)
		order.PutUint32(raw[4:], 100)
		return gpsFixtureTag{tag, 5, 1, raw}
	}
	return []gpsFixtureTag{
		{12, 2, 2, []byte{unit, 0}}, scalar(13, speed),
		{14, 2, 2, []byte{trackRef, 0}}, scalar(15, track),
		{16, 2, 2, []byte{imageRef, 0}}, scalar(17, image),
	}
}

func TestJPEGGPSMotionIndependentSourceFacts(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, unit := range []byte{'K', 'M', 'N'} {
			t.Run(order.String()+"/"+string(unit), func(t *testing.T) {
				tags := append(gpsFixtureTags(order), gpsMotionTags(order, unit, 'T', 'M', 1250, 0, 35999)...)
				result := describeGPSJPEG(t, gpsFixture(order, tags))
				gps := result.FormatInfo["exif"].(map[string]interface{})["gps"].(map[string]interface{})
				want := map[string]interface{}{"speed_ref": string(unit), "speed": 12.5, "track_ref": "T", "track_degrees": float64(0), "image_direction_ref": "M", "image_direction_degrees": 359.99}
				for key, value := range want {
					if !reflect.DeepEqual(gps[key], value) {
						t.Fatalf("%s: got %v want %v", key, gps[key], value)
					}
				}
				if result.FormatInfo["exif_status"] != "parsed" || result.Spatial == nil {
					t.Fatalf("motion invalidated valid GPS: %#v", result)
				}
			})
		}
		for _, refsOnly := range []bool{false, true} {
			t.Run(order.String()+map[bool]string{false: "/zero values only", true: "/references only"}[refsOnly], func(t *testing.T) {
				tags := gpsFixtureTags(order)
				for i, tag := range gpsMotionTags(order, 'N', 'M', 'T', 0, 0, 0) {
					if (i%2 == 0) == refsOnly {
						tags = append(tags, tag)
					}
				}
				result := describeGPSJPEG(t, gpsFixture(order, tags))
				gps := result.FormatInfo["exif"].(map[string]interface{})["gps"].(map[string]interface{})
				for i, key := range []string{"speed_ref", "speed", "track_ref", "track_degrees", "image_direction_ref", "image_direction_degrees"} {
					if (gps[key] != nil) != ((i%2 == 0) == refsOnly) {
						t.Fatalf("missing source defaulted or removed: %#v", gps)
					}
					if i%2 == 1 && !refsOnly && gps[key] != float64(0) {
						t.Fatalf("zero source was lost: %#v", gps)
					}
				}
				if result.FormatInfo["exif_status"] != "parsed" {
					t.Fatalf("missing independent field invalidated source: %#v", result)
				}
			})
		}
	}
}

func TestJPEGGPSInvalidMotionPreservesSourcesAndBlocksNormalizedFacts(t *testing.T) {
	type mutation struct {
		name  string
		apply func(*gpsFixtureTag)
	}
	keys := []string{"speed_ref", "speed", "track_ref", "track_degrees", "image_direction_ref", "image_direction_degrees"}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for index, key := range keys {
			mutations := []mutation{
				{"type", func(tag *gpsFixtureTag) { tag.typ = 7 }},
				{"count", func(tag *gpsFixtureTag) { tag.count = 3 }},
			}
			if index%2 == 0 {
				mutations = append(mutations, mutation{"reserved ref", func(tag *gpsFixtureTag) { tag.value[0] = 'X' }},
					mutation{"unterminated ref", func(tag *gpsFixtureTag) { tag.value[1] = 'X' }})
			} else {
				mutations = append(mutations, mutation{"zero denominator", func(tag *gpsFixtureTag) { order.PutUint32(tag.value[4:], 0) }})
				if index > 1 {
					mutations = append(mutations, mutation{"over maximum", func(tag *gpsFixtureTag) { order.PutUint32(tag.value, 359991); order.PutUint32(tag.value[4:], 1000) }})
				}
			}
			for _, mutation := range mutations {
				t.Run(order.String()+"/"+key+"/"+mutation.name, func(t *testing.T) {
					motion := gpsMotionTags(order, 'K', 'M', 'T', 0, 12500, 25000)
					mutation.apply(&motion[index])
					tags := append(gpsClockTags(order, "2024:02:29", 12, 1, 34, 1, 56, 1), motion...)
					result := describeGPSJPEG(t, gpsFixture(order, tags))
					exif := result.FormatInfo["exif"].(map[string]interface{})
					gps := exif["gps"].(map[string]interface{})
					if result.FormatInfo["exif_status"] != "invalid" || gps[key] != nil || gps["date_time_utc"] != nil || result.Spatial != nil || gps["date_stamp"] != "2024:02:29" || gps["time_hms"] == nil || exif["orientation"] != 6 {
						t.Fatalf("invalid motion leaked normalized facts or erased sources: %#v", result)
					}
					for otherIndex, otherKey := range keys {
						if otherIndex != index && gps[otherKey] == nil {
							t.Fatalf("independent source lost: %s %#v", otherKey, gps)
						}
					}
				})
			}
		}
		t.Run(order.String()+"/scalar offset outside APP1", func(t *testing.T) {
			tags := append(gpsFixtureTags(order), gpsMotionTags(order, 'K', 'T', 'M', 1250, 0, 35999)...)
			metadata := gpsFixture(order, tags)
			order.PutUint32(metadata[40+10*12+8:], uint32(len(metadata)+10))
			result := describeGPSJPEG(t, metadata)
			gps := result.FormatInfo["exif"].(map[string]interface{})["gps"].(map[string]interface{})
			if result.FormatInfo["exif_status"] != "invalid" || gps["speed"] != nil || gps["track_degrees"] != float64(0) || result.Spatial != nil {
				t.Fatalf("bad scalar offset: %#v", result)
			}
		})
	}
}
