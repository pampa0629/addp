package metaenrich

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/color"
	"image/gif"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
)

func TestAnimationSummaryUsesFormatFactsAndImageIdentity(t *testing.T) {
	g := &gif.GIF{Delay: []int{2, 3}}
	for range 2 {
		g.Image = append(g.Image, image.NewPaletted(image.Rect(0, 0, 7, 5), color.Palette{color.Black, color.White}))
	}
	var gifData bytes.Buffer
	if err := gif.EncodeAll(&gifData, g); err != nil {
		t.Fatal(err)
	}
	// Deterministic two-frame lossless WebP encoded independently with Pillow.
	webpData, err := hex.DecodeString("524946468400000057454250565038580a00000002000000060000040000414e494d06000000000000000000414e4d4628000000000000000000060000040000140000025650384c0f0000002f060001000710fd8ffe0722a2ff0100414e4d46280000000000000000000600000400001e0000005650384c0f0000002f060001000710d1fffe0722a2ff0100")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		kind    format.FormatType
		content []byte
	}{
		{format.FormatGIF, gifData.Bytes()}, {format.FormatWebP, webpData},
	} {
		t.Run(string(tt.kind), func(t *testing.T) {
			path := "images/animation." + string(tt.kind)
			size := int64(len(tt.content))
			item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{
				Layout: format.LayoutSingle, DataType: datatype.Unknown, Format: string(format.FormatUnknown), PrimaryContentPath: path, SizeBytes: &size,
			}, PhysicalPath: path}
			attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
			enriched, _, err := EnrichResourceAttributes(t.Context(), attrs, ResourceAttributesInput{
				ContentReader: bytesContentReader{content: tt.content}, Item: item, PhysicalPath: path, SizeBytes: size,
				EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) },
			})
			if err != nil {
				t.Fatal(err)
			}
			media := commonJSON.Section(attrs, "type_info.media")
			info := commonJSON.Section(attrs, "format_info."+string(tt.kind)+".animation")
			if enriched.DataType != datatype.Media || enriched.Format != string(tt.kind) || media["kind"] != "image" ||
				commonJSON.InterfaceInt64(media["width"]) != 7 || commonJSON.InterfaceInt64(media["height"]) != 5 ||
				info["summary_status"] != "parsed" || commonJSON.InterfaceInt64(info["frame_count"]) != 2 || commonJSON.InterfaceInt64(info["duration_ms"]) != 50 {
				t.Fatalf("wrong animation facts: %#v", attrs)
			}
			if media["duration_ms"] != nil || media["frame_count"] != nil || len(commonJSON.Section(attrs, "capabilities.spatial")) != 0 {
				t.Fatalf("format facts escaped: %#v", attrs)
			}
			// Known-item refresh reuses attributes. A truncated source must replace
			// its old summary, rather than retain frame facts from the last scan.
			_, _, err = EnrichResourceAttributes(t.Context(), attrs, ResourceAttributesInput{
				ContentReader: bytesContentReader{content: tt.content[:len(tt.content)-1]}, Item: enriched, PhysicalPath: path, SizeBytes: size - 1,
				EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) },
			})
			if err != nil {
				t.Fatal(err)
			}
			info = commonJSON.Section(attrs, "format_info."+string(tt.kind)+".animation")
			if info["summary_status"] != "invalid" || info["frame_count"] != nil || info["duration_ms"] != nil {
				t.Fatalf("stale animation facts survived refresh: %#v", attrs)
			}
		})
	}
}
