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

type TableDescribeAttributesInput struct {
	FormatName         string
	Table              *datatype.TableInfo
	FormatInfo         map[string]interface{}
	Spatial            *datatype.SpatialInfo
	AccessIndex        *datatype.AccessIndex
	IncludeAccessIndex bool
	AccessIndexSource  map[string]interface{}
}

func TableDescribeAttributes(input TableDescribeAttributesInput) map[string]interface{} {
	attrs := map[string]interface{}{}
	if input.Table != nil {
		if tableAttrs := datatype.TableInfoPayload(input.Table); len(tableAttrs) > 0 {
			UpsertNested(attrs, "type_info", "table", tableAttrs)
		}
	}
	if len(input.FormatInfo) > 0 {
		MergeStandardAttributes(attrs, FormatInfoAttributes(input.FormatName, input.FormatInfo))
	}
	if spatialPayload := datatype.SpatialInfoPayload(input.Spatial); len(spatialPayload) > 0 {
		UpsertNested(attrs, "capabilities", "spatial", spatialPayload)
	}
	if input.IncludeAccessIndex && input.AccessIndex != nil {
		indexInfo := input.AccessIndex.Clone()
		if indexInfo.Source == nil && len(input.AccessIndexSource) > 0 {
			indexInfo.Source = cloneInterfaceMap(input.AccessIndexSource)
		}
		UpsertNested(attrs, "access_index", "table", commonJSON.MapFromStruct(indexInfo))
	}
	return attrs
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

	MergeStandardAttributes(attrs, TableDescribeAttributes(TableDescribeAttributesInput{
		Table:              input.Table,
		Spatial:            input.Spatial,
		AccessIndex:        input.AccessIndex,
		IncludeAccessIndex: input.IncludeAccessIndex,
		AccessIndexSource: map[string]interface{}{
			"size_bytes": input.TotalSize,
		},
	}))

	return attrs
}
