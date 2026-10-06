package image

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"
)

var gpsDestinationKeys = []string{"destination_latitude_ref", "destination_latitude_dms", "destination_longitude_ref", "destination_longitude_dms", "destination_bearing_ref", "destination_bearing_degrees", "destination_distance_ref", "destination_distance"}

func gpsDestinationTags(order binary.ByteOrder, unit byte) []gpsFixtureTag {
	rationals := func(id uint16, values ...uint32) gpsFixtureTag {
		raw := make([]byte, len(values)*4)
		for i, value := range values {
			order.PutUint32(raw[i*4:], value)
		}
		return gpsFixtureTag{id, 5, uint32(len(values) / 2), raw}
	}
	return []gpsFixtureTag{
		{19, 2, 2, []byte{'N', 0}}, rationals(20, 10, 1, 125, 10, 0, 1),
		{21, 2, 2, []byte{'E', 0}}, rationals(22, 20, 1, 15, 1, 0, 1),
		{23, 2, 2, []byte{'T', 0}}, rationals(24, 35999, 100),
		{25, 2, 2, []byte{unit, 0}}, rationals(26, 125, 100),
	}
}

func TestJPEGGPSDestinationIndependentSources(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, unit := range []byte{'K', 'M', 'N'} {
			for _, only := range []int{-1, 0, 1, 2, 3, 4, 5, 6, 7} {
				t.Run(fmt.Sprintf("%s/unit=%c/only=%d", order.String(), unit, only), func(t *testing.T) {
					destination := gpsDestinationTags(order, unit)
					want := []interface{}{"N", []float64{10, 12.5, 0}, "E", []float64{20, 15, 0}, "T", 359.99, string(unit), 1.25}
					tags := gpsClockTags(order, "2024:02:29", 12, 1, 34, 1, 56, 1)
					for i, tag := range destination {
						if only == -1 || only == i {
							tags = append(tags, tag)
						}
					}
					result := describeGPSJPEG(t, gpsFixture(order, tags))
					gps := result.FormatInfo["exif"].(map[string]interface{})["gps"].(map[string]interface{})
					for i, key := range gpsDestinationKeys {
						expected := want[i]
						if only != -1 && only != i {
							expected = nil
						}
						if !reflect.DeepEqual(gps[key], expected) {
							t.Fatalf("%s: got %#v want %#v", key, gps[key], expected)
						}
					}
					if result.FormatInfo["exif_status"] != "parsed" || gps["date_time_utc"] != "2024-02-29T12:34:56Z" || result.Spatial == nil || result.Spatial.CaptureLocation.Latitude != -30.5 || result.Spatial.CaptureLocation.Longitude != -120.25 {
						t.Fatalf("destination changed capture or clock: %#v", result)
					}
				})
			}
		}
		t.Run(order.String()+"/destination only and zero boundary", func(t *testing.T) {
			destination := gpsDestinationTags(order, 'N')
			destination[0].value[0] = 'S'
			destination[2].value[0] = 'W'
			destination[4].value[0] = 'M'
			for _, i := range []int{1, 3, 5, 7} {
				for n := 0; n < len(destination[i].value); n += 8 {
					order.PutUint32(destination[i].value[n:], 0)
				}
			}
			order.PutUint32(destination[1].value, 90)
			order.PutUint32(destination[3].value, 180)
			tags := append([]gpsFixtureTag{{0, 1, 4, []byte{2, 3, 0, 0}}}, destination...)
			result := describeGPSJPEG(t, gpsFixture(order, tags))
			gps := result.FormatInfo["exif"].(map[string]interface{})["gps"].(map[string]interface{})
			if result.FormatInfo["exif_status"] != "parsed" || result.Spatial != nil || gps["destination_distance"] != float64(0) || gps["destination_bearing_degrees"] != float64(0) || gps["destination_latitude_ref"] != "S" || gps["destination_longitude_ref"] != "W" || gps["destination_bearing_ref"] != "M" || !reflect.DeepEqual(gps["destination_latitude_dms"], []float64{90, 0, 0}) || !reflect.DeepEqual(gps["destination_longitude_dms"], []float64{180, 0, 0}) {
				t.Fatalf("destination became capture/defaulted: %#v", result)
			}
		})
	}
}

func TestJPEGGPSInvalidDestinationPreservesOtherSources(t *testing.T) {
	type mutation struct {
		name  string
		apply func(*gpsFixtureTag)
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for i, key := range gpsDestinationKeys {
			mutations := []mutation{
				{"type", func(tag *gpsFixtureTag) { tag.typ = 7 }},
				{"count", func(tag *gpsFixtureTag) { tag.count++ }},
			}
			if i%2 == 0 {
				mutations = append(mutations, mutation{"reserved", func(tag *gpsFixtureTag) { tag.value[0] = 'X' }}, mutation{"unterminated", func(tag *gpsFixtureTag) { tag.value[1] = 'X' }})
			} else {
				mutations = append(mutations, mutation{"zero denominator", func(tag *gpsFixtureTag) { order.PutUint32(tag.value[4:], 0) }})
				if i == 1 || i == 3 {
					mutations = append(mutations, mutation{"minutes", func(tag *gpsFixtureTag) { order.PutUint32(tag.value[8:], 60); order.PutUint32(tag.value[12:], 1) }}, mutation{"seconds", func(tag *gpsFixtureTag) { order.PutUint32(tag.value[16:], 60) }}, mutation{"total", func(tag *gpsFixtureTag) {
						limit := uint32(90)
						if i == 3 {
							limit = 180
						}
						order.PutUint32(tag.value, limit)
					}})
				} else if i == 5 {
					mutations = append(mutations, mutation{"360 degrees", func(tag *gpsFixtureTag) { order.PutUint32(tag.value, 36000) }})
				}
			}
			for _, mutation := range mutations {
				t.Run(order.String()+"/"+key+"/"+mutation.name, func(t *testing.T) {
					destination := gpsDestinationTags(order, 'K')
					mutation.apply(&destination[i])
					tags := append(gpsClockTags(order, "2024:02:29", 12, 1, 34, 1, 56, 1), destination...)
					result := describeGPSJPEG(t, gpsFixture(order, tags))
					gps := result.FormatInfo["exif"].(map[string]interface{})["gps"].(map[string]interface{})
					if result.FormatInfo["exif_status"] != "invalid" || gps[key] != nil || result.Spatial != nil || gps["date_time_utc"] != nil {
						t.Fatalf("invalid destination leaked: %#v", result)
					}
					for n, other := range gpsDestinationKeys {
						if n != i && gps[other] == nil {
							t.Fatalf("lost %s: %#v", other, gps)
						}
					}
					if gps["date_stamp"] != "2024:02:29" || gps["time_hms"] == nil || gps["latitude_dms"] == nil {
						t.Fatalf("lost independent facts: %#v", gps)
					}
				})
			}
		}
		for _, i := range []int{1, 3, 5, 7} {
			t.Run(order.String()+"/"+gpsDestinationKeys[i]+"/outside APP1", func(t *testing.T) {
				base := gpsFixtureTags(order)
				data := gpsFixture(order, append(base, gpsDestinationTags(order, 'K')...))
				order.PutUint32(data[40+(len(base)+i)*12+8:], uint32(len(data)+100))
				result := describeGPSJPEG(t, data)
				gps := result.FormatInfo["exif"].(map[string]interface{})["gps"].(map[string]interface{})
				if result.FormatInfo["exif_status"] != "invalid" || gps[gpsDestinationKeys[i]] != nil || gps["destination_distance_ref"] != "K" || result.Spatial != nil {
					t.Fatalf("invalid offset: %#v", result)
				}
			})
		}
	}
}
