package xyz

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestXYZDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatXYZ {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatXYZ)
	}
	if descriptor.DataType != datatype.PointCloud {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.PointCloud)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribePointCloudReadsXYZText(t *testing.T) {
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{Reader: bytes.NewReader([]byte(`# comment
0.0 1.0 2.0
3.0 4.0 5.0
`))}, nil)
	if err != nil {
		t.Fatalf("DescribePointCloud() error = %v", err)
	}
	if result.PointCloud.PointCloudKind != datatype.PointCloudKindRawPointCloud {
		t.Fatalf("PointCloudKind = %q, want raw_point_cloud", result.PointCloud.PointCloudKind)
	}
	if result.PointCloud.PointCount == nil || *result.PointCloud.PointCount != 2 {
		t.Fatalf("PointCount = %v, want 2", result.PointCloud.PointCount)
	}
	if result.PointCloud.Bounds3D == nil || result.PointCloud.Bounds3D.MaxZ == nil || *result.PointCloud.Bounds3D.MaxZ != 5 {
		t.Fatalf("Bounds3D = %#v, want max_z 5", result.PointCloud.Bounds3D)
	}
	if result.Spatial == nil || result.Spatial.Extent == nil {
		t.Fatalf("Spatial = %#v, want extent", result.Spatial)
	}
	if result.FormatInfo["delimiter"] != "whitespace" || result.FormatInfo["scan_complete"] != true {
		t.Fatalf("format_info = %#v, want whitespace complete", result.FormatInfo)
	}
}

func TestDescribePointCloudReadsCommaXYZText(t *testing.T) {
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{Reader: bytes.NewReader([]byte("0,1,2\n3,4,5\n"))}, nil)
	if err != nil {
		t.Fatalf("DescribePointCloud() error = %v", err)
	}
	if result.FormatInfo["delimiter"] != "comma" {
		t.Fatalf("delimiter = %#v, want comma", result.FormatInfo["delimiter"])
	}
}

func TestDescribePointCloudBudgetDoesNotWriteExactCount(t *testing.T) {
	var builder strings.Builder
	for i := 0; i < maxScanLines+2; i++ {
		builder.WriteString("0 1 2\n")
	}
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{Reader: strings.NewReader(builder.String())}, nil)
	if err != nil {
		t.Fatalf("DescribePointCloud() error = %v", err)
	}
	if result.PointCloud.PointCount != nil {
		t.Fatalf("PointCount = %v, want omitted when scan is partial", result.PointCloud.PointCount)
	}
	if result.PointCloud.Bounds3D != nil {
		t.Fatalf("Bounds3D = %#v, want omitted when scan is partial", result.PointCloud.Bounds3D)
	}
	if result.FormatInfo["scan_complete"] != false {
		t.Fatalf("scan_complete = %#v, want false", result.FormatInfo["scan_complete"])
	}
}
