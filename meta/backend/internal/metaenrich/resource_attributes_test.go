package metaenrich

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	stdimage "image"
	"image/color"
	"image/png"
	"io"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
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
