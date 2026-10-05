package image

import (
	"encoding/binary"
	"strings"

	"github.com/addp/common/datatype"
)

// exifSubIFD validates the two independent IFD0 child directories. Neither
// directory may overlap IFD0 or the other child directory; no links are followed.
func exifSubIFD(root *tiffIFD, metadata tiffMetadata, order binary.ByteOrder, rootOffset uint32, tag uint16) (*tiffIFD, bool) {
	entry := root.tags[tag]
	offset, ok := root.firstLong(tag)
	if !ok || entry.typ != tiffTypeLong || entry.count != 1 || offset < 8 {
		return nil, false
	}
	child, ok := parseExifIFD(metadata, order, offset)
	if !ok {
		return nil, false
	}
	start, end := uint64(offset), uint64(offset)+uint64(2+len(child.tags)*12+4)
	rootStart, rootEnd := uint64(rootOffset), uint64(rootOffset)+uint64(2+len(root.tags)*12+4)
	if start < rootEnd && rootStart < end {
		return nil, false
	}
	otherTag := uint16(34665)
	if tag == otherTag {
		otherTag = 34853
	}
	if otherEntry, present := root.tags[otherTag]; present && otherEntry.typ == tiffTypeLong && otherEntry.count == 1 {
		otherOffset, valid := root.firstLong(otherTag)
		if count, available := metadata.slice(uint64(otherOffset), 2); valid && otherOffset >= 8 && available {
			otherStart := uint64(otherOffset)
			otherEnd := otherStart + uint64(2+int(order.Uint16(count))*12+4)
			if start < otherEnd && otherStart < end {
				return nil, false
			}
		}
	}
	return child, true
}

func extractGPSIFD(root *tiffIFD, metadata tiffMetadata, order binary.ByteOrder, rootOffset uint32) (map[string]interface{}, bool) {
	if !root.hasTag(34853) {
		return nil, true
	}
	ifd, ok := exifSubIFD(root, metadata, order, rootOffset, 34853)
	if !ok {
		return nil, false
	}
	gps := map[string]interface{}{}
	version, valid := exifBytes(ifd, 0, 4)
	if valid {
		gps["version_id"] = []int{int(version[0]), int(version[1]), int(version[2]), int(version[3])}
	}
	for _, field := range []struct {
		tag          uint16
		key, allowed string
	}{
		{1, "latitude_ref", "NS"}, {3, "longitude_ref", "EW"}, {9, "status", "AV"},
	} {
		if !ifd.hasTag(field.tag) {
			continue
		}
		value, ok := exifText(ifd, field.tag)
		ok = ok && ifd.tags[field.tag].count == 2 && len(value) == 1 && strings.Contains(field.allowed, value)
		valid = valid && ok
		if ok {
			gps[field.key] = value
		}
	}
	for _, field := range []struct {
		tag     uint16
		key     string
		maximum float64
	}{
		{2, "latitude_dms", 90}, {4, "longitude_dms", 180},
	} {
		if !ifd.hasTag(field.tag) {
			continue
		}
		values, ok := exifDMS(ifd, field.tag, field.maximum)
		valid = valid && ok
		if ok {
			gps[field.key] = values
		}
	}
	if ifd.hasTag(5) {
		ref, ok := exifBytes(ifd, 5, 1)
		ok = ok && ref[0] <= 1
		valid = valid && ok
		if ok {
			gps["altitude_ref"] = int(ref[0])
		}
	}
	if ifd.hasTag(6) {
		value, ok := exifRational(ifd, 6, false)
		valid = valid && ok
		if ok {
			gps["altitude_meters"] = value
		}
	}
	if ifd.hasTag(18) {
		value, ok := exifText(ifd, 18)
		for _, char := range value {
			ok = ok && char < 128
		}
		valid = valid && ok
		if ok && value != "" {
			gps["map_datum"] = value
		}
	}
	clockValid := extractGPSTime(ifd, gps, valid)
	return gps, valid && clockValid
}

func exifBytes(ifd *tiffIFD, tag uint16, count uint32) ([]byte, bool) {
	entry := ifd.tags[tag]
	if entry.typ != tiffTypeByte || entry.count != count {
		return nil, false
	}
	raw, ok := ifd.tagBytes(entry)
	return raw, ok && len(raw) == int(count)
}

func exifDMS(ifd *tiffIFD, tag uint16, maximum float64) ([]float64, bool) {
	values, ok := exifRationalTriplet(ifd, tag)
	if !ok || values[1] >= 60 || values[2] >= 60 || values[0]+values[1]/60+values[2]/3600 > maximum {
		return nil, false
	}
	return values, true
}

func exifRationalTriplet(ifd *tiffIFD, tag uint16) ([]float64, bool) {
	entry := ifd.tags[tag]
	if entry.typ != tiffTypeRational || entry.count != 3 {
		return nil, false
	}
	raw, ok := ifd.tagBytes(entry)
	if !ok || len(raw) != 24 {
		return nil, false
	}
	values := make([]float64, 3)
	for i := range values {
		denominator := ifd.order.Uint32(raw[i*8+4 : i*8+8])
		if denominator == 0 {
			return nil, false
		}
		values[i] = float64(ifd.order.Uint32(raw[i*8:i*8+4])) / float64(denominator)
	}
	return values, true
}

// jpegCaptureLocation consumes the supported GPS facts from the same parse.
// A point never supplies the raster's CRS, coverage or vertical reference.
func jpegCaptureLocation(info map[string]interface{}) *datatype.SpatialInfo {
	if info["exif_status"] != "parsed" {
		return nil
	}
	exif, _ := info["exif"].(map[string]interface{})
	gps, _ := exif["gps"].(map[string]interface{})
	lat, latOK := gps["latitude_dms"].([]float64)
	lon, lonOK := gps["longitude_dms"].([]float64)
	latRef, lonRef := gps["latitude_ref"], gps["longitude_ref"]
	if !latOK || !lonOK || latRef != "N" && latRef != "S" || lonRef != "E" && lonRef != "W" || gps["status"] == "V" {
		return nil
	}
	point := &datatype.CaptureLocation{Latitude: lat[0] + lat[1]/60 + lat[2]/3600, Longitude: lon[0] + lon[1]/60 + lon[2]/3600}
	if latRef == "S" {
		point.Latitude = -point.Latitude
	}
	if lonRef == "W" {
		point.Longitude = -point.Longitude
	}
	point.Datum, _ = gps["map_datum"].(string)
	if strings.EqualFold(strings.TrimSpace(point.Datum), "WGS-84") {
		srid := 4326
		point.SRID = &srid
	}
	return &datatype.SpatialInfo{CaptureLocation: point}
}
