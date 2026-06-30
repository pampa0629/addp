package format

import "github.com/addp/common/datatype"

// TableDescribeResult groups facts that may be produced by one table format parse.
//
// The fields map to separate attributes partitions: Table to type_info.table,
// Spatial to capabilities.spatial, AccessIndex to access_index.table and
// FormatInfo to the current format's format_info.<format> namespace.
type TableDescribeResult struct {
	Table       *datatype.TableInfo    `json:"table,omitempty"`
	Spatial     *datatype.SpatialInfo  `json:"spatial,omitempty"`
	AccessIndex *datatype.AccessIndex  `json:"access_index,omitempty"`
	FormatInfo  map[string]interface{} `json:"format_info,omitempty"`
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

// Model3DDescribeResult groups facts that may be produced by one 3D model
// format parse.
//
// Model3D maps to type_info.model_3d, Spatial to capabilities.spatial and
// FormatInfo to format_info.<format>.
type Model3DDescribeResult struct {
	Model3D    *datatype.Model3DInfo  `json:"model_3d,omitempty"`
	Spatial    *datatype.SpatialInfo  `json:"spatial,omitempty"`
	FormatInfo map[string]interface{} `json:"format_info,omitempty"`
}

// PointCloudDescribeResult groups facts that may be produced by one point
// cloud format parse.
//
// PointCloud maps to type_info.point_cloud, Spatial to capabilities.spatial
// and FormatInfo to format_info.<format>.
type PointCloudDescribeResult struct {
	PointCloud *datatype.PointCloudInfo `json:"point_cloud,omitempty"`
	Spatial    *datatype.SpatialInfo    `json:"spatial,omitempty"`
	FormatInfo map[string]interface{}   `json:"format_info,omitempty"`
}

// GaussianSplatDescribeResult groups facts that may be produced by one
// Gaussian splatting format parse.
//
// GaussianSplat maps to type_info.gaussian_splat, Spatial to
// capabilities.spatial and FormatInfo to format_info.<format>.
type GaussianSplatDescribeResult struct {
	GaussianSplat *datatype.GaussianSplatInfo `json:"gaussian_splat,omitempty"`
	Spatial       *datatype.SpatialInfo       `json:"spatial,omitempty"`
	FormatInfo    map[string]interface{}      `json:"format_info,omitempty"`
}
