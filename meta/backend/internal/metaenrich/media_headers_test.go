package metaenrich

import (
	"bytes"
	"image"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"golang.org/x/image/bmp"
)

func TestEnrichWebPAndBMPUsesSharedMediaAttributes(t *testing.T) {
	var bitmap bytes.Buffer
	if err := bmp.Encode(&bitmap, image.NewRGBA(image.Rect(0, 0, 7, 5))); err != nil {
		t.Fatal(err)
	}
	// Extended animated WebP canvas, without any frame payload. The header
	// describes the canvas, not frame count, duration or complete-file validity.
	webp := []byte("RIFF\x16\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02\x00\x00\x00\x06\x00\x00\x04\x00\x00")
	for _, tt := range []struct {
		kind    format.FormatType
		content []byte
	}{
		{format.FormatWebP, webp},
		{format.FormatBMP, bitmap.Bytes()},
	} {
		t.Run(string(tt.kind), func(t *testing.T) {
			path := "images/sample." + string(tt.kind)
			size := int64(len(tt.content))
			item := &metaitem.DetectedItem{
				ResolvedItem: dataitem.ResolvedItem{
					Layout: format.LayoutSingle, DataType: datatype.Unknown, Format: string(format.FormatUnknown), PrimaryContentPath: path, SizeBytes: &size,
				},
				PhysicalPath: path,
			}
			attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
			enriched, _, err := EnrichResourceAttributes(t.Context(), attrs, ResourceAttributesInput{
				ContentReader: bytesContentReader{content: tt.content}, Item: item, PhysicalPath: path, SizeBytes: size,
				EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) },
			})
			if err != nil {
				t.Fatal(err)
			}
			if enriched.DataType != datatype.Media || enriched.Format != string(tt.kind) {
				t.Fatalf("wrong identity: %#v", enriched)
			}
			m := commonJSON.Section(attrs, "type_info.media")
			if m["kind"] != "image" || m["encoding"] != string(tt.kind) || m["mime_type"] != "image/"+string(tt.kind) ||
				commonJSON.InterfaceInt64(m["width"]) != 7 || commonJSON.InterfaceInt64(m["height"]) != 5 {
				t.Fatalf("media facts not mapped: %#v", attrs)
			}
			if m["color_space"] != nil || m["duration_ms"] != nil || len(commonJSON.Section(attrs, "format_info")) != 0 || len(commonJSON.Section(attrs, "capabilities.spatial")) != 0 {
				t.Fatalf("invented format/spatial/color facts: %#v", attrs)
			}
		})
	}
}
