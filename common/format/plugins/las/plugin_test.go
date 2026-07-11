package las

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/shared/lasfamily"
)

func TestLASDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatLAS {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatLAS)
	}
	if descriptor.DataType != datatype.PointCloud {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.PointCloud)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribePointCloudReadsLASHeader(t *testing.T) {
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{Reader: bytes.NewReader(buildLASHeader())}, nil)
	if err != nil {
		t.Fatalf("DescribePointCloud() error = %v", err)
	}
	if result == nil || result.PointCloud == nil {
		t.Fatalf("DescribePointCloud() = %#v, want point cloud info", result)
	}
	if result.PointCloud.PointCloudKind != datatype.PointCloudKindRawPointCloud {
		t.Fatalf("PointCloudKind = %q, want raw_point_cloud", result.PointCloud.PointCloudKind)
	}
	if result.PointCloud.PointCount == nil || *result.PointCloud.PointCount != 123456789 {
		t.Fatalf("PointCount = %v, want 123456789", result.PointCloud.PointCount)
	}
	if result.PointCloud.PointFormat != "las_1.4_point_format_7" {
		t.Fatalf("PointFormat = %q, want las_1.4_point_format_7", result.PointCloud.PointFormat)
	}
	if result.PointCloud.Bounds3D == nil || result.PointCloud.Bounds3D.MaxZ == nil || *result.PointCloud.Bounds3D.MaxZ != 30 {
		t.Fatalf("Bounds3D = %#v, want max_z 30", result.PointCloud.Bounds3D)
	}
	if result.Spatial == nil || result.Spatial.Extent == nil {
		t.Fatalf("Spatial = %#v, want extent", result.Spatial)
	}
	if result.FormatInfo["version"] != "1.4" {
		t.Fatalf("format_info.version = %#v, want 1.4", result.FormatInfo["version"])
	}
}

func buildLASHeader() []byte {
	buf := make([]byte, lasfamily.MaxHeaderRead)
	copy(buf[:4], []byte(lasfamily.Magic))
	buf[24] = 1
	buf[25] = 4
	copy(buf[26:58], []byte("ADDP"))
	copy(buf[58:90], []byte("unit-test"))
	binary.LittleEndian.PutUint16(buf[94:96], uint16(lasfamily.MaxHeaderRead))
	binary.LittleEndian.PutUint32(buf[96:100], 375)
	binary.LittleEndian.PutUint32(buf[100:104], 2)
	buf[104] = 7
	binary.LittleEndian.PutUint16(buf[105:107], 36)
	binary.LittleEndian.PutUint32(buf[107:111], 0)
	putFloat64(buf[131:139], 0.01)
	putFloat64(buf[139:147], 0.01)
	putFloat64(buf[147:155], 0.01)
	putFloat64(buf[155:163], 1000)
	putFloat64(buf[163:171], 2000)
	putFloat64(buf[171:179], 3000)
	putFloat64(buf[179:187], 10)
	putFloat64(buf[187:195], 1)
	putFloat64(buf[195:203], 20)
	putFloat64(buf[203:211], 2)
	putFloat64(buf[211:219], 30)
	putFloat64(buf[219:227], 3)
	binary.LittleEndian.PutUint64(buf[235:243], 4096)
	binary.LittleEndian.PutUint32(buf[243:247], 1)
	binary.LittleEndian.PutUint64(buf[247:255], 123456789)
	return buf
}

func putFloat64(target []byte, value float64) {
	binary.LittleEndian.PutUint64(target, math.Float64bits(value))
}
