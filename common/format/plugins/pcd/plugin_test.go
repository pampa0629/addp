package pcd

import (
	"bytes"
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestPCDDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatPCD {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatPCD)
	}
	if descriptor.DataType != datatype.PointCloud {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.PointCloud)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribePointCloudReadsASCIIHeader(t *testing.T) {
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{Reader: bytes.NewReader([]byte(`# .PCD v.5 - Point Cloud Data file format
VERSION .5
FIELDS x y z
SIZE 4 4 4
TYPE F F F
COUNT 1 1 1
WIDTH 397
HEIGHT 1
POINTS 397
DATA ascii
0.1 0.2 0.3
`))}, nil)
	if err != nil {
		t.Fatalf("DescribePointCloud() error = %v", err)
	}
	if result.PointCloud.PointCloudKind != datatype.PointCloudKindRawPointCloud {
		t.Fatalf("PointCloudKind = %q, want raw_point_cloud", result.PointCloud.PointCloudKind)
	}
	if result.PointCloud.PointCount == nil || *result.PointCloud.PointCount != 397 {
		t.Fatalf("PointCount = %v, want 397", result.PointCloud.PointCount)
	}
	if result.PointCloud.PointFormat != "pcd_ascii" {
		t.Fatalf("PointFormat = %q, want pcd_ascii", result.PointCloud.PointFormat)
	}
	if result.FormatInfo["version"] != ".5" || result.FormatInfo["data"] != "ascii" {
		t.Fatalf("format_info = %#v, want version .5 data ascii", result.FormatInfo)
	}
}

func TestDescribePointCloudReadsBinaryHeader(t *testing.T) {
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{Reader: bytes.NewReader([]byte(`# .PCD v0.7 - Point Cloud Data file format
VERSION 0.7
FIELDS x y z rgb normal_x normal_y normal_z curvature
SIZE 4 4 4 4 4 4 4 4
TYPE F F F U F F F F
COUNT 1 1 1 1 1 1 1 1
WIDTH 1
HEIGHT 1000
VIEWPOINT 0 0 0 1 0 0 0
POINTS 1000
DATA binary
`))}, nil)
	if err != nil {
		t.Fatalf("DescribePointCloud() error = %v", err)
	}
	if result.PointCloud.PointCount == nil || *result.PointCloud.PointCount != 1000 {
		t.Fatalf("PointCount = %v, want 1000", result.PointCloud.PointCount)
	}
	if result.PointCloud.HasColor == nil || !*result.PointCloud.HasColor {
		t.Fatalf("HasColor = %v, want true", result.PointCloud.HasColor)
	}
	if result.PointCloud.DimensionCount == nil || *result.PointCloud.DimensionCount != 8 {
		t.Fatalf("DimensionCount = %v, want 8", result.PointCloud.DimensionCount)
	}
	if result.FormatInfo["data"] != "binary" {
		t.Fatalf("format_info.data = %#v, want binary", result.FormatInfo["data"])
	}
}
