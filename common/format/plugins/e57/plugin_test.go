package e57

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestE57Descriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatE57 {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatE57)
	}
	if descriptor.DataType != datatype.PointCloud {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.PointCloud)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribePointCloudReadsE57XMLSummary(t *testing.T) {
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{Reader: bytes.NewReader(buildE57File())}, nil)
	if err != nil {
		t.Fatalf("DescribePointCloud() error = %v", err)
	}
	if result == nil || result.PointCloud == nil {
		t.Fatalf("DescribePointCloud() = %#v, want point cloud info", result)
	}
	if result.PointCloud.PointCloudKind != datatype.PointCloudKindScanCollection {
		t.Fatalf("PointCloudKind = %q, want scan_collection", result.PointCloud.PointCloudKind)
	}
	if result.PointCloud.PointCount == nil || *result.PointCloud.PointCount != 30571 {
		t.Fatalf("PointCount = %v, want 30571", result.PointCloud.PointCount)
	}
	if result.PointCloud.Bounds3D == nil || result.PointCloud.Bounds3D.MinX == nil || *result.PointCloud.Bounds3D.MinX != -0.094689 {
		t.Fatalf("Bounds3D = %#v, want min_x -0.094689", result.PointCloud.Bounds3D)
	}
	if result.Spatial == nil || result.Spatial.Extent == nil {
		t.Fatalf("Spatial = %#v, want extent", result.Spatial)
	}
	if result.FormatInfo["scan_count"] != 1 {
		t.Fatalf("format_info.scan_count = %#v, want 1", result.FormatInfo["scan_count"])
	}
	if names, ok := result.FormatInfo["scan_names"].([]string); !ok || len(names) != 1 || names[0] != "bunny" {
		t.Fatalf("format_info.scan_names = %#v, want bunny", result.FormatInfo["scan_names"])
	}
}

func TestDescribePointCloudReadsE57XMLSummaryWithRangeReader(t *testing.T) {
	file := buildE57FileWithXMLOffset(uint64(e57MaxXMLSeekBytes + 1024))
	result, err := NewPlugin().DescribePointCloud(context.Background(), format.PointCloudDescribeInput{
		Reader:      bytes.NewReader(file[:e57HeaderSize]),
		RangeReader: e57RangeReader{data: file},
		Ref:         contentio.NewRef("bunny.e57", contentio.RoleMain),
		SizeBytes:   int64(len(file)),
	}, nil)
	if err != nil {
		t.Fatalf("DescribePointCloud() error = %v", err)
	}
	if result == nil || result.PointCloud == nil || result.PointCloud.PointCount == nil || *result.PointCloud.PointCount != 30571 {
		t.Fatalf("PointCloud = %#v, want range-read E57 summary", result)
	}
	if result.FormatInfo["xml_read"] != true || result.FormatInfo["scan_count"] != 1 {
		t.Fatalf("format_info = %#v, want XML read through range reader", result.FormatInfo)
	}
}

func buildE57File() []byte {
	return buildE57FileWithXMLOffset(1024)
}

func buildE57FileWithXMLOffset(xmlOffset uint64) []byte {
	xmlPayload := strings.TrimSpace(`
<?xml version="1.0" encoding="UTF-8"?>
<e57Root type="Structure" xmlns="http://www.astm.org/COMMIT/E57/2010-e57-v1.0">
  <formatName type="String">ASTM E57 3D Imaging Data File</formatName>
  <guid type="String">{unit-test}</guid>
  <versionMajor type="Integer">1</versionMajor>
  <versionMinor type="Integer">0</versionMinor>
  <e57LibraryVersion type="String">ADDP test</e57LibraryVersion>
  <coordinateMetadata type="String"/>
  <data3D type="Vector" allowHeterogeneousChildren="1">
    <vectorChild type="Structure">
      <name type="String">bunny</name>
      <cartesianBounds type="Structure">
        <xMinimum type="Float">-0.094689</xMinimum>
        <xMaximum type="Float">0.061009</xMaximum>
        <yMinimum type="Float">0.040011</yMinimum>
        <yMaximum type="Float">0.187321</yMaximum>
        <zMinimum type="Float">-0.061873</zMinimum>
        <zMaximum type="Float">0.058799</zMaximum>
      </cartesianBounds>
      <points type="CompressedVector" fileOffset="48" recordCount="30571">
        <prototype type="Structure">
          <cartesianX type="Float"/>
          <cartesianY type="Float"/>
          <cartesianZ type="Float"/>
        </prototype>
      </points>
    </vectorChild>
  </data3D>
</e57Root>`)
	xmlBytes := []byte(xmlPayload)
	pageSize := uint64(1024)
	file := make([]byte, int(xmlOffset))
	copy(file[:8], []byte(e57Magic))
	binary.LittleEndian.PutUint32(file[8:12], 1)
	binary.LittleEndian.PutUint32(file[12:16], 0)
	binary.LittleEndian.PutUint64(file[24:32], xmlOffset)
	binary.LittleEndian.PutUint64(file[32:40], uint64(len(xmlBytes)))
	binary.LittleEndian.PutUint64(file[40:48], pageSize)
	pagedXML := pageE57Payload(xmlBytes, int(pageSize))
	file = append(file, pagedXML...)
	binary.LittleEndian.PutUint64(file[16:24], uint64(len(file)))
	return file
}

func pageE57Payload(payload []byte, pageSize int) []byte {
	payloadSize := pageSize - e57PageChecksumBytes
	var output []byte
	for len(payload) > 0 {
		chunkSize := payloadSize
		if len(payload) < chunkSize {
			chunkSize = len(payload)
		}
		output = append(output, payload[:chunkSize]...)
		payload = payload[chunkSize:]
		output = append(output, 0, 0, 0, 0)
	}
	return output
}

type e57RangeReader struct {
	data []byte
}

func (r e57RangeReader) Open(context.Context, contentio.Ref) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.data)), nil
}

func (r e57RangeReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return &contentio.Stat{Exists: true, Size: int64(len(r.data))}, nil
}

func (r e57RangeReader) OpenRange(_ context.Context, _ contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	end := offset + length
	if offset < 0 || length < 0 || end > int64(len(r.data)) {
		return nil, contentio.ErrContentNotFound
	}
	return io.NopCloser(bytes.NewReader(r.data[offset:end])), nil
}
