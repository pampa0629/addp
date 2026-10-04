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
	// TIFF IFD0 has Orientation=6 and Make="DJI". Both values are inline;
	// there is no GPS IFD or capture timestamp to infer.
	payload := []byte{'E', 'x', 'i', 'f', 0, 0,
		'I', 'I', 42, 0, 8, 0, 0, 0, 2, 0,
		0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0,
		0x0f, 1, 2, 0, 4, 0, 0, 0, 'D', 'J', 'I', 0,
		0, 0, 0, 0}
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
		commonJSON.String(attrs, "format_info.jpeg", "exif_status") != "parsed" {
		t.Fatalf("JPEG facts not persisted: %#v", attrs)
	}
	if media["orientation"] != nil || media["exif"] != nil || exif["date_time_original"] != nil ||
		len(commonJSON.Section(attrs, "capabilities.spatial")) != 0 {
		t.Fatalf("format facts leaked into type info or invented timestamp/spatial facts: %#v", attrs)
	}
}
