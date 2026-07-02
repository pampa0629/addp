package ksplat

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestKSplatDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatKSplat {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatKSplat)
	}
	if descriptor.DataType != datatype.GaussianSplat {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.GaussianSplat)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribeGaussianSplatReturnsStableKSplatFacts(t *testing.T) {
	header := make([]byte, ksplatHeaderSizeBytes)
	header[0] = 0
	header[1] = 1
	binary.LittleEndian.PutUint32(header[4:8], 15)
	binary.LittleEndian.PutUint32(header[8:12], 12)
	binary.LittleEndian.PutUint32(header[12:16], 4096)
	binary.LittleEndian.PutUint32(header[16:20], 2048)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint32(header[24:28], math.Float32bits(-53.5))
	binary.LittleEndian.PutUint32(header[28:32], math.Float32bits(1.25))
	binary.LittleEndian.PutUint32(header[32:36], math.Float32bits(1522.75))

	result, err := NewPlugin().DescribeGaussianSplat(context.Background(), format.GaussianSplatDescribeInput{
		Reader: bytes.NewReader(header),
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
	if result.GaussianSplat.SplatCount == nil || *result.GaussianSplat.SplatCount != 2048 {
		t.Fatalf("SplatCount = %#v, want 2048", result.GaussianSplat.SplatCount)
	}
	if result.FormatInfo["encoding"] != "ksplat" {
		t.Fatalf("format_info = %#v, want ksplat encoding", result.FormatInfo)
	}
	if result.FormatInfo["section_count"] != int64(12) || result.FormatInfo["compression_level"] != 1 {
		t.Fatalf("format_info = %#v, want section and compression facts", result.FormatInfo)
	}
	center, ok := result.FormatInfo["scene_center"].([]float64)
	if !ok || len(center) != 3 || center[0] != -53.5 || center[1] != 1.25 || center[2] != 1522.75 {
		t.Fatalf("scene_center = %#v, want parsed KSplat scene center", result.FormatInfo["scene_center"])
	}
}
