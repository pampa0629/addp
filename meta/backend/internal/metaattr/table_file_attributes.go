package metaattr

import (
	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
)

type TableFileAttributesInput struct {
	FormatName         string
	Mode               string
	FileCount          int
	PhysicalPath       string
	TotalSize          int64
	Table              *datatype.TableInfo
	FormatInfo         map[string]interface{}
	Spatial            *datatype.SpatialInfo
	AccessIndex        *datatype.AccessIndex
	IncludeAccessIndex bool
}

func TableFileAttributes(input TableFileAttributesInput) map[string]interface{} {
	attrs := map[string]interface{}{}
	if input.PhysicalPath != "" || input.TotalSize > 0 {
		storage := map[string]interface{}{}
		if input.PhysicalPath != "" {
			storage["physical_path"] = input.PhysicalPath
		}
		if input.TotalSize > 0 {
			storage["total_size"] = input.TotalSize
		}
		UpsertSection(attrs, "storage", storage)
	}

	formatName := input.FormatName
	if formatName != "" {
		formatAttrs := map[string]interface{}{}
		if input.Mode != "" {
			formatAttrs["mode"] = input.Mode
		}
		formatAttrs["file_count"] = input.FileCount
		for key, value := range input.FormatInfo {
			formatAttrs[key] = value
		}
		UpsertNested(attrs, "format_info", formatName, formatAttrs)
	}

	if input.Table != nil {
		if tableAttrs := datatype.TableInfoAttributes(input.Table); len(tableAttrs) > 0 {
			if len(input.Table.Fields) == 0 {
				delete(tableAttrs, "fields")
			}
			if len(tableAttrs) > 0 {
				UpsertNested(attrs, "type_info", "table", tableAttrs)
			}
		}
	}

	if spatialAttrs := SpatialInfoAttributes(input.Spatial); len(spatialAttrs) > 0 {
		UpsertNested(attrs, "capabilities", "spatial", spatialAttrs)
	}

	if input.IncludeAccessIndex && input.AccessIndex != nil {
		indexInfo := input.AccessIndex.Clone()
		if indexInfo.Source == nil {
			indexInfo.Source = map[string]interface{}{
				"size_bytes": input.TotalSize,
			}
		}
		UpsertNested(attrs, "access_index", "table", commonJSON.MapFromStruct(indexInfo))
	}

	return attrs
}
