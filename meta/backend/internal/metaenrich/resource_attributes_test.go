package metaenrich

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	stdimage "image"
	"image/color"
	"image/png"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"golang.org/x/image/tiff"
)

func TestEnrichResourceAttributesKeepsContainerSummaryWhenContentOpenFails(t *testing.T) {
	t.Parallel()

	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Container,
			Format:             string(format.FormatZIP),
			PrimaryContentPath: "broken.zip",
		},
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	_, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: failingContentReader{},
		Item:          item,
		PhysicalPath:  "broken.zip",
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}

	container := commonJSON.Section(attrs, "type_info.container")
	if container["child_count"] != 0 || container["resource_count"] != 1 {
		t.Fatalf("type_info.container = %#v, want summary", container)
	}
}

func TestEnrichResourceAttributesKeepsKnownFieldsWithoutContentReader(t *testing.T) {
	t.Parallel()

	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:   format.LayoutSingle,
			DataType: datatype.Table,
			Format:   string(format.FormatCSV),
		},
		Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}},
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	_, fields, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{Item: item})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if len(fields) != 1 {
		t.Fatalf("fields len = %d, want 1", len(fields))
	}
	table := commonJSON.Section(attrs, "type_info.table")
	tableFields := commonJSON.InterfaceSlice(table["fields"])
	if len(tableFields) != 1 {
		t.Fatalf("type_info.table.fields = %#v, want one field", tableFields)
	}
}

func TestEnrichResourceAttributesDetectsUnknownDocumentAndWritesDocumentInfo(t *testing.T) {
	t.Parallel()

	content := resourceAttributesTestDOCX(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>正文</w:t></w:r></w:p></w:body></w:document>`,
		"docProps/core.xml": `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>统一文档</dc:title><dc:language>zh-CN</dc:language></cp:coreProperties>`,
		"docProps/app.xml":  `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Pages>3</Pages><Words>88</Words></Properties>`,
	})
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "docs/report.docx",
			SizeBytes:          &size,
		},
		PhysicalPath: "docs/report.docx",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: rangeBytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  "docs/report.docx",
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if enriched.DataType != datatype.Document || enriched.Format != string(format.FormatDOCX) {
		t.Fatalf("enriched item = %s/%s, want document/docx", enriched.DataType, enriched.Format)
	}
	if got := commonJSON.String(attrs, "item", "data_type"); got != string(datatype.Document) {
		t.Fatalf("item.data_type = %q, want document", got)
	}
	if got := commonJSON.String(attrs, "item", "format"); got != string(format.FormatDOCX) {
		t.Fatalf("item.format = %q, want docx", got)
	}
	document := commonJSON.Section(attrs, "type_info.document")
	if document["title"] != "统一文档" || commonJSON.InterfaceInt64(document["page_count"]) != 3 {
		t.Fatalf("type_info.document = %#v, want title and page_count", document)
	}
}

func TestEnrichResourceAttributesDetectsUnknownMediaAndWritesMediaInfo(t *testing.T) {
	t.Parallel()

	content := resourceAttributesTestPNG(t, 2, 3)
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "images/pixel.png",
			SizeBytes:          &size,
		},
		PhysicalPath: "images/pixel.png",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  "images/pixel.png",
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if enriched.DataType != datatype.Media || enriched.Format != string(format.FormatPNG) {
		t.Fatalf("enriched item = %s/%s, want media/png", enriched.DataType, enriched.Format)
	}
	media := commonJSON.Section(attrs, "type_info.media")
	if media["kind"] != string(datatype.MediaKindImage) || commonJSON.InterfaceInt64(media["width"]) != 2 || commonJSON.InterfaceInt64(media["height"]) != 3 {
		t.Fatalf("type_info.media = %#v, want image 2x3", media)
	}
}

func TestEnrichResourceAttributesWritesMediaFormatInfo(t *testing.T) {
	t.Parallel()

	content := resourceAttributesTestTIFF(t, 2, 3)
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Media,
			Format:             string(format.FormatTIFF),
			PrimaryContentPath: "images/pixel.tif",
			SizeBytes:          &size,
		},
		PhysicalPath: "images/pixel.tif",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	_, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  "images/pixel.tif",
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	tiffInfo := commonJSON.Section(attrs, "format_info.tiff")
	if tiffInfo["profile"] != "plain_tiff" {
		t.Fatalf("format_info.tiff = %#v, want plain_tiff profile", tiffInfo)
	}
	if tiffInfo["big_tiff"] != false {
		t.Fatalf("format_info.tiff.big_tiff = %#v, want false", tiffInfo["big_tiff"])
	}
}

func TestEnrichResourceAttributesWritesMediaInfoForMultiTIFF(t *testing.T) {
	t.Parallel()

	content := resourceAttributesTestTIFF(t, 2, 3)
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutMulti,
			DataType:           datatype.Media,
			Format:             string(format.FormatTIFF),
			PrimaryContentPath: "geotiff/srtm_40_01.tif",
			RefList: []dataitem.ItemRef{
				{Path: "geotiff/srtm_40_01.tif", Role: "main", Required: true, Primary: true, Extension: ".tif"},
				{Path: "geotiff/srtm_40_01.tfw", Role: "world_file", Extension: ".tfw"},
				{Path: "geotiff/srtm_40_01.tif.aux.xml", Role: "auxiliary_metadata", Extension: ".aux.xml"},
			},
			SizeBytes: &size,
		},
		PhysicalPath: "geotiff/srtm_40_01.tif",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	_, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  "geotiff/srtm_40_01.tif",
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	media := commonJSON.Section(attrs, "type_info.media")
	if commonJSON.InterfaceInt64(media["width"]) != 2 || commonJSON.InterfaceInt64(media["height"]) != 3 {
		t.Fatalf("type_info.media = %#v, want primary TIFF dimensions", media)
	}
	tiffInfo := commonJSON.Section(attrs, "format_info.tiff")
	if tiffInfo["profile"] != "plain_tiff" || tiffInfo["big_tiff"] != false {
		t.Fatalf("format_info.tiff = %#v, want primary TIFF format info", tiffInfo)
	}
	if refs := commonJSON.InterfaceSlice(commonJSON.Section(attrs, "item")["refs"]); len(refs) != 3 {
		t.Fatalf("item.refs = %#v, want multi refs preserved", refs)
	}
}

func TestEnrichResourceAttributesDetectsUnknownGLBAndWritesModel3DInfo(t *testing.T) {
	t.Parallel()

	content := resourceAttributesTestGLB([]byte(`{
		"asset":{"version":"2.0","generator":"resource-attributes-test"},
		"nodes":[{}],
		"meshes":[{"primitives":[{"attributes":{"POSITION":0},"indices":1}]}],
		"materials":[{}],
		"textures":[{}],
		"accessors":[
			{"count":8,"type":"VEC3","min":[1,2,3],"max":[4,5,6]},
			{"count":12,"type":"SCALAR"}
		]
	}`))
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "models/building.glb",
			SizeBytes:          &size,
		},
		PhysicalPath: "models/building.glb",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  "models/building.glb",
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if enriched.DataType != datatype.Model3D || enriched.Format != string(format.FormatGLB) {
		t.Fatalf("enriched item = %s/%s, want model_3d/glb", enriched.DataType, enriched.Format)
	}
	if got := commonJSON.String(attrs, "item", "data_type"); got != string(datatype.Model3D) {
		t.Fatalf("item.data_type = %q, want model_3d", got)
	}
	if got := commonJSON.String(attrs, "item", "format"); got != string(format.FormatGLB) {
		t.Fatalf("item.format = %q, want glb", got)
	}
	model := commonJSON.Section(attrs, "type_info.model_3d")
	if model["model_kind"] != string(datatype.Model3DKindMeshScene) {
		t.Fatalf("type_info.model_3d = %#v, want mesh_scene", model)
	}
	if commonJSON.InterfaceInt64(model["vertex_count"]) != 8 || commonJSON.InterfaceInt64(model["triangle_count"]) != 4 {
		t.Fatalf("type_info.model_3d = %#v, want vertex_count 8 and triangle_count 4", model)
	}
	formatInfo := commonJSON.Section(attrs, "format_info.glb")
	if formatInfo["gltf_version"] != "2.0" {
		t.Fatalf("format_info.glb = %#v, want gltf_version 2.0", formatInfo)
	}
}

func TestEnrichResourceAttributesDetectsSingleModel3DFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		path          string
		content       []byte
		wantFormat    format.FormatType
		wantModelKind string
		infoKey       string
	}{
		{
			name: "obj",
			path: "models/mesh.obj",
			content: []byte(`o mesh
v 0 0 0
v 1 0 0
v 0 1 0
f 1 2 3
`),
			wantFormat:    format.FormatOBJ,
			wantModelKind: datatype.Model3DKindMeshScene,
			infoKey:       "format_info.obj",
		},
		{
			name: "stl",
			path: "models/mesh.stl",
			content: []byte(`solid mesh
facet normal 0 0 1
 outer loop
  vertex 0 0 0
  vertex 1 0 0
  vertex 0 1 0
 endloop
endfacet
endsolid mesh`),
			wantFormat:    format.FormatSTL,
			wantModelKind: datatype.Model3DKindMeshScene,
			infoKey:       "format_info.stl",
		},
		{
			name:          "fbx",
			path:          "models/mesh.fbx",
			content:       append([]byte("Kaydara FBX Binary  \x00\x1a\x00"), []byte{0, 0, 0, 0}...),
			wantFormat:    format.FormatFBX,
			wantModelKind: datatype.Model3DKindMeshScene,
			infoKey:       "format_info.fbx",
		},
		{
			name: "ifc",
			path: "models/building.ifc",
			content: []byte(`ISO-10303-21;
HEADER;
FILE_SCHEMA(('IFC4'));
ENDSEC;
DATA;
#1=IFCPROJECT('0',$,'Project',$,$,$,$,$,$);
#2=IFCBUILDING('1',$,'Building',$,$,$,$,$,$,$,$);
ENDSEC;
END-ISO-10303-21;`),
			wantFormat:    format.FormatIFC,
			wantModelKind: datatype.Model3DKindBIMModel,
			infoKey:       "format_info.ifc",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			size := int64(len(tt.content))
			item := &metaitem.DetectedItem{
				ResolvedItem: dataitem.ResolvedItem{
					Layout:             format.LayoutSingle,
					DataType:           datatype.Unknown,
					Format:             string(format.FormatUnknown),
					PrimaryContentPath: tt.path,
					SizeBytes:          &size,
				},
				PhysicalPath: tt.path,
			}
			attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

			enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
				ContentReader: bytesContentReader{content: tt.content},
				Item:          item,
				PhysicalPath:  tt.path,
				SizeBytes:     size,
				CatalogPathFor: func(path string) plugin.CatalogPath {
					return plugin.FileItemPath(1, path)
				},
			})
			if err != nil {
				t.Fatalf("EnrichResourceAttributes() error = %v", err)
			}
			if enriched.DataType != datatype.Model3D || enriched.Format != string(tt.wantFormat) {
				t.Fatalf("enriched item = %s/%s, want model_3d/%s", enriched.DataType, enriched.Format, tt.wantFormat)
			}
			model := commonJSON.Section(attrs, "type_info.model_3d")
			if model["model_kind"] != tt.wantModelKind {
				t.Fatalf("type_info.model_3d = %#v, want %s", model, tt.wantModelKind)
			}
			if info := commonJSON.Section(attrs, tt.infoKey); len(info) == 0 {
				t.Fatalf("%s is empty, attrs=%#v", tt.infoKey, attrs)
			}
		})
	}
}

func TestEnrichResourceAttributesClassifiesPLYLayouts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		content      []byte
		wantDataType datatype.DataType
		typeInfoPath string
		wantLayout   string
	}{
		{
			name: "mesh",
			content: []byte(`ply
format ascii 1.0
element vertex 8
property float x
property float y
property float z
element face 12
property list uchar int vertex_indices
end_header
`),
			wantDataType: datatype.Model3D,
			typeInfoPath: "type_info.model_3d",
			wantLayout:   "mesh",
		},
		{
			name: "point_cloud",
			content: []byte(`ply
format ascii 1.0
element vertex 3
property float x
property float y
property float z
property uchar red
property uchar green
property uchar blue
property float intensity
end_header
`),
			wantDataType: datatype.PointCloud,
			typeInfoPath: "type_info.point_cloud",
			wantLayout:   "point_cloud",
		},
		{
			name: "gaussian_splat",
			content: []byte(`ply
format binary_little_endian 1.0
element vertex 3843812
property float x
property float y
property float z
property float f_dc_0
property float f_dc_1
property float f_dc_2
property float opacity
property float scale_0
property float scale_1
property float scale_2
property float rot_0
property float rot_1
property float rot_2
property float rot_3
end_header
`),
			wantDataType: datatype.GaussianSplat,
			typeInfoPath: "type_info.gaussian_splat",
			wantLayout:   "gaussian_splat",
		},
		{
			name: "compressed_gaussian_splat",
			content: []byte(`ply
format binary_little_endian 1.0
comment Generated by SuperSplat 2.6.2
element chunk 2605
property float min_x
property float min_y
property float min_z
property float max_x
property float max_y
property float max_z
element vertex 666816
property uint packed_position
property uint packed_rotation
property uint packed_scale
property uint packed_color
element sh 666816
property uchar f_rest_0
property uchar f_rest_1
property uchar f_rest_2
property uchar f_rest_3
property uchar f_rest_4
property uchar f_rest_5
property uchar f_rest_6
property uchar f_rest_7
property uchar f_rest_8
property uchar f_rest_9
property uchar f_rest_10
property uchar f_rest_11
property uchar f_rest_12
property uchar f_rest_13
property uchar f_rest_14
property uchar f_rest_15
property uchar f_rest_16
property uchar f_rest_17
property uchar f_rest_18
property uchar f_rest_19
property uchar f_rest_20
property uchar f_rest_21
property uchar f_rest_22
property uchar f_rest_23
property uchar f_rest_24
property uchar f_rest_25
property uchar f_rest_26
property uchar f_rest_27
property uchar f_rest_28
property uchar f_rest_29
property uchar f_rest_30
property uchar f_rest_31
property uchar f_rest_32
property uchar f_rest_33
property uchar f_rest_34
property uchar f_rest_35
property uchar f_rest_36
property uchar f_rest_37
property uchar f_rest_38
property uchar f_rest_39
property uchar f_rest_40
property uchar f_rest_41
property uchar f_rest_42
property uchar f_rest_43
property uchar f_rest_44
end_header
`),
			wantDataType: datatype.GaussianSplat,
			typeInfoPath: "type_info.gaussian_splat",
			wantLayout:   "gaussian_splat",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			size := int64(len(tt.content))
			item := &metaitem.DetectedItem{
				ResolvedItem: dataitem.ResolvedItem{
					Layout:             format.LayoutSingle,
					DataType:           datatype.Unknown,
					Format:             string(format.FormatUnknown),
					PrimaryContentPath: "models/" + tt.name + ".ply",
					SizeBytes:          &size,
				},
				PhysicalPath: "models/" + tt.name + ".ply",
			}
			attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

			enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
				ContentReader: bytesContentReader{content: tt.content},
				Item:          item,
				PhysicalPath:  item.PhysicalPath,
				SizeBytes:     size,
				CatalogPathFor: func(path string) plugin.CatalogPath {
					return plugin.FileItemPath(1, path)
				},
			})
			if err != nil {
				t.Fatalf("EnrichResourceAttributes() error = %v", err)
			}
			if enriched.DataType != tt.wantDataType || enriched.Format != string(format.FormatPLY) {
				t.Fatalf("enriched item = %s/%s, want %s/ply", enriched.DataType, enriched.Format, tt.wantDataType)
			}
			if got := commonJSON.String(attrs, "item", "data_type"); got != string(tt.wantDataType) {
				t.Fatalf("item.data_type = %q, want %s", got, tt.wantDataType)
			}
			if info := commonJSON.Section(attrs, tt.typeInfoPath); len(info) == 0 {
				t.Fatalf("%s is empty, attrs=%#v", tt.typeInfoPath, attrs)
			}
			formatInfo := commonJSON.Section(attrs, "format_info.ply")
			if formatInfo["layout"] != tt.wantLayout {
				t.Fatalf("format_info.ply = %#v, want layout %s", formatInfo, tt.wantLayout)
			}
		})
	}
}

func TestEnrichResourceAttributesWritesSampledBoundsForLargeGaussianPLY(t *testing.T) {
	t.Parallel()

	content := resourceAttributesLargeGaussianPLY()
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "models/large-gaussian.ply",
			SizeBytes:          &size,
		},
		PhysicalPath: "models/large-gaussian.ply",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: rangeBytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  item.PhysicalPath,
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if enriched.DataType != datatype.GaussianSplat || enriched.Format != string(format.FormatPLY) {
		t.Fatalf("enriched item = %s/%s, want gaussian_splat/ply", enriched.DataType, enriched.Format)
	}
	info := commonJSON.Section(attrs, "type_info.gaussian_splat")
	if info["bounds_3d"] != nil {
		t.Fatalf("bounds_3d = %#v, want nil for large PLY meta scan", info["bounds_3d"])
	}
	if info["sampled_bounds_method"] != "sampled_binary_vertices" {
		t.Fatalf("sampled_bounds_method = %#v, want sampled_binary_vertices", info["sampled_bounds_method"])
	}
	if commonJSON.InterfaceInt64(info["sampled_bounds_sample_count"]) <= 0 {
		t.Fatalf("sampled_bounds_sample_count = %#v, want positive", info["sampled_bounds_sample_count"])
	}
	bounds := commonJSON.Section(info, "sampled_bounds_3d")
	if len(bounds) == 0 {
		t.Fatalf("sampled_bounds_3d is empty, info=%#v", info)
	}
	if bounds["min_x"] == nil || bounds["max_x"] == nil {
		t.Fatalf("sampled_bounds_3d = %#v, want x bounds", bounds)
	}
}

func TestEnrichResourceAttributesClassifiesSplatAsGaussianSplat(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	writeResourceAttributesSplatRecord(&buffer, -1, 2, 10, 0.01, 0.01, 0.01, 255)
	writeResourceAttributesSplatRecord(&buffer, 3, -4, 20, 0.001, 0.1, 0.001, 250)
	content := buffer.Bytes()
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "models/site.splat",
			SizeBytes:          &size,
		},
		PhysicalPath: "models/site.splat",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  item.PhysicalPath,
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if enriched.DataType != datatype.GaussianSplat || enriched.Format != string(format.FormatSplat) {
		t.Fatalf("enriched item = %s/%s, want gaussian_splat/splat", enriched.DataType, enriched.Format)
	}
	if got := commonJSON.String(attrs, "item", "data_type"); got != string(datatype.GaussianSplat) {
		t.Fatalf("item.data_type = %q, want gaussian_splat", got)
	}
	if got := commonJSON.String(attrs, "item", "format"); got != string(format.FormatSplat) {
		t.Fatalf("item.format = %q, want splat", got)
	}
	if info := commonJSON.Section(attrs, "type_info.gaussian_splat"); info["representation"] != datatype.GaussianSplatRepresentation3DGS {
		t.Fatalf("type_info.gaussian_splat = %#v, want representation", info)
	}
	formatInfo := commonJSON.Section(attrs, "format_info.splat")
	if formatInfo["encoding"] != "splat" {
		t.Fatalf("format_info.splat = %#v, want splat encoding", formatInfo)
	}
	scaleStats := commonJSON.Section(formatInfo, "scale_stats")
	if scaleStats["method"] != "exact_splat_records" {
		t.Fatalf("format_info.splat.scale_stats = %#v, want exact method", scaleStats)
	}
	if commonJSON.InterfaceInt64(scaleStats["sample_count"]) != 2 {
		t.Fatalf("format_info.splat.scale_stats.sample_count = %#v, want 2", scaleStats["sample_count"])
	}
	if commonJSON.InterfaceInt64(scaleStats["anisotropic_count"]) != 1 {
		t.Fatalf("format_info.splat.scale_stats.anisotropic_count = %#v, want 1", scaleStats["anisotropic_count"])
	}
	diagnostic := commonJSON.Section(formatInfo, "render_diagnostic")
	if diagnostic["recommended_render_mode"] != "2d" {
		t.Fatalf("format_info.splat.render_diagnostic = %#v, want 2d", diagnostic)
	}
}

func TestEnrichResourceAttributesClassifiesKSplatAsGaussianSplat(t *testing.T) {
	t.Parallel()

	content := []byte{0, 1, 2, 3}
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "models/site.ksplat",
			SizeBytes:          &size,
		},
		PhysicalPath: "models/site.ksplat",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  item.PhysicalPath,
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if enriched.DataType != datatype.GaussianSplat || enriched.Format != string(format.FormatKSplat) {
		t.Fatalf("enriched item = %s/%s, want gaussian_splat/ksplat", enriched.DataType, enriched.Format)
	}
	if got := commonJSON.String(attrs, "item", "data_type"); got != string(datatype.GaussianSplat) {
		t.Fatalf("item.data_type = %q, want gaussian_splat", got)
	}
	if got := commonJSON.String(attrs, "item", "format"); got != string(format.FormatKSplat) {
		t.Fatalf("item.format = %q, want ksplat", got)
	}
	if info := commonJSON.Section(attrs, "type_info.gaussian_splat"); info["representation"] != datatype.GaussianSplatRepresentation3DGS {
		t.Fatalf("type_info.gaussian_splat = %#v, want representation", info)
	}
	if formatInfo := commonJSON.Section(attrs, "format_info.ksplat"); formatInfo["encoding"] != "ksplat" {
		t.Fatalf("format_info.ksplat = %#v, want ksplat encoding", formatInfo)
	}
}

func TestEnrichResourceAttributesDetectsUnknownLASAndWritesPointCloudInfo(t *testing.T) {
	t.Parallel()

	content := resourceAttributesTestLASHeader()
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "point-cloud/site.las",
			SizeBytes:          &size,
		},
		PhysicalPath: "point-cloud/site.las",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  "point-cloud/site.las",
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if enriched.DataType != datatype.PointCloud || enriched.Format != string(format.FormatLAS) {
		t.Fatalf("enriched item = %s/%s, want point_cloud/las", enriched.DataType, enriched.Format)
	}
	if got := commonJSON.String(attrs, "item", "data_type"); got != string(datatype.PointCloud) {
		t.Fatalf("item.data_type = %q, want point_cloud", got)
	}
	if got := commonJSON.String(attrs, "item", "format"); got != string(format.FormatLAS) {
		t.Fatalf("item.format = %q, want las", got)
	}
	pointCloud := commonJSON.Section(attrs, "type_info.point_cloud")
	if pointCloud["point_cloud_kind"] != string(datatype.PointCloudKindRawPointCloud) {
		t.Fatalf("type_info.point_cloud = %#v, want raw_point_cloud", pointCloud)
	}
	if commonJSON.InterfaceInt64(pointCloud["point_count"]) != 123456789 {
		t.Fatalf("type_info.point_cloud = %#v, want point_count 123456789", pointCloud)
	}
	if pointCloud["point_format"] != "las_1.4_point_format_7" {
		t.Fatalf("type_info.point_cloud = %#v, want point format 7", pointCloud)
	}
	spatial := commonJSON.Section(attrs, "capabilities.spatial")
	if extent := commonJSON.InterfaceSlice(spatial["extent"]); len(extent) != 4 {
		t.Fatalf("capabilities.spatial = %#v, want extent", spatial)
	}
	formatInfo := commonJSON.Section(attrs, "format_info.las")
	if formatInfo["version"] != "1.4" {
		t.Fatalf("format_info.las = %#v, want version 1.4", formatInfo)
	}
}

func TestEnrichResourceAttributesDetectsUnknownLAZAndWritesPointCloudInfo(t *testing.T) {
	t.Parallel()

	content := resourceAttributesTestLASHeader()
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "point-cloud/site.laz",
			SizeBytes:          &size,
		},
		PhysicalPath: "point-cloud/site.laz",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  "point-cloud/site.laz",
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if enriched.DataType != datatype.PointCloud || enriched.Format != string(format.FormatLAZ) {
		t.Fatalf("enriched item = %s/%s, want point_cloud/laz", enriched.DataType, enriched.Format)
	}
	pointCloud := commonJSON.Section(attrs, "type_info.point_cloud")
	if pointCloud["point_cloud_kind"] != string(datatype.PointCloudKindRawPointCloud) {
		t.Fatalf("type_info.point_cloud = %#v, want raw_point_cloud", pointCloud)
	}
	formatInfo := commonJSON.Section(attrs, "format_info.laz")
	if formatInfo["compression"] != "laszip" || formatInfo["version"] != "1.4" {
		t.Fatalf("format_info.laz = %#v, want laszip version 1.4", formatInfo)
	}
}

func TestEnrichResourceAttributesDetectsUnknownCOPCAndWritesTiledPointCloudInfo(t *testing.T) {
	t.Parallel()

	content := resourceAttributesTestCOPCHeader()
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "point-cloud/site.copc.laz",
			SizeBytes:          &size,
		},
		PhysicalPath: "point-cloud/site.copc.laz",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: rangeBytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  "point-cloud/site.copc.laz",
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if enriched.DataType != datatype.PointCloud || enriched.Format != string(format.FormatCOPC) {
		t.Fatalf("enriched item = %s/%s, want point_cloud/copc", enriched.DataType, enriched.Format)
	}
	pointCloud := commonJSON.Section(attrs, "type_info.point_cloud")
	if pointCloud["point_cloud_kind"] != string(datatype.PointCloudKindTiledPointCloud) {
		t.Fatalf("type_info.point_cloud = %#v, want tiled_point_cloud", pointCloud)
	}
	formatInfo := commonJSON.Section(attrs, "format_info.copc")
	if formatInfo["profile"] != "copc" ||
		formatInfo["compression"] != "laszip" ||
		commonJSON.InterfaceInt64(formatInfo["root_hierarchy_size"]) != 64 ||
		commonJSON.InterfaceInt64(formatInfo["root_hierarchy_entry_count"]) != 2 ||
		commonJSON.InterfaceInt64(formatInfo["root_hierarchy_point_count"]) != 250 {
		t.Fatalf("format_info.copc = %#v, want copc laszip with root hierarchy summary", formatInfo)
	}
}

func TestEnrichResourceAttributesDetectsUnknownE57AndWritesScanCollectionInfo(t *testing.T) {
	t.Parallel()

	content := resourceAttributesTestE57()
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "point-cloud/bunny.e57",
			SizeBytes:          &size,
		},
		PhysicalPath: "point-cloud/bunny.e57",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  "point-cloud/bunny.e57",
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if enriched.DataType != datatype.PointCloud || enriched.Format != string(format.FormatE57) {
		t.Fatalf("enriched item = %s/%s, want point_cloud/e57", enriched.DataType, enriched.Format)
	}
	pointCloud := commonJSON.Section(attrs, "type_info.point_cloud")
	if pointCloud["point_cloud_kind"] != string(datatype.PointCloudKindScanCollection) {
		t.Fatalf("type_info.point_cloud = %#v, want scan_collection", pointCloud)
	}
	if commonJSON.InterfaceInt64(pointCloud["point_count"]) != 30571 {
		t.Fatalf("type_info.point_cloud = %#v, want point_count 30571", pointCloud)
	}
	spatial := commonJSON.Section(attrs, "capabilities.spatial")
	if extent := commonJSON.InterfaceSlice(spatial["extent"]); len(extent) != 4 {
		t.Fatalf("capabilities.spatial = %#v, want extent", spatial)
	}
	formatInfo := commonJSON.Section(attrs, "format_info.e57")
	if formatInfo["scan_count"] != 1 || formatInfo["xml_read"] != true {
		t.Fatalf("format_info.e57 = %#v, want scan_count 1 and xml_read true", formatInfo)
	}
}

func TestEnrichResourceAttributesDetectsUnknownPCDAndWritesPointCloudInfo(t *testing.T) {
	t.Parallel()

	content := []byte(`# .PCD v0.7 - Point Cloud Data file format
VERSION 0.7
FIELDS x y z rgb
SIZE 4 4 4 4
TYPE F F F U
COUNT 1 1 1 1
WIDTH 2
HEIGHT 1
POINTS 2
DATA ascii
0 1 2 255
3 4 5 255
`)
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "point-cloud/sample.pcd",
			SizeBytes:          &size,
		},
		PhysicalPath: "point-cloud/sample.pcd",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  "point-cloud/sample.pcd",
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if enriched.DataType != datatype.PointCloud || enriched.Format != string(format.FormatPCD) {
		t.Fatalf("enriched item = %s/%s, want point_cloud/pcd", enriched.DataType, enriched.Format)
	}
	pointCloud := commonJSON.Section(attrs, "type_info.point_cloud")
	if commonJSON.InterfaceInt64(pointCloud["point_count"]) != 2 {
		t.Fatalf("type_info.point_cloud = %#v, want point_count 2", pointCloud)
	}
	if pointCloud["has_color"] != true {
		t.Fatalf("type_info.point_cloud = %#v, want has_color true", pointCloud)
	}
	formatInfo := commonJSON.Section(attrs, "format_info.pcd")
	if formatInfo["data"] != "ascii" || commonJSON.InterfaceInt64(formatInfo["points"]) != 2 {
		t.Fatalf("format_info.pcd = %#v, want ascii points 2", formatInfo)
	}
}

func TestEnrichResourceAttributesDetectsUnknownXYZAndWritesPointCloudInfo(t *testing.T) {
	t.Parallel()

	content := []byte("0 1 2\n3 4 5\n")
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "point-cloud/sample.xyz",
			SizeBytes:          &size,
		},
		PhysicalPath: "point-cloud/sample.xyz",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	enriched, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: content},
		Item:          item,
		PhysicalPath:  "point-cloud/sample.xyz",
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if enriched.DataType != datatype.PointCloud || enriched.Format != string(format.FormatXYZ) {
		t.Fatalf("enriched item = %s/%s, want point_cloud/xyz", enriched.DataType, enriched.Format)
	}
	pointCloud := commonJSON.Section(attrs, "type_info.point_cloud")
	if commonJSON.InterfaceInt64(pointCloud["point_count"]) != 2 {
		t.Fatalf("type_info.point_cloud = %#v, want point_count 2", pointCloud)
	}
	if bounds := commonJSON.Section(attrs, "type_info.point_cloud")["bounds_3d"]; bounds == nil {
		t.Fatalf("type_info.point_cloud = %#v, want bounds_3d", pointCloud)
	}
	spatial := commonJSON.Section(attrs, "capabilities.spatial")
	if extent := commonJSON.InterfaceSlice(spatial["extent"]); len(extent) != 4 {
		t.Fatalf("capabilities.spatial = %#v, want extent", spatial)
	}
	formatInfo := commonJSON.Section(attrs, "format_info.xyz")
	if formatInfo["scan_complete"] != true || formatInfo["delimiter"] != "whitespace" {
		t.Fatalf("format_info.xyz = %#v, want complete whitespace", formatInfo)
	}
}

func TestResolveAndEnrichMultiTIFFUsesPrimaryContent(t *testing.T) {
	t.Parallel()

	content := resourceAttributesTestTIFF(t, 2, 3)
	size := int64(len(content))
	result, err := metaitem.ResolveItems(context.Background(), metaitem.DirectoryResolveInput{
		DirPath: "geotiff",
		Files: []metaitem.StorageFileRef{
			{Name: "srtm_40_01.tif", Path: "geotiff/srtm_40_01.tif", Size: size},
			{Name: "srtm_40_01.tfw", Path: "geotiff/srtm_40_01.tfw", Size: 42},
			{Name: "srtm_40_01.hdr", Path: "geotiff/srtm_40_01.hdr", Size: 84},
			{Name: "srtm_40_01.tif.aux.xml", Path: "geotiff/srtm_40_01.tif.aux.xml", Size: 126},
		},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("Items = %#v, want one multi TIFF item", result.Items)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutMulti || item.PrimaryContentPath != "geotiff/srtm_40_01.tif" {
		t.Fatalf("resolved item = %#v, want multi TIFF primary", item)
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
	reader := pathContentReader{content: map[string][]byte{
		"geotiff/srtm_40_01.tif": content,
	}}

	_, _, err = EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: reader,
		Item:          item,
		PhysicalPath:  item.PrimaryContentPath,
		SizeBytes:     size,
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if media := commonJSON.Section(attrs, "type_info.media"); commonJSON.InterfaceInt64(media["width"]) != 2 || commonJSON.InterfaceInt64(media["height"]) != 3 {
		t.Fatalf("type_info.media = %#v, want primary TIFF media facts", media)
	}
	if tiffInfo := commonJSON.Section(attrs, "format_info.tiff"); tiffInfo["profile"] != "plain_tiff" {
		t.Fatalf("format_info.tiff = %#v, want primary TIFF profile", tiffInfo)
	}
	if refs := commonJSON.InterfaceSlice(commonJSON.Section(attrs, "item")["refs"]); len(refs) != 4 {
		t.Fatalf("item.refs = %#v, want sidecar refs preserved", refs)
	}
}

func TestResolveAndEnrichMultiGLTFUsesPrimaryManifest(t *testing.T) {
	t.Parallel()

	content := []byte(`{
		"asset":{"version":"2.0","generator":"ADDP test"},
		"scene":0,
		"scenes":[{"nodes":[0]}],
		"nodes":[{"mesh":0}],
		"meshes":[{"primitives":[{"attributes":{"POSITION":0},"indices":1}]}],
		"materials":[{}],
		"textures":[{},{}],
		"images":[{"uri":"textures/baseColor.png"},{"uri":"textures/normal.ktx2"}],
		"buffers":[{"uri":"buffers/geometry.bin","byteLength":256}],
		"accessors":[
			{"count":8,"type":"VEC3","min":[0,0,0],"max":[1,2,3]},
			{"count":12,"type":"SCALAR"}
		],
		"extensionsUsed":["KHR_texture_basisu"]
	}`)
	result, err := metaitem.ResolveItems(context.Background(), metaitem.DirectoryResolveInput{
		ContentReader: pathContentReader{content: map[string][]byte{
			"models/building/scene.gltf": content,
		}},
		DirPath: "models/building",
		Files: []metaitem.StorageFileRef{
			{Name: "scene.gltf", Path: "models/building/scene.gltf", Size: int64(len(content))},
			{Name: "geometry.bin", Path: "models/building/buffers/geometry.bin", Size: 256},
			{Name: "baseColor.png", Path: "models/building/textures/baseColor.png", Size: 32},
			{Name: "normal.ktx2", Path: "models/building/textures/normal.ktx2", Size: 64},
		},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("Items = %#v, want one glTF item", result.Items)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutMulti || item.Format != string(format.FormatGLTF) || item.PrimaryContentPath != "models/building/scene.gltf" {
		t.Fatalf("resolved item = %#v, want multi glTF primary manifest", item)
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	_, _, err = EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: pathContentReader{content: map[string][]byte{
			"models/building/scene.gltf": content,
		}},
		Item:         item,
		PhysicalPath: item.PrimaryContentPath,
		SizeBytes:    int64(len(content)),
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	modelInfo := commonJSON.Section(attrs, "type_info.model_3d")
	if modelInfo["model_kind"] != datatype.Model3DKindMeshScene || commonJSON.InterfaceInt64(modelInfo["vertex_count"]) != 8 || commonJSON.InterfaceInt64(modelInfo["triangle_count"]) != 4 {
		t.Fatalf("type_info.model_3d = %#v, want mesh_scene counts", modelInfo)
	}
	formatInfo := commonJSON.Section(attrs, "format_info.gltf")
	if formatInfo["gltf_version"] != "2.0" || formatInfo["generator"] != "ADDP test" || commonJSON.InterfaceInt64(formatInfo["external_resource_count"]) != 3 {
		t.Fatalf("format_info.gltf = %#v, want glTF manifest facts", formatInfo)
	}
	if refs := commonJSON.InterfaceSlice(commonJSON.Section(attrs, "item")["refs"]); len(refs) != 4 {
		t.Fatalf("item.refs = %#v, want glTF refs preserved", refs)
	}
}

func TestEnrichResourceAttributesKeepsPartialTIFFFormatInfo(t *testing.T) {
	t.Parallel()

	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Media,
			Format:             string(format.FormatTIFF),
			PrimaryContentPath: "images/large.tif",
		},
		PhysicalPath: "images/large.tif",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	_, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: bytesContentReader{content: []byte{'I', 'I', 43, 0, 8, 0, 0, 0}},
		Item:          item,
		PhysicalPath:  "images/large.tif",
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if media := commonJSON.Section(attrs, "type_info.media"); len(media) != 0 {
		t.Fatalf("type_info.media = %#v, want no partial media facts", media)
	}
	tiffInfo := commonJSON.Section(attrs, "format_info.tiff")
	if tiffInfo["profile"] != "unknown" || tiffInfo["big_tiff"] != true {
		t.Fatalf("format_info.tiff = %#v, want partial BigTIFF facts", tiffInfo)
	}
}

type failingContentReader struct{}

func (r failingContentReader) Type() string         { return "failing" }
func (r failingContentReader) DisplayName() string  { return "failing" }
func (r failingContentReader) EngineOrigin() string { return "general" }
func (r failingContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r failingContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r failingContentReader) DefaultPort() int                                   { return 0 }
func (r failingContentReader) RequiredFields() []string                           { return nil }
func (r failingContentReader) SensitiveFields() []string                          { return nil }
func (r failingContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r failingContentReader) StoreSemantics() plugin.StoreSemantics { return plugin.StoreSemantics{} }
func (r failingContentReader) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("open failed")
}

type bytesContentReader struct {
	content []byte
}

func (r bytesContentReader) Type() string         { return "bytes" }
func (r bytesContentReader) DisplayName() string  { return "bytes" }
func (r bytesContentReader) EngineOrigin() string { return "general" }
func (r bytesContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r bytesContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r bytesContentReader) DefaultPort() int                                   { return 0 }
func (r bytesContentReader) RequiredFields() []string                           { return nil }
func (r bytesContentReader) SensitiveFields() []string                          { return nil }
func (r bytesContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r bytesContentReader) StoreSemantics() plugin.StoreSemantics { return plugin.StoreSemantics{} }
func (r bytesContentReader) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.content)), nil
}

type rangeBytesContentReader struct {
	content []byte
}

func (r rangeBytesContentReader) Type() string         { return "range-bytes" }
func (r rangeBytesContentReader) DisplayName() string  { return "range-bytes" }
func (r rangeBytesContentReader) EngineOrigin() string { return "general" }
func (r rangeBytesContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r rangeBytesContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r rangeBytesContentReader) DefaultPort() int                                   { return 0 }
func (r rangeBytesContentReader) RequiredFields() []string                           { return nil }
func (r rangeBytesContentReader) SensitiveFields() []string                          { return nil }
func (r rangeBytesContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r rangeBytesContentReader) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (r rangeBytesContentReader) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.content)), nil
}
func (r rangeBytesContentReader) OpenRange(_ context.Context, _ plugin.ConnectionInfo, _ plugin.CatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	offset := opts.Offset
	length := opts.Length
	if offset < 0 {
		offset = 0
	}
	if length < 0 {
		length = 0
	}
	start := metaenrichMinInt64(offset, int64(len(r.content)))
	end := metaenrichMinInt64(start+length, int64(len(r.content)))
	return io.NopCloser(bytes.NewReader(r.content[start:end])), nil
}

func metaenrichMinInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

type pathContentReader struct {
	content map[string][]byte
}

func (r pathContentReader) Type() string         { return "path-content" }
func (r pathContentReader) DisplayName() string  { return "path-content" }
func (r pathContentReader) EngineOrigin() string { return "general" }
func (r pathContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r pathContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r pathContentReader) DefaultPort() int                                   { return 0 }
func (r pathContentReader) RequiredFields() []string                           { return nil }
func (r pathContentReader) SensitiveFields() []string                          { return nil }
func (r pathContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r pathContentReader) StoreSemantics() plugin.StoreSemantics { return plugin.StoreSemantics{} }
func (r pathContentReader) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	content, ok := r.content[path.StringPath()]
	if !ok {
		return nil, fmt.Errorf("unexpected content path %q", path.StringPath())
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func resourceAttributesLargeGaussianPLY() []byte {
	const vertexCount = 100001
	var buffer bytes.Buffer
	buffer.WriteString(`ply
format binary_little_endian 1.0
comment Generated by P3BJet GaussianSplats
element vertex 100001
property float x
property float y
property float z
property float f_dc_0
property float f_dc_1
property float f_dc_2
property float opacity
property float scale_0
property float scale_1
property float scale_2
property float rot_0
property float rot_1
property float rot_2
property float rot_3
end_header
`)
	record := make([]byte, 14*4)
	for i := 0; i < vertexCount; i++ {
		x, y, z := float32(0), float32(0), float32(0)
		switch i {
		case 0:
			x, y, z = -2, 3, 100
		case vertexCount - 1:
			x, y, z = 5, -7, 120
		}
		writeGaussianPLYRecord(record, x, y, z)
		buffer.Write(record)
	}
	return buffer.Bytes()
}

func writeResourceAttributesSplatRecord(buffer *bytes.Buffer, x, y, z, scaleX, scaleY, scaleZ float32, alpha byte) {
	record := make([]byte, 32)
	binary.LittleEndian.PutUint32(record[0:4], math.Float32bits(x))
	binary.LittleEndian.PutUint32(record[4:8], math.Float32bits(y))
	binary.LittleEndian.PutUint32(record[8:12], math.Float32bits(z))
	binary.LittleEndian.PutUint32(record[12:16], math.Float32bits(scaleX))
	binary.LittleEndian.PutUint32(record[16:20], math.Float32bits(scaleY))
	binary.LittleEndian.PutUint32(record[20:24], math.Float32bits(scaleZ))
	record[27] = alpha
	buffer.Write(record)
}

func writeGaussianPLYRecord(record []byte, x, y, z float32) {
	for i := range record {
		record[i] = 0
	}
	binary.LittleEndian.PutUint32(record[0:4], math.Float32bits(x))
	binary.LittleEndian.PutUint32(record[4:8], math.Float32bits(y))
	binary.LittleEndian.PutUint32(record[8:12], math.Float32bits(z))
	binary.LittleEndian.PutUint32(record[24:28], math.Float32bits(1))
	binary.LittleEndian.PutUint32(record[28:32], math.Float32bits(1))
	binary.LittleEndian.PutUint32(record[32:36], math.Float32bits(1))
	binary.LittleEndian.PutUint32(record[36:40], math.Float32bits(1))
}

func resourceAttributesTestDOCX(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}
	return buf.Bytes()
}

func resourceAttributesTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func resourceAttributesTestTIFF(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := tiff.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode tiff: %v", err)
	}
	return buf.Bytes()
}

func resourceAttributesTestGLB(jsonChunk []byte) []byte {
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, ' ')
	}
	totalLen := uint32(12 + 8 + len(jsonChunk))
	buf := bytes.NewBuffer(make([]byte, 0, totalLen))
	buf.WriteString("glTF")
	_ = binary.Write(buf, binary.LittleEndian, uint32(2))
	_ = binary.Write(buf, binary.LittleEndian, totalLen)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(jsonChunk)))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0x4E4F534A))
	buf.Write(jsonChunk)
	return buf.Bytes()
}

func resourceAttributesTestLASHeader() []byte {
	const headerSize = 375
	buf := make([]byte, headerSize)
	copy(buf[:4], []byte("LASF"))
	buf[24] = 1
	buf[25] = 4
	copy(buf[26:58], []byte("ADDP"))
	copy(buf[58:90], []byte("resource-attributes-test"))
	binary.LittleEndian.PutUint16(buf[94:96], uint16(headerSize))
	binary.LittleEndian.PutUint32(buf[96:100], headerSize)
	binary.LittleEndian.PutUint32(buf[100:104], 2)
	buf[104] = 7
	binary.LittleEndian.PutUint16(buf[105:107], 36)
	binary.LittleEndian.PutUint32(buf[107:111], 0)
	resourceAttributesTestPutFloat64(buf[131:139], 0.01)
	resourceAttributesTestPutFloat64(buf[139:147], 0.01)
	resourceAttributesTestPutFloat64(buf[147:155], 0.01)
	resourceAttributesTestPutFloat64(buf[155:163], 1000)
	resourceAttributesTestPutFloat64(buf[163:171], 2000)
	resourceAttributesTestPutFloat64(buf[171:179], 3000)
	resourceAttributesTestPutFloat64(buf[179:187], 10)
	resourceAttributesTestPutFloat64(buf[187:195], 1)
	resourceAttributesTestPutFloat64(buf[195:203], 20)
	resourceAttributesTestPutFloat64(buf[203:211], 2)
	resourceAttributesTestPutFloat64(buf[211:219], 30)
	resourceAttributesTestPutFloat64(buf[219:227], 3)
	binary.LittleEndian.PutUint64(buf[235:243], 4096)
	binary.LittleEndian.PutUint32(buf[243:247], 1)
	binary.LittleEndian.PutUint64(buf[247:255], 123456789)
	return buf
}

func resourceAttributesTestCOPCHeader() []byte {
	const (
		infoOffset    = 375
		vlrHeaderSize = 54
		infoSize      = 160
	)
	headerSize := infoOffset + vlrHeaderSize + infoSize
	buf := make([]byte, headerSize)
	copy(buf[:infoOffset], resourceAttributesTestLASHeader())
	binary.LittleEndian.PutUint32(buf[96:100], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[100:104], 1)
	buf[104] = 8
	binary.LittleEndian.PutUint16(buf[105:107], 38)
	vlrHeader := buf[infoOffset : infoOffset+vlrHeaderSize]
	copy(vlrHeader[2:18], []byte("copc"))
	binary.LittleEndian.PutUint16(vlrHeader[18:20], 1)
	binary.LittleEndian.PutUint16(vlrHeader[20:22], infoSize)
	copy(vlrHeader[22:54], []byte("COPC info"))
	info := buf[infoOffset+vlrHeaderSize:]
	resourceAttributesTestPutFloat64(info[0:8], 100)
	resourceAttributesTestPutFloat64(info[8:16], 200)
	resourceAttributesTestPutFloat64(info[16:24], 300)
	resourceAttributesTestPutFloat64(info[24:32], 500)
	resourceAttributesTestPutFloat64(info[32:40], 0.25)
	binary.LittleEndian.PutUint64(info[40:48], uint64(headerSize))
	binary.LittleEndian.PutUint64(info[48:56], 64)
	resourceAttributesTestPutFloat64(info[56:64], 10)
	resourceAttributesTestPutFloat64(info[64:72], 20)
	hierarchy := make([]byte, 64)
	binary.LittleEndian.PutUint32(hierarchy[0:4], 0)
	binary.LittleEndian.PutUint32(hierarchy[28:32], 0)
	binary.LittleEndian.PutUint32(hierarchy[32:36], 1)
	binary.LittleEndian.PutUint32(hierarchy[56:60], 4096)
	binary.LittleEndian.PutUint32(hierarchy[60:64], 250)
	buf = append(buf, hierarchy...)
	return buf
}

func resourceAttributesTestE57() []byte {
	xmlPayload := strings.TrimSpace(`
<?xml version="1.0" encoding="UTF-8"?>
<e57Root type="Structure" xmlns="http://www.astm.org/COMMIT/E57/2010-e57-v1.0">
  <formatName type="String">ASTM E57 3D Imaging Data File</formatName>
  <guid type="String">{resource-attributes-test}</guid>
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
	const pageSize = 1024
	const xmlOffset = 1024
	file := make([]byte, xmlOffset)
	copy(file[:8], []byte("ASTM-E57"))
	binary.LittleEndian.PutUint32(file[8:12], 1)
	binary.LittleEndian.PutUint64(file[24:32], xmlOffset)
	binary.LittleEndian.PutUint64(file[32:40], uint64(len(xmlBytes)))
	binary.LittleEndian.PutUint64(file[40:48], pageSize)
	file = append(file, resourceAttributesTestE57PagedPayload(xmlBytes, pageSize)...)
	binary.LittleEndian.PutUint64(file[16:24], uint64(len(file)))
	return file
}

func resourceAttributesTestE57PagedPayload(payload []byte, pageSize int) []byte {
	payloadSize := pageSize - 4
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

func resourceAttributesTestPutFloat64(target []byte, value float64) {
	binary.LittleEndian.PutUint64(target, math.Float64bits(value))
}
