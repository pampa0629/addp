package image

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func gpsQualityTags(order binary.ByteOrder, mode byte, differential uint16, dop, errorMeters uint32) []gpsFixtureTag {
	scalar := func(id uint16, numerator uint32) gpsFixtureTag {
		raw := make([]byte, 8)
		order.PutUint32(raw, numerator)
		order.PutUint32(raw[4:], 100)
		return gpsFixtureTag{id, 5, 1, raw}
	}
	correction := make([]byte, 2)
	order.PutUint16(correction, differential)
	return []gpsFixtureTag{{10, 2, 2, []byte{mode, 0}}, scalar(11, dop), {30, 3, 1, correction}, scalar(31, errorMeters)}
}

func TestJPEGGPSQualityIndependentSources(t *testing.T) {
	keys := []string{"measure_mode", "dop", "differential", "horizontal_positioning_error_meters"}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, tc := range []struct {
			name             string
			indices          []int
			mode             byte
			differential     uint16
			dop, errorMeters uint32
		}{
			{"2D", []int{0, 1, 2, 3}, '2', 0, 125, 250},
			{"3D", []int{0, 1, 2, 3}, '3', 1, 225, 750},
			{"zero values without mode", []int{1, 2, 3}, '3', 0, 0, 0},
			{"mode only", []int{0}, '2', 1, 125, 250},
			{"DOP only", []int{1}, '3', 1, 125, 250},
			{"correction only", []int{2}, '3', 1, 125, 250},
			{"error only", []int{3}, '3', 1, 125, 250},
			{"large source values", []int{0, 1, 2, 3}, '3', 1, ^uint32(0), ^uint32(0)},
		} {
			t.Run(order.String()+"/"+tc.name, func(t *testing.T) {
				quality := gpsQualityTags(order, tc.mode, tc.differential, tc.dop, tc.errorMeters)
				tags := gpsClockTags(order, "2024:02:29", 12, 1, 34, 1, 56, 1)
				want := map[string]interface{}{}
				values := []interface{}{string(tc.mode), float64(tc.dop) / 100, int(tc.differential), float64(tc.errorMeters) / 100}
				for _, index := range tc.indices {
					tags = append(tags, quality[index])
					want[keys[index]] = values[index]
				}
				result := describeGPSJPEG(t, gpsFixture(order, tags))
				gps := result.FormatInfo["exif"].(map[string]interface{})["gps"].(map[string]interface{})
				for _, key := range keys {
					if !reflect.DeepEqual(gps[key], want[key]) {
						t.Fatalf("missing/defaulted %s: got %#v want %#v", key, gps[key], want[key])
					}
				}
				if result.FormatInfo["exif_status"] != "parsed" || result.Spatial == nil || gps["date_time_utc"] != "2024-02-29T12:34:56Z" {
					t.Fatalf("quality changed valid GPS: %#v", result)
				}
				if result.Spatial.Extent != nil || gps["hdop"] != nil || gps["pdop"] != nil || gps["rtk_status"] != nil {
					t.Fatalf("quality inferred extra facts: %#v", result)
				}
			})
		}
	}
}

func TestJPEGGPSInvalidQualityPreservesOtherSources(t *testing.T) {
	type mutation struct {
		name  string
		apply func(*gpsFixtureTag)
	}
	keys := []string{"measure_mode", "dop", "differential", "horizontal_positioning_error_meters"}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for index, key := range keys {
			mutations := []mutation{
				{"type", func(tag *gpsFixtureTag) { tag.typ = 7 }},
				{"count", func(tag *gpsFixtureTag) {
					tag.count = 2
					if tag.id == 10 {
						tag.count = 3
					}
				}},
			}
			switch index {
			case 0:
				mutations = append(mutations, mutation{"reserved mode", func(tag *gpsFixtureTag) { tag.value[0] = '4' }}, mutation{"unterminated mode", func(tag *gpsFixtureTag) { tag.value[1] = 'X' }})
			case 2:
				mutations = append(mutations, mutation{"reserved correction", func(tag *gpsFixtureTag) { order.PutUint16(tag.value, 2) }})
			default:
				mutations = append(mutations, mutation{"zero denominator", func(tag *gpsFixtureTag) { order.PutUint32(tag.value[4:], 0) }})
			}
			for _, m := range mutations {
				t.Run(order.String()+"/"+key+"/"+m.name, func(t *testing.T) {
					quality := gpsQualityTags(order, '3', 1, 125, 250)
					m.apply(&quality[index])
					tags := append(gpsClockTags(order, "2024:02:29", 12, 1, 34, 1, 56, 1), quality...)
					result := describeGPSJPEG(t, gpsFixture(order, tags))
					exif := result.FormatInfo["exif"].(map[string]interface{})
					gps := exif["gps"].(map[string]interface{})
					if result.FormatInfo["exif_status"] != "invalid" || gps[key] != nil || result.Spatial != nil || gps["date_time_utc"] != nil {
						t.Fatalf("invalid quality leaked facts: %#v", result)
					}
					for i, other := range keys {
						if i != index && gps[other] == nil {
							t.Fatalf("lost independent %s: %#v", other, gps)
						}
					}
					if gps["date_stamp"] != "2024:02:29" || gps["time_hms"] == nil || exif["orientation"] != 6 {
						t.Fatalf("lost unrelated source: %#v", result)
					}
				})
			}
		}
		for _, index := range []int{1, 3} {
			t.Run(order.String()+"/"+keys[index]+"/outside APP1", func(t *testing.T) {
				base := gpsFixtureTags(order)
				tags := append(base, gpsQualityTags(order, '2', 0, 125, 250)...)
				data := gpsFixture(order, tags)
				order.PutUint32(data[40+(len(base)+index)*12+8:], uint32(len(data)+100))
				result := describeGPSJPEG(t, data)
				gps := result.FormatInfo["exif"].(map[string]interface{})["gps"].(map[string]interface{})
				if result.FormatInfo["exif_status"] != "invalid" || gps[keys[index]] != nil || gps["measure_mode"] != "2" || gps["differential"] != 0 || result.Spatial != nil {
					t.Fatalf("offset not diagnosed independently: %#v", result)
				}
			})
		}
	}
}
