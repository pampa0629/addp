package ksplat

import (
	"bytes"
	"context"
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
	result, err := NewPlugin().DescribeGaussianSplat(context.Background(), format.GaussianSplatDescribeInput{
		Reader: bytes.NewReader([]byte{0, 1, 2, 3}),
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
	if result.FormatInfo["encoding"] != "ksplat" {
		t.Fatalf("format_info = %#v, want ksplat encoding", result.FormatInfo)
	}
}
