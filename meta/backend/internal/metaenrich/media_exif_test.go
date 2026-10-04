package metaenrich

import (
	"bytes"
	"context"
	"encoding/binary"
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

func TestEnrichJPEGExifStaysInFormatInfoAndPreservesPixelDimensions(t *testing.T) {
	t.Parallel()
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 3)), nil); err != nil {
		t.Fatal(err)
	}
	// IFD0 holds Orientation, Make and the Exif IFD pointer. Exposure
	// fields remain format facts; no GPS or capture timestamp is present.
	var metadata bytes.Buffer
	metadata.Write([]byte{'I', 'I', 42, 0, 8, 0, 0, 0, 3, 0})
	entry := func(tag, typ uint16, count, value uint32) {
		_ = binary.Write(&metadata, binary.LittleEndian, tag)
		_ = binary.Write(&metadata, binary.LittleEndian, typ)
		_ = binary.Write(&metadata, binary.LittleEndian, count)
		_ = binary.Write(&metadata, binary.LittleEndian, value)
	}
	entry(274, 3, 1, 6)
	entry(271, 2, 4, 0x00494a44) // Inline "DJI\x00".
	entry(34665, 4, 1, 50)
	_ = binary.Write(&metadata, binary.LittleEndian, uint32(0))
	_ = binary.Write(&metadata, binary.LittleEndian, uint16(4))
	entry(33434, 5, 1, 104)
	entry(33437, 5, 1, 112)
	entry(37380, 10, 1, 120)
	entry(37386, 5, 1, 128)
	_ = binary.Write(&metadata, binary.LittleEndian, uint32(0))
	_ = binary.Write(&metadata, binary.LittleEndian, []uint32{1, 125, 28, 10, 0, 1, 35, 1})
	payload := append([]byte("Exif\x00\x00"), metadata.Bytes()...)
	content := append([]byte{}, encoded.Bytes()[:2]...)
	header := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(header[2:], uint16(len(payload)+2))
	content = append(content, header...)
	content = append(content, payload...)
	content = append(content, encoded.Bytes()[2:]...)
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout: format.LayoutSingle, DataType: datatype.Unknown,
			Format: string(format.FormatUnknown), PrimaryContentPath: "images/drone.jpg", SizeBytes: &size,
		},
		PhysicalPath: "images/drone.jpg",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content}, Item: item, PhysicalPath: item.PhysicalPath, SizeBytes: size,
		EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if enriched.DataType != datatype.Media || enriched.Format != string(format.FormatJPEG) {
		t.Fatalf("JPEG identity changed: %s/%s", enriched.DataType, enriched.Format)
	}
	media := commonJSON.Section(attrs, "type_info.media")
	exif := commonJSON.Section(attrs, "format_info.jpeg.exif")
	if commonJSON.InterfaceInt64(media["width"]) != 2 || commonJSON.InterfaceInt64(media["height"]) != 3 ||
		commonJSON.InterfaceInt64(exif["orientation"]) != 6 || exif["make"] != "DJI" ||
		exif["exposure_time_seconds"] != float64(1)/125 || exif["f_number"] != 2.8 ||
		exif["exposure_bias_ev"] != float64(0) || exif["focal_length_mm"] != float64(35) ||
		commonJSON.String(attrs, "format_info.jpeg", "exif_status") != "parsed" {
		t.Fatalf("JPEG facts not persisted: %#v", attrs)
	}
	if media["orientation"] != nil || media["exif"] != nil || media["exposure_time_seconds"] != nil || exif["date_time_original"] != nil ||
		len(commonJSON.Section(attrs, "capabilities.spatial")) != 0 {
		t.Fatalf("format facts leaked into type info or invented timestamp/spatial facts: %#v", attrs)
	}
}
