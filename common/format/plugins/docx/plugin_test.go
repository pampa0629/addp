package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestPluginDescriptorDeclaresDocumentTextReader(t *testing.T) {
	plugin := NewPlugin()
	descriptor := plugin.Descriptor()
	if descriptor.Format != format.FormatDOCX {
		t.Fatalf("descriptor format = %q, want %q", descriptor.Format, format.FormatDOCX)
	}
	if descriptor.DataType != datatype.DataTypeDocument {
		t.Fatalf("descriptor data type = %q, want document", descriptor.DataType)
	}
	if !descriptor.Providers.DocumentInfo {
		t.Fatalf("docx should declare document info provider")
	}
	if !contains(descriptor.ContentReaders, string(format.ContentReaderDocumentText)) {
		t.Fatalf("content readers = %#v, want document text", descriptor.ContentReaders)
	}
	if !contains(descriptor.ContentReaders, string(format.ContentReaderRawContent)) ||
		!contains(descriptor.ContentReaders, string(format.ContentReaderRangeContent)) {
		t.Fatalf("content readers = %#v, want raw and range", descriptor.ContentReaders)
	}
}

func TestPluginDescribeDocument(t *testing.T) {
	plugin := NewPlugin()
	data := minimalDOCXWithFiles(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Hello</w:t></w:r></w:p></w:body></w:document>`,
		"docProps/core.xml": `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Design Doc</dc:title><dc:language>zh-CN</dc:language></cp:coreProperties>`,
		"docProps/app.xml":  `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Pages>3</Pages><Words>42</Words></Properties>`,
	})

	info, err := plugin.DescribeDocument(context.Background(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeDocument() error = %v", err)
	}
	if info.Title != "Design Doc" || info.Language != "zh-CN" || info.PageCount != 3 || info.WordCount != 42 {
		t.Fatalf("DescribeDocument() = %#v", info)
	}
}

func TestPluginReadDocumentText(t *testing.T) {
	plugin := NewPlugin()
	data := minimalDOCX(t, `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Hello</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>ADDP</w:t></w:r></w:p><w:p><w:r><w:t>Second line</w:t></w:r></w:p></w:body></w:document>`)

	text, truncated, err := plugin.ReadDocumentText(context.Background(), bytes.NewReader(data), 1024, nil)
	if err != nil {
		t.Fatalf("ReadDocumentText() error = %v", err)
	}
	if truncated {
		t.Fatal("ReadDocumentText() truncated = true, want false")
	}
	if text != "Hello\tADDP\nSecond line" {
		t.Fatalf("ReadDocumentText() = %q", text)
	}
}

func TestPluginReadDocumentTextIncludesRelatedParts(t *testing.T) {
	plugin := NewPlugin()
	data := minimalDOCXWithFiles(t, map[string]string{
		"word/document.xml":  `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Main body</w:t></w:r></w:p></w:body></w:document>`,
		"word/header2.xml":   `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Header two</w:t></w:r></w:p></w:hdr>`,
		"word/header1.xml":   `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Header one</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml":   `<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Footer one</w:t></w:r></w:p></w:ftr>`,
		"word/footnotes.xml": `<w:footnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:footnote w:id="2"><w:p><w:r><w:t>Footnote text</w:t></w:r></w:p></w:footnote></w:footnotes>`,
		"word/endnotes.xml":  `<w:endnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:endnote w:id="3"><w:p><w:r><w:t>Endnote text</w:t></w:r></w:p></w:endnote></w:endnotes>`,
		"word/comments.xml":  `<w:comments xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:comment w:id="0"><w:p><w:r><w:t>Comment text</w:t></w:r></w:p></w:comment></w:comments>`,
	})

	text, truncated, err := plugin.ReadDocumentText(context.Background(), bytes.NewReader(data), 1024, nil)
	if err != nil {
		t.Fatalf("ReadDocumentText() error = %v", err)
	}
	if truncated {
		t.Fatal("ReadDocumentText() truncated = true, want false")
	}
	want := "Main body\nHeader one\nHeader two\nFooter one\nFootnote text\nEndnote text\nComment text"
	if text != want {
		t.Fatalf("ReadDocumentText() = %q, want %q", text, want)
	}
}

func TestPluginReadDocumentTextTruncates(t *testing.T) {
	plugin := NewPlugin()
	data := minimalDOCX(t, `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>abcdef</w:t></w:r></w:p></w:body></w:document>`)

	text, truncated, err := plugin.ReadDocumentText(context.Background(), bytes.NewReader(data), 3, nil)
	if err != nil {
		t.Fatalf("ReadDocumentText() error = %v", err)
	}
	if !truncated {
		t.Fatal("ReadDocumentText() truncated = false, want true")
	}
	if text != "abc" {
		t.Fatalf("ReadDocumentText() = %q", text)
	}
}

func minimalDOCX(t *testing.T, documentXML string) []byte {
	t.Helper()
	return minimalDOCXWithFiles(t, map[string]string{"word/document.xml": documentXML})
}

func minimalDOCXWithFiles(t *testing.T, files map[string]string) []byte {
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
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
