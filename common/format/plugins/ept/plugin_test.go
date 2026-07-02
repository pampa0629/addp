package ept

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestEPTDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatEPT {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatEPT)
	}
	if descriptor.DataType != datatype.PointCloud {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.PointCloud)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutWhole) {
		t.Fatalf("Layouts = %#v, want whole", descriptor.Layouts)
	}
	if len(descriptor.Identification.FileNames) != 1 || descriptor.Identification.FileNames[0] != ManifestFileName {
		t.Fatalf("FileNames = %#v, want ept.json", descriptor.Identification.FileNames)
	}
}

func TestDescribePointCloudScopeReadsManifest(t *testing.T) {
	manifest := `{
  "version": "1.1.0",
  "dataType": "laszip",
  "points": 12345,
  "bounds": [0, 1, 2, 10, 20, 30],
  "boundsConforming": [1, 2, 3, 9, 19, 29],
  "schema": [
    {"name": "X", "type": "signed", "size": 4, "scale": 0.01, "offset": 100},
    {"name": "Y", "type": "signed", "size": 4, "scale": 0.01, "offset": 200},
    {"name": "Z", "type": "signed", "size": 4, "scale": 0.01, "offset": 300},
    {"name": "Intensity", "type": "unsigned", "size": 2},
    {"name": "Classification", "type": "unsigned", "size": 1},
    {"name": "Red", "type": "unsigned", "size": 2},
    {"name": "Green", "type": "unsigned", "size": 2},
    {"name": "Blue", "type": "unsigned", "size": 2}
  ],
  "span": 128,
  "hierarchyType": "json",
  "srs": {"authority": "EPSG", "horizontal": "4978"}
}`
	result, err := NewPlugin().DescribePointCloudScope(context.Background(), memoryReader{
		data: map[string]string{"pointcloud/ept/ept.json": manifest},
	}, contentio.NewRef("pointcloud/ept", contentio.RoleScope), nil)
	if err != nil {
		t.Fatalf("DescribePointCloudScope() error = %v", err)
	}
	if result == nil || result.PointCloud == nil {
		t.Fatalf("DescribePointCloudScope() = %#v, want point cloud info", result)
	}
	if result.PointCloud.PointCloudKind != datatype.PointCloudKindTiledPointCloud {
		t.Fatalf("PointCloudKind = %q, want tiled_point_cloud", result.PointCloud.PointCloudKind)
	}
	if result.PointCloud.PointCount == nil || *result.PointCloud.PointCount != 12345 {
		t.Fatalf("PointCount = %#v, want 12345", result.PointCloud.PointCount)
	}
	if result.PointCloud.DimensionCount == nil || *result.PointCloud.DimensionCount != 8 {
		t.Fatalf("DimensionCount = %#v, want 8", result.PointCloud.DimensionCount)
	}
	if result.PointCloud.HasColor == nil || !*result.PointCloud.HasColor ||
		result.PointCloud.HasIntensity == nil || !*result.PointCloud.HasIntensity ||
		result.PointCloud.HasClassification == nil || !*result.PointCloud.HasClassification {
		t.Fatalf("point cloud capabilities = color:%#v intensity:%#v classification:%#v, want true", result.PointCloud.HasColor, result.PointCloud.HasIntensity, result.PointCloud.HasClassification)
	}
	if result.PointCloud.Bounds3D == nil || result.PointCloud.Bounds3D.MinX == nil || *result.PointCloud.Bounds3D.MinX != 1 {
		t.Fatalf("Bounds3D = %#v, want conforming min_x 1", result.PointCloud.Bounds3D)
	}
	if result.Spatial == nil || result.Spatial.SRID == nil || *result.Spatial.SRID != 4978 {
		t.Fatalf("Spatial = %#v, want EPSG:4978", result.Spatial)
	}
	if result.FormatInfo["manifest_ref"] != ManifestFileName || result.FormatInfo["hierarchy_type"] != "json" || result.FormatInfo["span"] != int64(128) {
		t.Fatalf("FormatInfo = %#v, want EPT manifest facts", result.FormatInfo)
	}
}

type memoryReader struct {
	data map[string]string
}

func (r memoryReader) Open(_ context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	value, ok := r.data[ref.Path]
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	return io.NopCloser(strings.NewReader(value)), nil
}

func (r memoryReader) Stat(_ context.Context, ref contentio.Ref) (*contentio.Stat, error) {
	_, ok := r.data[ref.Path]
	return &contentio.Stat{Ref: ref, Exists: ok}, nil
}
