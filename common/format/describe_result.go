package format

import "github.com/addp/common/datatype"

// TableDescribeResult groups facts that may be produced by one table format parse.
//
// The fields map to separate attributes partitions: Table to type_info.table,
// Spatial to capabilities.spatial, ContentIndex to content_index.table and
// FormatInfo to the current format's format_info.<format> namespace.
type TableDescribeResult struct {
	Table        *datatype.TableInfo    `json:"table,omitempty"`
	Spatial      *datatype.SpatialInfo  `json:"spatial,omitempty"`
	ContentIndex *datatype.ContentIndex `json:"content_index,omitempty"`
	FormatInfo   map[string]interface{} `json:"format_info,omitempty"`
}

// MediaDescribeResult groups facts that may be produced by one media format parse.
//
// Media maps to type_info.media, Spatial to capabilities.spatial and
// FormatInfo to format_info.<format>.
type MediaDescribeResult struct {
	Media      *datatype.MediaInfo    `json:"media,omitempty"`
	Spatial    *datatype.SpatialInfo  `json:"spatial,omitempty"`
	FormatInfo map[string]interface{} `json:"format_info,omitempty"`
}
