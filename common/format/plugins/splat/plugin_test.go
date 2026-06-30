package splat

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
)

func TestSplatDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatSplat {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatSplat)
	}
	if descriptor.DataType != datatype.GaussianSplat {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.GaussianSplat)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribeGaussianSplatReturnsStableSplatFacts(t *testing.T) {
	var content bytes.Buffer
	writeSplatTestRecord(&content, -1, 2, 10)
	writeSplatTestRecord(&content, 3, -4, 20)
	result, err := NewPlugin().DescribeGaussianSplat(context.Background(), format.GaussianSplatDescribeInput{
		Reader: bytes.NewReader(content.Bytes()),
	}, nil)
	if err != nil {
		t.Fatalf("DescribeGaussianSplat() error = %v", err)
	}
	if result == nil || result.GaussianSplat == nil {
		t.Fatalf("DescribeGaussianSplat() = %#v, want gaussian_splat info", result)
	}
	if result.GaussianSplat.Representation != datatype.GaussianSplatRepresentation3DGS {
		t.Fatalf("Representation = %q, want 3d_gaussian_splatting", result.GaussianSplat.Representation)
	}
	if result.GaussianSplat.SplatCount == nil || *result.GaussianSplat.SplatCount != 2 {
		t.Fatalf("SplatCount = %#v, want 2", result.GaussianSplat.SplatCount)
	}
	assertSplatBounds(t, result.GaussianSplat.Bounds3D, -1, -4, 10, 3, 2, 20)
	if result.FormatInfo["encoding"] != "splat" {
		t.Fatalf("format_info = %#v, want splat encoding", result.FormatInfo)
	}
	if result.FormatInfo["record_size"] != int64(splatRecordSize) {
		t.Fatalf("record_size = %#v, want %d", result.FormatInfo["record_size"], splatRecordSize)
	}
}

func TestDescribeGaussianSplatReportsScaleDiagnostics(t *testing.T) {
	var content bytes.Buffer
	writeSplatTestRecordWithScale(&content, 0, 0, 0, 0.001, 0.1, 0.001, 250)
	writeSplatTestRecordWithScale(&content, 1, 1, 1, 0.01, 0.01, 0.01, 10)

	result, err := NewPlugin().DescribeGaussianSplat(context.Background(), format.GaussianSplatDescribeInput{
		Reader: bytes.NewReader(content.Bytes()),
	}, nil)
	if err != nil {
		t.Fatalf("DescribeGaussianSplat() error = %v", err)
	}
	scaleStats, ok := result.FormatInfo["scale_stats"].(map[string]interface{})
	if !ok {
		t.Fatalf("scale_stats = %#v, want map", result.FormatInfo["scale_stats"])
	}
	if scaleStats["sample_count"] != int64(2) {
		t.Fatalf("sample_count = %#v, want 2", scaleStats["sample_count"])
	}
	if scaleStats["anisotropic_count"] != int64(1) {
		t.Fatalf("anisotropic_count = %#v, want 1", scaleStats["anisotropic_count"])
	}
	if scaleStats["low_alpha_count"] != int64(1) {
		t.Fatalf("low_alpha_count = %#v, want 1", scaleStats["low_alpha_count"])
	}
	diagnostic, ok := result.FormatInfo["render_diagnostic"].(map[string]interface{})
	if !ok {
		t.Fatalf("render_diagnostic = %#v, want map", result.FormatInfo["render_diagnostic"])
	}
	if diagnostic["recommended_render_mode"] != "2d" {
		t.Fatalf("recommended_render_mode = %#v, want 2d", diagnostic["recommended_render_mode"])
	}
}

func TestDescribeGaussianSplatSamplesLargeSplatBounds(t *testing.T) {
	var content bytes.Buffer
	writeSplatTestRecord(&content, -1, 2, 10)
	for i := 1; i < maxExactSplatBoundsRecords; i++ {
		writeSplatTestRecord(&content, 0, 0, 15)
	}
	writeSplatTestRecord(&content, 3, -4, 20)
	reader := splatMemoryRangeReader{content: content.Bytes()}

	result, err := NewPlugin().DescribeGaussianSplat(context.Background(), format.GaussianSplatDescribeInput{
		Reader:      bytes.NewReader(content.Bytes()),
		RangeReader: reader,
		Ref:         contentio.NewRef("site.splat", contentio.RoleMain),
		SizeBytes:   int64(content.Len()),
	}, nil)
	if err != nil {
		t.Fatalf("DescribeGaussianSplat() error = %v", err)
	}
	if result == nil || result.GaussianSplat == nil {
		t.Fatalf("DescribeGaussianSplat() = %#v, want gaussian_splat info", result)
	}
	assertSplatBounds(t, result.GaussianSplat.SampledBounds3D, -1, -4, 10, 3, 2, 20)
	if result.GaussianSplat.Bounds3D != nil {
		t.Fatalf("Bounds3D = %#v, want nil for sampled large splat", result.GaussianSplat.Bounds3D)
	}
	if result.GaussianSplat.SampledBoundsMethod != "sampled_splat_records" {
		t.Fatalf("SampledBoundsMethod = %q, want sampled_splat_records", result.GaussianSplat.SampledBoundsMethod)
	}
	if result.GaussianSplat.SampledBoundsSampleCount == nil || *result.GaussianSplat.SampledBoundsSampleCount != splatSampledBoundsSampleCount {
		t.Fatalf("SampledBoundsSampleCount = %v, want %d", result.GaussianSplat.SampledBoundsSampleCount, splatSampledBoundsSampleCount)
	}
}

func writeSplatTestRecord(buffer *bytes.Buffer, x, y, z float32) {
	writeSplatTestRecordWithScale(buffer, x, y, z, 0.01, 0.01, 0.01, 255)
}

func writeSplatTestRecordWithScale(buffer *bytes.Buffer, x, y, z, scaleX, scaleY, scaleZ float32, alpha byte) {
	record := make([]byte, splatRecordSize)
	binary.LittleEndian.PutUint32(record[0:4], math.Float32bits(x))
	binary.LittleEndian.PutUint32(record[4:8], math.Float32bits(y))
	binary.LittleEndian.PutUint32(record[8:12], math.Float32bits(z))
	binary.LittleEndian.PutUint32(record[12:16], math.Float32bits(scaleX))
	binary.LittleEndian.PutUint32(record[16:20], math.Float32bits(scaleY))
	binary.LittleEndian.PutUint32(record[20:24], math.Float32bits(scaleZ))
	record[27] = alpha
	buffer.Write(record)
}

type splatMemoryRangeReader struct {
	content []byte
}

func (r splatMemoryRangeReader) Open(context.Context, contentio.Ref) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.content)), nil
}

func (r splatMemoryRangeReader) Stat(_ context.Context, ref contentio.Ref) (*contentio.Stat, error) {
	return &contentio.Stat{Ref: ref, Size: int64(len(r.content)), Exists: true}, nil
}

func (r splatMemoryRangeReader) OpenRange(_ context.Context, _ contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 {
		offset = 0
	}
	if length < 0 {
		length = 0
	}
	start := minInt64(offset, int64(len(r.content)))
	end := minInt64(start+length, int64(len(r.content)))
	return io.NopCloser(bytes.NewReader(r.content[start:end])), nil
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func assertSplatBounds(t *testing.T, bounds *datatype.Bounds3D, minX, minY, minZ, maxX, maxY, maxZ float64) {
	t.Helper()
	if bounds == nil || bounds.MinX == nil || bounds.MinY == nil || bounds.MinZ == nil ||
		bounds.MaxX == nil || bounds.MaxY == nil || bounds.MaxZ == nil {
		t.Fatalf("Bounds3D = %#v, want complete bounds", bounds)
	}
	if *bounds.MinX != minX || *bounds.MinY != minY || *bounds.MinZ != minZ ||
		*bounds.MaxX != maxX || *bounds.MaxY != maxY || *bounds.MaxZ != maxZ {
		t.Fatalf("Bounds3D = %#v, want [%v %v %v]-[%v %v %v]", bounds, minX, minY, minZ, maxX, maxY, maxZ)
	}
}
