package tiles3d

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func Test3DTilesDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.Format3DTiles {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.Format3DTiles)
	}
	if descriptor.DataType != datatype.Model3D {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.Model3D)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutWhole) {
		t.Fatalf("Layouts = %#v, want whole", descriptor.Layouts)
	}
}

func TestDescribeModel3DReadsTilesetManifest(t *testing.T) {
	result, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewReader([]byte(testTilesetJSON())), nil)
	if err != nil {
		t.Fatalf("DescribeModel3D() error = %v", err)
	}
	if result == nil || result.Model3D == nil {
		t.Fatalf("DescribeModel3D() = %#v, want model info", result)
	}
	if result.Model3D.ModelKind != datatype.Model3DKindTiledScene {
		t.Fatalf("ModelKind = %q, want tiled_scene", result.Model3D.ModelKind)
	}
	if result.Model3D.LODCount == nil || *result.Model3D.LODCount != 2 {
		t.Fatalf("LODCount = %v, want 2", result.Model3D.LODCount)
	}
	if result.Model3D.Bounds3D == nil || result.Model3D.Bounds3D.MinZ == nil || *result.Model3D.Bounds3D.MinZ != 0 {
		t.Fatalf("Bounds3D = %#v, want min_z 0", result.Model3D.Bounds3D)
	}
	if result.Spatial == nil || result.Spatial.Extent == nil {
		t.Fatalf("Spatial = %#v, want extent from region", result.Spatial)
	}
	if result.FormatInfo["asset_version"] != "1.1" {
		t.Fatalf("asset_version = %#v, want 1.1", result.FormatInfo["asset_version"])
	}
	if result.FormatInfo["tile_count"] != int64(2) || result.FormatInfo["content_count"] != int64(2) {
		t.Fatalf("format_info = %#v, want tile/content counts", result.FormatInfo)
	}
}

func TestDescribeModel3DScopeOpensTilesetJSON(t *testing.T) {
	reader := memoryTilesetReader{files: map[string]string{
		"city/tileset.json": testTilesetJSON(),
	}}
	result, err := NewPlugin().DescribeModel3DScope(context.Background(), reader, contentio.NewRef("city", contentio.RoleScope), nil)
	if err != nil {
		t.Fatalf("DescribeModel3DScope() error = %v", err)
	}
	if result == nil || result.Model3D == nil || result.Model3D.ModelKind != datatype.Model3DKindTiledScene {
		t.Fatalf("DescribeModel3DScope() = %#v, want tiled_scene", result)
	}
}

func TestDecodeTilesetRejectsPlainJSON(t *testing.T) {
	_, err := DecodeTileset(bytes.NewReader([]byte(`{"name":"tileset but not 3d tiles"}`)), 1024)
	if err == nil {
		t.Fatal("DecodeTileset() error = nil, want invalid manifest")
	}
}

type memoryTilesetReader struct {
	files map[string]string
}

func (r memoryTilesetReader) Open(_ context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	value, ok := r.files[ref.Path]
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	return io.NopCloser(bytes.NewReader([]byte(value))), nil
}

func (r memoryTilesetReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, contentio.ErrContentNotFound
}

func testTilesetJSON() string {
	return `{
		"asset": {"version": "1.1", "tilesetVersion": "2026.06"},
		"geometricError": 200,
		"root": {
			"refine": "ADD",
			"boundingVolume": {"region": [1, 0.5, 1.1, 0.6, 0, 120]},
			"geometricError": 100,
			"content": {"uri": "root.b3dm"},
			"children": [{
				"boundingVolume": {"box": [10, 20, 30, 1, 0, 0, 0, 2, 0, 0, 0, 3]},
				"geometricError": 0,
				"content": {"uri": "child.b3dm"}
			}]
		},
		"extensionsUsed": ["3DTILES_metadata"]
	}`
}
