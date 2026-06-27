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
		ContentReader: bytesContentReader{content: content},
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

func resourceAttributesTestPutFloat64(target []byte, value float64) {
	binary.LittleEndian.PutUint64(target, math.Float64bits(value))
}
