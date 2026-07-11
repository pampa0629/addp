package laz

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

func TestLAZDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatLAZ {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatLAZ)
	}
	if descriptor.DataType != datatype.PointCloud {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.PointCloud)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribePointCloudReadsLAZHeader(t *testing.T) {
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{Reader: bytes.NewReader(buildLAZHeader())}, nil)
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
	if result.FormatInfo["compression"] != "laszip" {
		t.Fatalf("format_info.compression = %#v, want laszip", result.FormatInfo["compression"])
	}
}

func TestDescribePointCloudDoesNotReadLAS14EVLRFieldsFromLAS12Header(t *testing.T) {
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{Reader: bytes.NewReader(buildLAZ12HeaderWithPayloadBytes())}, nil)
	if err != nil {
		t.Fatalf("DescribePointCloud() error = %v", err)
	}
	if _, ok := result.FormatInfo["evlr_offset"]; ok {
		t.Fatalf("format_info.evlr_offset = %#v, want omitted for LAS 1.2 header", result.FormatInfo["evlr_offset"])
	}
	if _, ok := result.FormatInfo["evlr_count"]; ok {
		t.Fatalf("format_info.evlr_count = %#v, want omitted for LAS 1.2 header", result.FormatInfo["evlr_count"])
	}
}

func buildLAZ12HeaderWithPayloadBytes() []byte {
	buf := buildLAZHeader()
	buf[25] = 2
	binary.LittleEndian.PutUint16(buf[94:96], 227)
	binary.LittleEndian.PutUint32(buf[107:111], 100000)
	copy(buf[235:255], []byte("projection-payload!!"))
	return buf
}

func buildLAZHeader() []byte {
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
	binary.LittleEndian.PutUint64(buf[247:255], 123456789)
	return buf
}

func putFloat64(target []byte, value float64) {
	binary.LittleEndian.PutUint64(target, math.Float64bits(value))
}
