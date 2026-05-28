package pptx

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
	if descriptor.Format != format.FormatPPTX {
		t.Fatalf("descriptor format = %q, want %q", descriptor.Format, format.FormatPPTX)
	}
	if descriptor.DataType != datatype.DataTypeDocument {
		t.Fatalf("descriptor data type = %q, want document", descriptor.DataType)
	}
	if !descriptor.Providers.DocumentInfo {
		t.Fatalf("pptx should declare document info provider")
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
	data := minimalPPTX(t, map[string]string{
		"ppt/slides/slide1.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Slide</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"docProps/core.xml":     `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>AI4Science</dc:title><dc:language>zh-CN</dc:language></cp:coreProperties>`,
		"docProps/app.xml":      `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Slides>8</Slides><Words>300</Words></Properties>`,
	})

	info, err := plugin.DescribeDocument(context.Background(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeDocument() error = %v", err)
	}
	if info.Title != "AI4Science" || info.Language != "zh-CN" || info.PageCount != 8 || info.WordCount != 300 {
		t.Fatalf("DescribeDocument() = %#v", info)
	}
}

func TestPluginReadDocumentText(t *testing.T) {
	plugin := NewPlugin()
	data := minimalPPTX(t, map[string]string{
		"ppt/slides/slide2.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Second slide</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"ppt/slides/slide1.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>行业赛道</a:t></a:r><a:r><a:t>分析</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
	})

	text, truncated, err := plugin.ReadDocumentText(context.Background(), bytes.NewReader(data), 1024, nil)
	if err != nil {
		t.Fatalf("ReadDocumentText() error = %v", err)
	}
	if truncated {
		t.Fatal("ReadDocumentText() truncated = true, want false")
	}
	if text != "行业赛道分析\nSecond slide" {
		t.Fatalf("ReadDocumentText() = %q", text)
	}
}

func TestPluginReadDocumentTextIncludesNotesAndComments(t *testing.T) {
	plugin := NewPlugin()
	data := minimalPPTX(t, map[string]string{
		"ppt/slides/slide1.xml":             `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Slide one</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"ppt/slides/slide2.xml":             `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Slide two</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"ppt/notesSlides/notesSlide1.xml":   `<p:notes xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Speaker note one</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:notes>`,
		"ppt/comments/comment1.xml":         `<p:cmLst xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cm><p:text>First comment</p:text></p:cm></p:cmLst>`,
		"ppt/commentsAuthors.xml":           `<p:cmAuthorLst xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cmAuthor name="Author"/></p:cmAuthorLst>`,
		"ppt/notesMasters/notesMaster1.xml": `<p:notesMaster xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Ignored notes master</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:notesMaster>`,
	})

	text, truncated, err := plugin.ReadDocumentText(context.Background(), bytes.NewReader(data), 1024, nil)
	if err != nil {
		t.Fatalf("ReadDocumentText() error = %v", err)
	}
	if truncated {
		t.Fatal("ReadDocumentText() truncated = true, want false")
	}
	want := "Slide one\nSpeaker note one\nSlide two\nFirst comment"
	if text != want {
		t.Fatalf("ReadDocumentText() = %q, want %q", text, want)
	}
}

func TestPluginReadDocumentTextTruncates(t *testing.T) {
	plugin := NewPlugin()
	data := minimalPPTX(t, map[string]string{
		"ppt/slides/slide1.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>abcdef</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
	})

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

func minimalPPTX(t *testing.T, files map[string]string) []byte {
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
