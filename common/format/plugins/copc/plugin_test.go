package copc

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/shared/lasfamily"
)

func TestCOPCDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatCOPC {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatCOPC)
	}
	if descriptor.DataType != datatype.PointCloud {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.PointCloud)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribePointCloudReadsCOPCHeader(t *testing.T) {
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{Reader: bytes.NewReader(buildCOPCHeader())}, nil)
	if err != nil {
		t.Fatalf("DescribePointCloud() error = %v", err)
	}
	if result == nil || result.PointCloud == nil {
		t.Fatalf("DescribePointCloud() = %#v, want point cloud info", result)
	}
	if result.PointCloud.PointCloudKind != datatype.PointCloudKindTiledPointCloud {
		t.Fatalf("PointCloudKind = %q, want tiled_point_cloud", result.PointCloud.PointCloudKind)
	}
	if result.PointCloud.PointCount == nil || *result.PointCloud.PointCount != 123456789 {
		t.Fatalf("PointCount = %v, want 123456789", result.PointCloud.PointCount)
	}
	if result.FormatInfo["profile"] != "copc" {
		t.Fatalf("format_info.profile = %#v, want copc", result.FormatInfo["profile"])
	}
	if result.FormatInfo["copc_version"] != "1.0" || result.FormatInfo["root_hierarchy_offset"] != uint64(copcInfoReadSize) || result.FormatInfo["root_hierarchy_size"] != uint64(copcHierarchyEntrySize*2) {
		t.Fatalf("format_info = %#v, want COPC info VLR facts", result.FormatInfo)
	}
	center, ok := result.FormatInfo["octree_center"].([]float64)
	if !ok || len(center) != 3 || center[0] != 100 || center[1] != 200 || center[2] != 300 {
		t.Fatalf("octree_center = %#v, want [100 200 300]", result.FormatInfo["octree_center"])
	}
}

func TestDescribePointCloudReadsRootHierarchyWhenRangeReaderIsAvailable(t *testing.T) {
	header := buildCOPCHeader()
	hierarchy := buildCOPCRootHierarchy()
	reader := &rangeReader{data: append(append([]byte(nil), header...), hierarchy...)}
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{
		Reader:      bytes.NewReader(header),
		RangeReader: reader,
		Ref:         contentio.NewRef("site.copc.laz", contentio.RoleMain),
		SizeBytes:   int64(len(reader.data)),
	}, nil)
	if err != nil {
		t.Fatalf("DescribePointCloud() error = %v", err)
	}
	if result.FormatInfo["root_hierarchy_entry_count"] != 2 ||
		result.FormatInfo["root_hierarchy_leaf_entry_count"] != 1 ||
		result.FormatInfo["root_hierarchy_internal_entry_count"] != 1 ||
		result.FormatInfo["root_hierarchy_point_count"] != int64(250) ||
		result.FormatInfo["root_hierarchy_byte_size"] != int64(4096) ||
		result.FormatInfo["hierarchy_entry_count"] != 2 ||
		result.FormatInfo["hierarchy_read_complete"] != true {
		t.Fatalf("format_info = %#v, want root hierarchy summary", result.FormatInfo)
	}
	if reader.offset != int64(copcInfoReadSize) || reader.length != int64(len(hierarchy)) {
		t.Fatalf("range = %d/%d, want %d/%d", reader.offset, reader.length, copcInfoReadSize, len(hierarchy))
	}
}

func TestDescribePointCloudRecursivelyReadsChildHierarchyPages(t *testing.T) {
	header := buildCOPCHeader()
	root := buildCOPCRootHierarchyWithChildPage()
	child := buildCOPCChildHierarchy()
	reader := &rangeReader{data: append(append(append([]byte(nil), header...), root...), child...)}
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{
		Reader:      bytes.NewReader(header),
		RangeReader: reader,
		Ref:         contentio.NewRef("site.copc.laz", contentio.RoleMain),
		SizeBytes:   int64(len(reader.data)),
	}, nil)
	if err != nil {
		t.Fatalf("DescribePointCloud() error = %v", err)
	}
	if result.FormatInfo["root_hierarchy_entry_count"] != 2 ||
		result.FormatInfo["root_hierarchy_page_entry_count"] != 1 ||
		result.FormatInfo["hierarchy_entry_count"] != 3 ||
		result.FormatInfo["hierarchy_leaf_entry_count"] != 1 ||
		result.FormatInfo["hierarchy_page_entry_count"] != 1 ||
		result.FormatInfo["hierarchy_point_count"] != int64(500) ||
		result.FormatInfo["hierarchy_byte_size"] != int64(2048) ||
		result.FormatInfo["hierarchy_page_read_count"] != 2 ||
		result.FormatInfo["hierarchy_read_byte_size"] != int64(96) ||
		result.FormatInfo["hierarchy_read_complete"] != true {
		t.Fatalf("format_info = %#v, want recursive hierarchy summary", result.FormatInfo)
	}
}

func TestDescribePointCloudRejectsHeaderWithoutCOPCInfoVLR(t *testing.T) {
	buf := buildCOPCHeader()
	copy(buf[copcInfoVLROffset+2:copcInfoVLROffset+18], []byte("not-copc"))
	if _, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{Reader: bytes.NewReader(buf)}, nil); err == nil {
		t.Fatal("DescribePointCloud() error = nil, want invalid COPC info VLR error")
	}
}

func buildCOPCHeader() []byte {
	buf := make([]byte, copcInfoReadSize)
	copy(buf[:4], []byte(lasfamily.Magic))
	buf[24] = 1
	buf[25] = 4
	copy(buf[26:58], []byte("ADDP"))
	copy(buf[58:90], []byte("unit-test"))
	binary.LittleEndian.PutUint16(buf[94:96], uint16(lasfamily.MaxHeaderRead))
	binary.LittleEndian.PutUint32(buf[96:100], uint32(copcInfoReadSize))
	binary.LittleEndian.PutUint32(buf[100:104], 1)
	buf[104] = 8
	binary.LittleEndian.PutUint16(buf[105:107], 38)
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
	vlrHeader := buf[copcInfoVLROffset : copcInfoVLROffset+copcVLRHeaderSize]
	copy(vlrHeader[2:18], []byte("copc"))
	binary.LittleEndian.PutUint16(vlrHeader[18:20], copcInfoVLRRecordID)
	binary.LittleEndian.PutUint16(vlrHeader[20:22], copcInfoVLRDataSize)
	copy(vlrHeader[22:54], []byte("COPC info"))
	infoData := buf[copcInfoVLROffset+copcVLRHeaderSize : copcInfoReadSize]
	putFloat64(infoData[0:8], 100)
	putFloat64(infoData[8:16], 200)
	putFloat64(infoData[16:24], 300)
	putFloat64(infoData[24:32], 500)
	putFloat64(infoData[32:40], 0.25)
	binary.LittleEndian.PutUint64(infoData[40:48], uint64(copcInfoReadSize))
	binary.LittleEndian.PutUint64(infoData[48:56], uint64(copcHierarchyEntrySize*2))
	putFloat64(infoData[56:64], 10)
	putFloat64(infoData[64:72], 20)
	return buf
}

func buildCOPCRootHierarchy() []byte {
	buf := make([]byte, copcHierarchyEntrySize*2)
	binary.LittleEndian.PutUint32(buf[0:4], 0)
	binary.LittleEndian.PutUint32(buf[28:32], 0)
	binary.LittleEndian.PutUint32(buf[32:36], 1)
	binary.LittleEndian.PutUint32(buf[56:60], 4096)
	binary.LittleEndian.PutUint32(buf[60:64], 250)
	return buf
}

func buildCOPCRootHierarchyWithChildPage() []byte {
	buf := make([]byte, copcHierarchyEntrySize*2)
	binary.LittleEndian.PutUint32(buf[0:4], 0)
	binary.LittleEndian.PutUint32(buf[28:32], 0)
	binary.LittleEndian.PutUint32(buf[32:36], 1)
	binary.LittleEndian.PutUint64(buf[48:56], uint64(copcInfoReadSize+len(buf)))
	binary.LittleEndian.PutUint32(buf[56:60], copcHierarchyEntrySize)
	binary.LittleEndian.PutUint32(buf[60:64], ^uint32(0))
	return buf
}

func buildCOPCChildHierarchy() []byte {
	buf := make([]byte, copcHierarchyEntrySize)
	binary.LittleEndian.PutUint32(buf[0:4], 2)
	binary.LittleEndian.PutUint32(buf[24:28], 2048)
	binary.LittleEndian.PutUint32(buf[28:32], 500)
	return buf
}

type rangeReader struct {
	data   []byte
	offset int64
	length int64
}

func (r *rangeReader) Open(context.Context, contentio.Ref) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.data)), nil
}

func (r *rangeReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return &contentio.Stat{Exists: true}, nil
}

func (r *rangeReader) OpenRange(_ context.Context, _ contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	r.offset = offset
	r.length = length
	end := offset + length
	if offset < 0 || length < 0 || end > int64(len(r.data)) {
		return nil, contentio.ErrContentNotFound
	}
	return io.NopCloser(bytes.NewReader(r.data[offset:end])), nil
}

func putFloat64(target []byte, value float64) {
	binary.LittleEndian.PutUint64(target, math.Float64bits(value))
}
