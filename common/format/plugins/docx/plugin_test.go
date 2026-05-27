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
	if descriptor.Providers.DocumentInfo {
		t.Fatalf("docx should not declare document info provider before backend parsing is defined")
	}
	if !contains(descriptor.ContentReaders, string(format.ContentReaderDocumentText)) {
		t.Fatalf("content readers = %#v, want document text", descriptor.ContentReaders)
	}
	if !contains(descriptor.ContentReaders, string(format.ContentReaderRawContent)) ||
		!contains(descriptor.ContentReaders, string(format.ContentReaderRangeContent)) {
		t.Fatalf("content readers = %#v, want raw and range", descriptor.ContentReaders)
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
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	file, err := writer.Create("word/document.xml")
	if err != nil {
		t.Fatalf("create document.xml: %v", err)
	}
	if _, err := file.Write([]byte(documentXML)); err != nil {
		t.Fatalf("write document.xml: %v", err)
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
