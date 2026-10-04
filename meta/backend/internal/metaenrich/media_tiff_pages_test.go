package metaenrich

import (
	"context"
	"encoding/binary"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"testing"
)

func TestTIFFPageFactsStayInFormatInfo(t *testing.T) {
	content := make([]byte, 68)
	copy(content, []byte{'I', 'I', 42, 0, 8, 0, 0, 0})
	for i := 0; i < 2; i++ {
		offset := 8 + i*30
		binary.LittleEndian.PutUint16(content[offset:], 2)
		for j, tag := range []uint16{256, 257} {
			entry := content[offset+2+j*12:]
			binary.LittleEndian.PutUint16(entry, tag)
			binary.LittleEndian.PutUint16(entry[2:], 4)
			binary.LittleEndian.PutUint32(entry[4:], 1)
			binary.LittleEndian.PutUint32(entry[8:], uint32(2+i+j))
		}
		if i == 0 {
			binary.LittleEndian.PutUint32(content[offset+26:], 38)
		}
	}
	size := int64(len(content))
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{
		Layout: format.LayoutSingle, DataType: datatype.Unknown, Format: string(format.FormatUnknown), PrimaryContentPath: "images/pages.tiff", SizeBytes: &size,
	}, PhysicalPath: "images/pages.tiff"}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content}, Item: item, PhysicalPath: item.PhysicalPath, SizeBytes: size,
		EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) },
	})
	if err != nil {
		t.Fatal(err)
	}
	media := commonJSON.Section(attrs, "type_info.media")
	info := commonJSON.Section(attrs, "format_info.tiff")
	if enriched.DataType != datatype.Media || enriched.Format != "tiff" || info["page_summary_status"] != "parsed" || commonJSON.InterfaceInt64(info["page_count"]) != 2 ||
		commonJSON.InterfaceInt64(media["width"]) != 2 || commonJSON.InterfaceInt64(media["height"]) != 3 {
		t.Fatalf("TIFF facts=%#v", attrs)
	}
	if media["pages"] != nil || media["page_count"] != nil || len(commonJSON.Section(attrs, "capabilities.spatial")) != 0 {
		t.Fatalf("format facts escaped: %#v", attrs)
	}
}
