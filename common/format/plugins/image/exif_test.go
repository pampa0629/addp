package image

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	stdimage "image"
	"image/jpeg"
	"io"
	"reflect"
	"testing"

	"github.com/addp/common/format"
)

func exifFixture(order binary.ByteOrder, orientation uint16) []byte {
	data := make([]byte, 8+2+4*12+4+2+3*12+4)
	copy(data, "II")
	if order == binary.BigEndian {
		copy(data, "MM")
	}
	order.PutUint16(data[2:4], 42)
	order.PutUint32(data[4:8], 8)
	order.PutUint16(data[8:10], 4)
	order.PutUint16(data[62:64], 3)
	entry := func(offset int, tag, typ uint16, value []byte) {
		order.PutUint16(data[offset:offset+2], tag)
		order.PutUint16(data[offset+2:offset+4], typ)
		count := len(value)
		if typ == 3 {
			count /= 2
		} else if typ == 4 {
			count /= 4
		}
		order.PutUint32(data[offset+4:offset+8], uint32(count))
		if len(value) <= 4 {
			copy(data[offset+8:offset+12], value)
		} else {
			order.PutUint32(data[offset+8:offset+12], uint32(len(data)))
			data = append(data, value...)
		}
	}
	short := make([]byte, 2)
	order.PutUint16(short, orientation)
	entry(10, 274, 3, short)
	entry(22, 271, 2, []byte("Drone Maker\x00"))
	entry(34, 272, 2, []byte("Camera Model\x00"))
	pointer := make([]byte, 4)
	order.PutUint32(pointer, 62)
	entry(46, 34665, 4, pointer)
	entry(64, 36867, 2, []byte("2026:10:04 10:11:12\x00"))
	entry(76, 36881, 2, []byte("+08:00\x00"))
	entry(88, 37521, 2, []byte("007\x00"))
	return data
}

func jpegFixture(t *testing.T, payloads ...[]byte) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, stdimage.NewRGBA(stdimage.Rect(0, 0, 2, 3)), nil); err != nil {
		t.Fatal(err)
	}
	data := append([]byte{}, encoded.Bytes()[:2]...)
	data = append(data, jpegSegment(0xe0, []byte{'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0})...)
	for _, payload := range payloads {
		data = append(data, jpegSegment(0xe1, payload)...)
	}
	return append(data, encoded.Bytes()[2:]...)
}

func jpegSegment(marker byte, payload []byte) []byte {
	segment := []byte{0xff, marker, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	return append(segment, payload...)
}

func describeJPEG(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	provider, err := format.GetMediaInfoProvider(format.FormatJPEG)
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.DescribeMedia(context.Background(), bytes.NewBuffer(data), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Media.Width != 2 || result.Media.Height != 3 || result.Spatial != nil {
		t.Fatalf("raw pixel dimensions or spatial facts changed: %#v", result)
	}
	return result.FormatInfo
}

func TestJPEGExifByteOrderAndAllOrientations(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for orientation := uint16(1); orientation <= 8; orientation++ {
			info := describeJPEG(t, jpegFixture(t, append([]byte("Exif\x00\x00"), exifFixture(order, orientation)...)))
			want := map[string]interface{}{"orientation": int(orientation), "make": "Drone Maker", "model": "Camera Model",
				"date_time_original": "2026:10:04 10:11:12", "offset_time_original": "+08:00", "subsec_time_original": "007"}
			if info["exif_status"] != "parsed" || !reflect.DeepEqual(info["exif"], want) {
				t.Fatalf("%s orientation %d: %#v", order, orientation, info)
			}
		}
	}
}

func TestJPEGWithoutExifOrWithXMP(t *testing.T) {
	for _, payloads := range [][][]byte{nil, {[]byte("http://ns.adobe.com/xap/1.0/\x00<xmp/>")}} {
		info := describeJPEG(t, jpegFixture(t, payloads...))
		if info["exif_status"] != "absent" || info["exif"] != nil {
			t.Fatalf("non-Exif metadata treated as Exif: %#v", info)
		}
	}
}

func TestJPEGInvalidExifPreservesDimensionsAndValidFields(t *testing.T) {
	for name, mutate := range map[string]func([]byte){
		"orientation range":   func(d []byte) { binary.LittleEndian.PutUint16(d[18:20], 9) },
		"orientation type":    func(d []byte) { binary.LittleEndian.PutUint16(d[12:14], 4) },
		"orientation count":   func(d []byte) { binary.LittleEndian.PutUint32(d[14:18], 2) },
		"duplicate tags":      func(d []byte) { binary.LittleEndian.PutUint16(d[22:24], 274) },
		"camera offset":       func(d []byte) { binary.LittleEndian.PutUint32(d[30:34], 0xffffffff) },
		"camera count budget": func(d []byte) { binary.LittleEndian.PutUint32(d[26:30], 0xffffffff) },
		"camera invalid UTF8": func(d []byte) { offset := binary.LittleEndian.Uint32(d[30:34]); d[offset] = 0xff },
		"subIFD offset":       func(d []byte) { binary.LittleEndian.PutUint32(d[54:58], 0xffffffff) },
		"subIFD cycle":        func(d []byte) { binary.LittleEndian.PutUint32(d[54:58], 8) },
		"bad date": func(d []byte) {
			offset := binary.LittleEndian.Uint32(d[72:76])
			copy(d[offset:], "2026:02:30 10:11:12")
		},
		"bad timezone":  func(d []byte) { offset := binary.LittleEndian.Uint32(d[84:88]); copy(d[offset:], "+25:00") },
		"bad subsecond": func(d []byte) { copy(d[96:100], "x07\x00") },
	} {
		t.Run(name, func(t *testing.T) {
			data := exifFixture(binary.LittleEndian, 6)
			mutate(data)
			info := describeJPEG(t, jpegFixture(t, append([]byte("Exif\x00\x00"), data...)))
			if info["exif_status"] != "invalid" {
				t.Fatalf("illegal Exif accepted: %#v", info)
			}
		})
	}
	for _, data := range [][]byte{[]byte("Exif\x00\x00II"), []byte("Exif"), append([]byte("Exif\x00\x00"), make([]byte, 8)...)} {
		if info := describeJPEG(t, jpegFixture(t, data)); info["exif_status"] != "invalid" {
			t.Fatalf("malformed Exif accepted: %#v", info)
		}
	}
}

func TestJPEGDuplicateExifDoesNotChooseAnArbitrarySource(t *testing.T) {
	payload := append([]byte("Exif\x00\x00"), exifFixture(binary.LittleEndian, 6)...)
	info := describeJPEG(t, jpegFixture(t, payload, payload))
	if info["exif_status"] != "invalid" || info["exif"] != nil {
		t.Fatalf("duplicate Exif selected: %#v", info)
	}
}

func TestJPEGUnknownTimezoneAndDateDoNotInventCaptureTime(t *testing.T) {
	data := exifFixture(binary.LittleEndian, 6)
	binary.LittleEndian.PutUint16(data[76:78], 65000) // OffsetTimeOriginal absent.
	info := describeJPEG(t, jpegFixture(t, append([]byte("Exif\x00\x00"), data...)))
	exif := info["exif"].(map[string]interface{})
	if exif["date_time_original"] != "2026:10:04 10:11:12" || exif["offset_time_original"] != nil {
		t.Fatalf("source time changed: %#v", exif)
	}
	copy(data[binary.LittleEndian.Uint32(data[72:76]):], "    :  :     :  :  ")
	info = describeJPEG(t, jpegFixture(t, append([]byte("Exif\x00\x00"), data...)))
	if info["exif_status"] != "parsed" || info["exif"].(map[string]interface{})["date_time_original"] != nil {
		t.Fatalf("unknown timestamp inferred or rejected: %#v", info)
	}
}

type countedImageReader struct {
	reader io.Reader
	read   int
}

func (r *countedImageReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += n
	return n, err
}

func TestJPEGMetadataBudgetAndLargeImage(t *testing.T) {
	base := jpegFixture(t)
	scan := bytes.Index(base, []byte{0xff, 0xda})
	padding := bytes.Repeat(jpegSegment(0xe2, make([]byte, 65533)), 20)
	for name, data := range map[string][]byte{
		"large pixels": append(append([]byte{}, base...), make([]byte, 8<<20)...),
		"large header": append(append(append([]byte{}, base[:scan]...), padding...), base[scan:]...),
	} {
		t.Run(name, func(t *testing.T) {
			reader := &countedImageReader{reader: bytes.NewBuffer(data)}
			provider, _ := format.GetMediaInfoProvider(format.FormatJPEG)
			result, err := provider.DescribeMedia(context.Background(), reader, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := "absent"
			if name == "large header" {
				want = "budget_exceeded"
			}
			if reader.read > 1<<20 || result.FormatInfo["exif_status"] != want {
				t.Fatalf("read=%d result=%#v", reader.read, result)
			}
		})
	}
}

func TestJPEGSizeOutsideBudgetReturnsErrorInsteadOfGuessing(t *testing.T) {
	base := jpegFixture(t)
	base = append(append([]byte{}, base[:2]...), base[20:]...) // No JFIF: config needs SOS.
	scan := bytes.Index(base, []byte{0xff, 0xda})
	padding := bytes.Repeat(jpegSegment(0xe2, make([]byte, 65533)), 20)
	data := append(append(append([]byte{}, base[:scan]...), padding...), base[scan:]...)
	reader := &countedImageReader{reader: bytes.NewBuffer(data)}
	provider, _ := format.GetMediaInfoProvider(format.FormatJPEG)
	result, err := provider.DescribeMedia(context.Background(), reader, nil)
	if err == nil || result != nil || reader.read > 1<<20 {
		t.Fatalf("size inferred outside budget: %#v %v read=%d", result, err, reader.read)
	}
}

func TestMalformedJPEGMetadataAfterConfirmedDimensions(t *testing.T) {
	base := jpegFixture(t)
	scan := bytes.Index(base, []byte{0xff, 0xda})
	for _, tail := range [][]byte{
		{0xff, 0xe1, 0, 1},            // Illegal segment length.
		{0xff, 0xe1, 0, 30, 'E', 'x'}, // Truncated APP1.
		{0xff, 0},                     // Escaped entropy byte outside a scan.
	} {
		data := append(append([]byte{}, base[:scan]...), tail...)
		if info := describeJPEG(t, data); info["exif_status"] != "invalid" {
			t.Fatalf("bad marker accepted: %#v", info)
		}
	}
}

func TestGenericImageProviderUsesJPEGExifPath(t *testing.T) {
	provider, _ := format.GetMediaInfoProvider(format.FormatImage)
	payload := append([]byte("Exif\x00\x00"), exifFixture(binary.BigEndian, 8)...)
	result, err := provider.DescribeMedia(context.Background(), bytes.NewBuffer(jpegFixture(t, payload)), nil)
	if err != nil || result.FormatInfo["exif_status"] != "parsed" {
		t.Fatalf("JPEG path not reused: %#v %v", result, err)
	}
}

func TestJPEGWithoutJFIFUsesSameExifPath(t *testing.T) {
	payload := append([]byte("Exif\x00\x00"), exifFixture(binary.LittleEndian, 3)...)
	data := jpegFixture(t, payload)
	data = append(append([]byte{}, data[:2]...), data[20:]...)
	if info := describeJPEG(t, data); info["exif_status"] != "parsed" {
		t.Fatalf("non-JFIF JPEG: %#v", info)
	}
}

type failingImageReader struct {
	data []byte
	err  error
}

func (r *failingImageReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data[:min(len(r.data), 7)])
	r.data = r.data[n:]
	return n, nil
}

func TestJPEGMetadataIOErrorsAndCancellationAreNotAbsence(t *testing.T) {
	data := jpegFixture(t)
	scan := bytes.Index(data, []byte{0xff, 0xda})
	provider, _ := format.GetMediaInfoProvider(format.FormatJPEG)
	want := errors.New("source read failed")
	if _, err := provider.DescribeMedia(context.Background(), &failingImageReader{data: data[:scan], err: want}, nil); !errors.Is(err, want) {
		t.Fatalf("I/O failure swallowed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.DescribeMedia(ctx, bytes.NewBuffer(data), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation swallowed: %v", err)
	}
}

func exifExposureFixture(order binary.ByteOrder, biasNumerator, biasDenominator int32) []byte {
	// IFD0 at 8: Orientation and ExifIFD pointer. Exif IFD at 38: four
	// single rationals, followed by their out-of-line payloads at 92.
	data := make([]byte, 92)
	copy(data, "II")
	if order == binary.BigEndian {
		copy(data, "MM")
	}
	order.PutUint16(data[2:4], 42)
	order.PutUint32(data[4:8], 8)
	order.PutUint16(data[8:10], 2)
	copy(data[10:22], buildIFDEntry(order, 274, tiffTypeShort, 1, 0))
	order.PutUint16(data[18:20], 6)
	copy(data[22:34], buildIFDEntry(order, 34665, tiffTypeLong, 1, 38))
	order.PutUint16(data[38:40], 4)
	for index, value := range []struct {
		tag, typ               uint16
		numerator, denominator uint32
	}{
		{33434, tiffTypeRational, 1, 125},
		{33437, tiffTypeRational, 28, 10},
		{37380, tiffTypeSRational, uint32(biasNumerator), uint32(biasDenominator)},
		{37386, tiffTypeRational, 35, 1},
	} {
		entry := 40 + index*12
		copy(data[entry:entry+12], buildIFDEntry(order, value.tag, value.typ, 1, uint32(len(data))))
		raw := make([]byte, 8)
		order.PutUint32(raw[:4], value.numerator)
		order.PutUint32(raw[4:], value.denominator)
		data = append(data, raw...)
	}
	return data
}

func TestJPEGExifExposureByteOrderAndSignedFractions(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, bias := range [][2]int32{{-1, 2}, {0, 1}, {2, 3}, {-1, -2}, {-2147483648, -1}} {
			data := exifExposureFixture(order, bias[0], bias[1])
			info := describeJPEG(t, jpegFixture(t, append([]byte("Exif\x00\x00"), data...)))
			want := map[string]interface{}{
				"orientation": 6, "exposure_time_seconds": float64(1) / 125,
				"f_number": 2.8, "exposure_bias_ev": float64(bias[0]) / float64(bias[1]), "focal_length_mm": float64(35),
			}
			if info["exif_status"] != "parsed" || !reflect.DeepEqual(info["exif"], want) {
				t.Fatalf("%s bias %v: %#v", order, bias, info)
			}
		}
	}
}

func TestJPEGExifInvalidExposureOmitsOnlyBadField(t *testing.T) {
	fields := []string{"exposure_time_seconds", "f_number", "exposure_bias_ev", "focal_length_mm"}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for index, field := range fields {
			for _, bad := range []string{"type", "zero count", "multiple count", "offset", "zero denominator", "zero positive value"} {
				if index == 2 && bad == "zero positive value" {
					continue // Exposure bias zero is valid.
				}
				t.Run(order.String()+"/"+field+"/"+bad, func(t *testing.T) {
					data := exifExposureFixture(order, -1, 2)
					entry := 40 + index*12
					payload := int(order.Uint32(data[entry+8 : entry+12]))
					switch bad {
					case "type":
						wrongType := uint16(tiffTypeSRational)
						if index == 2 {
							wrongType = tiffTypeRational
						}
						order.PutUint16(data[entry+2:entry+4], wrongType)
					case "zero count":
						order.PutUint32(data[entry+4:entry+8], 0)
					case "multiple count":
						order.PutUint32(data[entry+4:entry+8], 2)
					case "offset":
						order.PutUint32(data[entry+8:entry+12], 0xffffffff)
					case "zero denominator":
						order.PutUint32(data[payload+4:payload+8], 0)
					case "zero positive value":
						order.PutUint32(data[payload:payload+4], 0)
					}
					info := describeJPEG(t, jpegFixture(t, append([]byte("Exif\x00\x00"), data...)))
					exif := info["exif"].(map[string]interface{})
					if info["exif_status"] != "invalid" || exif[field] != nil || len(exif) != 4 || exif["orientation"] != 6 {
						t.Fatalf("bad exposure accepted or valid fields lost: %#v", info)
					}
				})
			}
		}
	}
}

func TestJPEGExifDoesNotDeriveMissingExposureFromAPEX(t *testing.T) {
	data := exifExposureFixture(binary.LittleEndian, 0, 1)
	binary.LittleEndian.PutUint16(data[40:42], 37377) // ShutterSpeedValue, not ExposureTime.
	binary.LittleEndian.PutUint16(data[52:54], 37378) // ApertureValue, not FNumber.
	info := describeJPEG(t, jpegFixture(t, append([]byte("Exif\x00\x00"), data...)))
	exif := info["exif"].(map[string]interface{})
	if info["exif_status"] != "parsed" || exif["exposure_time_seconds"] != nil || exif["f_number"] != nil {
		t.Fatalf("missing exposure derived from different tags: %#v", info)
	}
}

func exifSensitivityFixture(order binary.ByteOrder, values []uint16, kind uint16) []byte {
	data := make([]byte, 104) // IFD0 at 8, five-tag Exif IFD at 38.
	copy(data, "II")
	if order == binary.BigEndian {
		copy(data, "MM")
	}
	order.PutUint16(data[2:4], 42)
	order.PutUint32(data[4:8], 8)
	order.PutUint16(data[8:10], 2)
	copy(data[10:22], buildIFDEntry(order, 274, tiffTypeShort, 1, 0))
	order.PutUint16(data[18:20], 6)
	copy(data[22:34], buildIFDEntry(order, 34665, tiffTypeLong, 1, 38))
	order.PutUint16(data[38:40], 5)
	copy(data[40:52], buildIFDEntry(order, 34855, tiffTypeShort, uint32(len(values)), 0))
	raw := make([]byte, len(values)*2)
	for i, value := range values {
		order.PutUint16(raw[i*2:], value)
	}
	if len(raw) <= 4 {
		copy(data[48:52], raw)
	} else {
		order.PutUint32(data[48:52], uint32(len(data)))
		data = append(data, raw...)
	}
	copy(data[52:64], buildIFDEntry(order, 34864, tiffTypeShort, 1, 0))
	order.PutUint16(data[60:62], kind)
	for i, tag := range []uint16{34865, 34866, 34867} {
		copy(data[64+i*12:76+i*12], buildIFDEntry(order, tag, tiffTypeLong, 1, []uint32{80000, 102400, 0xffffffff}[i]))
	}
	return data
}

func TestJPEGExifSensitivityPreservesDefinitionsAndSourceValues(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, values := range [][]uint16{{100}, {200, 400}, {800, 1600, 65535}} {
			for kind := uint16(0); kind <= 7; kind++ {
				data := exifSensitivityFixture(order, values, kind)
				info := describeJPEG(t, jpegFixture(t, append([]byte("Exif\x00\x00"), data...)))
				want := map[string]interface{}{"orientation": 6, "photographic_sensitivity": values, "sensitivity_type": uint32(kind),
					"standard_output_sensitivity": uint32(80000), "recommended_exposure_index": uint32(102400), "iso_speed": uint32(0xffffffff)}
				if info["exif_status"] != "parsed" || !reflect.DeepEqual(info["exif"], want) {
					t.Fatalf("%s values=%v type=%d: %#v", order, values, kind, info)
				}
			}
		}
	}
}

func TestJPEGExifInvalidSensitivityOmitsOnlyBadField(t *testing.T) {
	fields := []string{"photographic_sensitivity", "sensitivity_type", "standard_output_sensitivity", "recommended_exposure_index", "iso_speed"}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for i, field := range fields {
			for _, bad := range []string{"type", "zero count", "multiple scalar count", "huge count", "invalid value", "offset"} {
				if i == 0 && bad == "multiple scalar count" || i > 0 && bad == "offset" {
					continue
				}
				t.Run(order.String()+"/"+field+"/"+bad, func(t *testing.T) {
					data := exifSensitivityFixture(order, []uint16{100, 200, 65535}, 3)
					entry := 40 + i*12
					switch bad {
					case "type":
						order.PutUint16(data[entry+2:entry+4], tiffTypeRational)
					case "zero count":
						order.PutUint32(data[entry+4:entry+8], 0)
					case "multiple scalar count":
						order.PutUint32(data[entry+4:entry+8], 2)
					case "huge count":
						order.PutUint32(data[entry+4:entry+8], 0xffffffff)
					case "invalid value":
						if i == 0 {
							order.PutUint16(data[106:108], 0) // A zero invalidates the complete array.
						} else if i == 1 {
							order.PutUint16(data[entry+8:entry+10], 8)
						} else {
							order.PutUint32(data[entry+8:entry+12], 0)
						}
					case "offset":
						order.PutUint32(data[entry+8:entry+12], 0xffffffff)
					}
					info := describeJPEG(t, jpegFixture(t, append([]byte("Exif\x00\x00"), data...)))
					exif := info["exif"].(map[string]interface{})
					if info["exif_status"] != "invalid" || exif[field] != nil || len(exif) != 5 || exif["orientation"] != 6 {
						t.Fatalf("bad sensitivity accepted or valid fields lost: %#v", info)
					}
				})
			}
		}
	}
}

func TestJPEGExifSensitivityDoesNotInferMissingTypesOrISO(t *testing.T) {
	data := exifSensitivityFixture(binary.LittleEndian, []uint16{65535}, 3)
	for i := 1; i < 5; i++ {
		binary.LittleEndian.PutUint16(data[40+i*12:42+i*12], uint16(65000+i))
	}
	info := describeJPEG(t, jpegFixture(t, append([]byte("Exif\x00\x00"), data...)))
	want := map[string]interface{}{"orientation": 6, "photographic_sensitivity": []uint16{65535}}
	if info["exif_status"] != "parsed" || !reflect.DeepEqual(info["exif"], want) {
		t.Fatalf("missing sensitivity definition or extended value inferred: %#v", info)
	}
}
