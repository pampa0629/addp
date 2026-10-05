package image

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const jpegMetadataReadLimit = 1 << 20

// describeJPEGExif examines only marker segments before the first scan. The
// shared limit includes bytes already read by image.DecodeConfig.
func describeJPEGExif(ctx context.Context, input io.Reader, limit *io.LimitedReader) (map[string]interface{}, error) {
	info := map[string]interface{}{"exif_status": "absent"}
	failure := func(err error) (map[string]interface{}, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, err
		}
		info["exif_status"] = "invalid"
		if limit.N == 0 {
			info["exif_status"] = "budget_exceeded"
		}
		return info, nil
	}
	var word [2]byte
	if _, err := io.ReadFull(input, word[:]); err != nil {
		return failure(err)
	}
	if word != [2]byte{0xff, 0xd8} {
		info["exif_status"] = "invalid"
		return info, nil
	}
	seenExif := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := io.ReadFull(input, word[:]); err != nil {
			return failure(err)
		}
		if word[0] != 0xff {
			info["exif_status"] = "invalid"
			return info, nil
		}
		marker := word[1]
		for marker == 0xff {
			if _, err := io.ReadFull(input, word[1:]); err != nil {
				return failure(err)
			}
			marker = word[1]
		}
		if marker == 0xda || marker == 0xd9 {
			return info, nil
		}
		if marker == 0 || marker == 0xd8 {
			info["exif_status"] = "invalid"
			return info, nil
		}
		if marker == 1 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if _, err := io.ReadFull(input, word[:]); err != nil {
			return failure(err)
		}
		size := int(binary.BigEndian.Uint16(word[:])) - 2
		if size < 0 {
			info["exif_status"] = "invalid"
			return info, nil
		}
		if marker != 0xe1 {
			if _, err := io.CopyN(io.Discard, input, int64(size)); err != nil {
				return failure(err)
			}
			continue
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(input, payload); err != nil {
			return failure(err)
		}
		if !bytes.HasPrefix(payload, []byte("Exif")) {
			continue
		}
		if seenExif {
			delete(info, "exif")
			info["exif_status"] = "invalid"
			return info, nil
		}
		seenExif = true
		if !bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
			info["exif_status"] = "invalid"
			continue
		}
		exif, valid := extractExifSummary(payload[6:])
		info["exif_status"] = "parsed"
		if !valid {
			info["exif_status"] = "invalid"
		}
		if len(exif) > 0 {
			info["exif"] = exif
		}
	}
}

func extractExifSummary(data []byte) (map[string]interface{}, bool) {
	metadata := newTIFFMetadata(data, nil)
	order, ok := metadata.byteOrder()
	if !ok || len(data) < 8 || order.Uint32(data[4:8]) < 8 {
		return nil, false
	}
	if order.Uint16(data[2:4]) != 42 {
		return nil, false
	}
	rootOffset := order.Uint32(data[4:8])
	root, ok := parseExifIFD(metadata, order, rootOffset)
	if !ok {
		return nil, false
	}
	exif := map[string]interface{}{}
	valid := true
	if entry, present := root.tags[274]; present {
		values, ok := root.shorts(274)
		if entry.count == 1 && ok && values[0] >= 1 && values[0] <= 8 {
			exif["orientation"] = int(values[0])
		} else {
			valid = false
		}
	}
	for tag, key := range map[uint16]string{271: "make", 272: "model"} {
		value, ok := exifText(root, tag)
		valid = valid && ok
		if ok && value != "" {
			exif[key] = value
		}
	}
	gps, gpsValid := extractGPSIFD(root, metadata, order, rootOffset)
	valid = valid && gpsValid
	if len(gps) > 0 {
		exif["gps"] = gps
	}
	_, present := root.tags[34665]
	if !present {
		return exif, valid
	}
	sub, ok := exifSubIFD(root, metadata, order, rootOffset, 34665)
	if !ok {
		return exif, false
	}
	for tag, key := range map[uint16]string{36867: "date_time_original", 36881: "offset_time_original", 37521: "subsec_time_original"} {
		value, ok := exifText(sub, tag)
		if entry, present := sub.tags[tag]; present {
			if tag == 36867 && entry.count != 20 || tag == 36881 && entry.count != 7 {
				ok = false
			}
		}
		if ok && value != "" {
			switch tag {
			case 36867:
				value, ok = exifDate(value)
			case 36881:
				ok = len(value) == 6 && (value[0] == '+' || value[0] == '-') && value[3] == ':' &&
					exifDigits(value[1:3]) && exifDigits(value[4:]) && value[1:3] <= "23" && value[4:] <= "59"
			case 37521:
				ok = exifDigits(value)
			}
		}
		valid = valid && ok
		if ok && value != "" {
			exif[key] = value
		}
	}
	for _, field := range []struct {
		tag    uint16
		key    string
		signed bool
	}{
		{33434, "exposure_time_seconds", false},
		{33437, "f_number", false},
		{37380, "exposure_bias_ev", true},
		{37386, "focal_length_mm", false},
	} {
		if !sub.hasTag(field.tag) {
			continue
		}
		value, ok := exifRational(sub, field.tag, field.signed)
		if ok && !field.signed && value <= 0 {
			ok = false
		}
		valid = valid && ok
		if ok {
			exif[field.key] = value
		}
	}
	if sub.hasTag(34855) {
		values, ok := sub.shorts(34855)
		for _, value := range values {
			ok = ok && value > 0
		}
		valid = valid && ok
		if ok {
			// Keep all source values, including the 65535 upper-limit marker.
			// PhotographicSensitivity does not necessarily mean ISO speed.
			exif["photographic_sensitivity"] = values
		}
	}
	for _, field := range []struct {
		tag uint16
		key string
		typ uint16
	}{
		{34864, "sensitivity_type", tiffTypeShort},
		{34865, "standard_output_sensitivity", tiffTypeLong},
		{34866, "recommended_exposure_index", tiffTypeLong},
		{34867, "iso_speed", tiffTypeLong},
	} {
		entry, present := sub.tags[field.tag]
		if !present {
			continue
		}
		value, ok := sub.firstLong(field.tag)
		ok = ok && entry.typ == field.typ && entry.count == 1
		if field.tag == 34864 {
			ok = ok && value <= 7
		} else {
			ok = ok && value > 0
		}
		valid = valid && ok
		if ok {
			exif[field.key] = value
		}
	}
	return exif, valid
}

func exifRational(ifd *tiffIFD, tag uint16, signed bool) (float64, bool) {
	entry := ifd.tags[tag]
	typ := uint16(tiffTypeRational)
	if signed {
		typ = tiffTypeSRational
	}
	if entry.typ != typ || entry.count != 1 {
		return 0, false
	}
	raw, ok := ifd.tagBytes(entry)
	if !ok || len(raw) != 8 {
		return 0, false
	}
	numerator := float64(ifd.order.Uint32(raw[:4]))
	denominator := float64(ifd.order.Uint32(raw[4:]))
	if signed {
		numerator = float64(int32(ifd.order.Uint32(raw[:4])))
		denominator = float64(int32(ifd.order.Uint32(raw[4:])))
	}
	if denominator == 0 {
		return 0, false
	}
	return numerator / denominator, true
}

func parseExifIFD(data tiffMetadata, order binary.ByteOrder, offset uint32) (*tiffIFD, bool) {
	ifd, ok := parseIFDAt(data, order, uint64(offset))
	if !ok {
		return nil, false
	}
	count, _ := data.slice(uint64(offset), 2)
	if int(order.Uint16(count)) != len(ifd.tags) {
		// parseIFDAt reuses TIFF storage decoding; Exif must not pick a value
		// when duplicate tag IDs make the source ambiguous.
		return nil, false
	}
	return ifd, true
}

func exifText(ifd *tiffIFD, tag uint16) (string, bool) {
	entry, present := ifd.tags[tag]
	if !present {
		return "", true
	}
	if entry.typ != tiffTypeASCII || entry.count == 0 || entry.count > 1024 {
		return "", false
	}
	raw, ok := ifd.tagBytes(entry)
	if !ok || len(raw) == 0 || raw[len(raw)-1] != 0 {
		return "", false
	}
	value := bytes.TrimRight(raw, "\x00")
	if !utf8.Valid(value) {
		return "", false
	}
	for _, char := range value {
		if char < 32 || char == 127 {
			return "", false
		}
	}
	if strings.TrimSpace(string(value)) == "" {
		return "", true
	}
	return string(value), true
}

func exifDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func exifDate(value string) (string, bool) {
	if len(value) != 19 {
		return "", false
	}
	unknown := false
	for index, char := range value {
		switch index {
		case 4, 7, 13, 16:
			if char != ':' {
				return "", false
			}
		case 10:
			if char != ' ' {
				return "", false
			}
		default:
			if char == ' ' {
				unknown = true
			} else if char < '0' || char > '9' {
				return "", false
			}
		}
	}
	if unknown {
		return "", true
	}
	_, err := time.Parse("2006:01:02 15:04:05", value)
	return value, err == nil
}
