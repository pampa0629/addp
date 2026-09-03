package metaenrich

import (
	"bytes"
	"context"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

func EnrichSingleCADItem(
	ctx context.Context,
	attrs models.JSONMap,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	item *metaitem.DetectedItem,
	physicalPath string,
	sizeBytes int64,
	catalogPathFor func(string) plugin.EngineCatalogPath,
) error {
	if attrs == nil || contentReader == nil || item == nil || item.Layout != format.LayoutSingle || physicalPath == "" {
		return nil
	}
	candidate := format.NormalizeFormat(item.Format)
	if item.DataType != datatype.CAD && !format.IsCADFormat(candidate) {
		return nil
	}
	if catalogPathFor == nil {
		catalogPathFor = plugin.FileItemPathForEngine(engineID)
	}
	peek, err := readSingleFilePeek(ctx, contentReader, connInfo, catalogPathFor(physicalPath))
	if err != nil {
		return err
	}
	detected := format.DetectFormat(physicalPath, peek)
	clearCADAttributes(attrs)
	if !format.IsCADFormat(detected) {
		item.Format = string(format.FormatUnknown)
		item.DataType = datatype.Unknown
		item.CAD = nil
		metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(item))
		return nil
	}

	item.Format = string(detected)
	item.DataType = datatype.CAD
	item.CAD = &datatype.CADInfo{DrawingKind: datatype.CADDrawingKind2D, SizeBytes: int64Ptr(sizeBytes)}
	metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(item))
	metaitem.ApplyCADInfo(attrs, item)
	if version := cadFormatVersion(detected, peek); version != "" {
		metaattr.MergeStandardAttributes(attrs, metaattr.FormatInfoAttributes(item.Format, map[string]interface{}{"format_version": version}))
	}
	return nil
}

func cadFormatVersion(formatType format.FormatType, peek []byte) string {
	switch formatType {
	case format.FormatDWG:
		if len(peek) >= 6 {
			return string(peek[:6])
		}
	case format.FormatDXF:
		normalized := bytes.ReplaceAll(bytes.TrimPrefix(peek, []byte{0xEF, 0xBB, 0xBF}), []byte("\r\n"), []byte("\n"))
		lines := bytes.Split(normalized, []byte("\n"))
		for index := 0; index+2 < len(lines); index++ {
			if strings.EqualFold(strings.TrimSpace(string(lines[index])), "$ACADVER") && strings.TrimSpace(string(lines[index+1])) == "1" {
				return strings.TrimSpace(string(lines[index+2]))
			}
		}
	}
	return ""
}

func clearCADAttributes(attrs models.JSONMap) {
	for parent, children := range map[string][]string{
		"type_info":   {"cad"},
		"format_info": {"dwg", "dxf"},
	} {
		section := metaattr.Section(attrs, parent)
		for _, child := range children {
			delete(section, child)
		}
		if len(section) == 0 {
			delete(attrs, parent)
		} else {
			attrs[parent] = section
		}
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
