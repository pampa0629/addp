package osgbscene

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestOSGBSceneDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatOSGBScene {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatOSGBScene)
	}
	if descriptor.DataType != datatype.Model3D {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.Model3D)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutWhole) {
		t.Fatalf("Layouts = %#v, want whole", descriptor.Layouts)
	}
	if len(descriptor.Identification.Extensions) != 0 {
		t.Fatalf("Extensions = %#v, want no single file extension", descriptor.Identification.Extensions)
	}
	if len(descriptor.Identification.FileNames) != 1 || descriptor.Identification.FileNames[0] != MetadataFileName {
		t.Fatalf("FileNames = %#v, want metadata.xml", descriptor.Identification.FileNames)
	}
	if len(descriptor.Identification.MimeTypes) != 0 {
		t.Fatalf("MimeTypes = %#v, OSGB scene must not claim generic XML MIME types", descriptor.Identification.MimeTypes)
	}
}

func TestDescribeModel3DReadsOSGBSceneMetadata(t *testing.T) {
	result, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewReader([]byte(testMetadataXML())), nil)
	if err != nil {
		t.Fatalf("DescribeModel3D() error = %v", err)
	}
	if result == nil || result.Model3D == nil {
		t.Fatalf("DescribeModel3D() = %#v, want model info", result)
	}
	if result.Model3D.ModelKind != datatype.Model3DKindPhotogrammetryScene {
		t.Fatalf("ModelKind = %q, want photogrammetry_scene", result.Model3D.ModelKind)
	}
	if result.Spatial == nil || result.Spatial.SRID == nil || *result.Spatial.SRID != 4549 {
		t.Fatalf("Spatial = %#v, want EPSG:4549", result.Spatial)
	}
	if result.FormatInfo["manifest_ref"] != MetadataFileName || result.FormatInfo["color_source"] != "Visible" {
		t.Fatalf("FormatInfo = %#v, want manifest and color source", result.FormatInfo)
	}
	origin, ok := result.FormatInfo["srs_origin"].([]float64)
	if !ok || len(origin) != 3 || origin[0] != 381180 || origin[1] != 4897399 {
		t.Fatalf("srs_origin = %#v, want parsed origin", result.FormatInfo["srs_origin"])
	}
}

func TestDescribeModel3DScopeOpensMetadataXML(t *testing.T) {
	reader := memoryOSGBSceneReader{files: map[string]string{
		"scene/metadata.xml": testMetadataXML(),
	}}
	result, err := NewPlugin().DescribeModel3DScope(context.Background(), reader, contentio.NewRef("scene", contentio.RoleScope), nil)
	if err != nil {
		t.Fatalf("DescribeModel3DScope() error = %v", err)
	}
	if result == nil || result.Model3D == nil || result.Model3D.ModelKind != datatype.Model3DKindPhotogrammetryScene {
		t.Fatalf("DescribeModel3DScope() = %#v, want photogrammetry_scene", result)
	}
}

func TestDecodeMetadataRejectsPlainXML(t *testing.T) {
	_, err := DecodeMetadata(bytes.NewReader([]byte(`<root><SRS>EPSG:4549</SRS></root>`)), 1024)
	if err == nil {
		t.Fatal("DecodeMetadata() error = nil, want invalid metadata")
	}
}

type memoryOSGBSceneReader struct {
	files map[string]string
}

func (r memoryOSGBSceneReader) Open(_ context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	value, ok := r.files[ref.Path]
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	return io.NopCloser(bytes.NewReader([]byte(value))), nil
}

func (r memoryOSGBSceneReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, contentio.ErrContentNotFound
}

func testMetadataXML() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<ModelMetadata version="1">
	<SRS>EPSG:4549</SRS>
	<SRSOrigin>381180,4897399,0</SRSOrigin>
	<Texture>
		<ColorSource>Visible</ColorSource>
	</Texture>
</ModelMetadata>`
}
