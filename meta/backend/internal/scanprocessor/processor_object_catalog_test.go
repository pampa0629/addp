package scanprocessor

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanresource"
)

func TestEnrichObjectStorageJSONTableUpdatesItemDataType(t *testing.T) {
	t.Parallel()

	db := openObjectCatalogProcessorTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	parentNode, err := repo.UpsertNode(1, 7, nil, "bucket", "addp", strPtr("addp"), scanresource.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatalf("create parent node: %v", err)
	}
	resource := scanresource.StorageResource{
		RootName:          "addp",
		Path:              "datasets/converted.json",
		FullPath:          "addp/datasets/converted.json",
		SizeBytes:         64,
		Format:            string(format.FormatJSON),
		EngineCatalogPath: plugin.ObjectItemPath(7, "addp", "datasets/converted.json"),
	}
	item := metaitemForJSONDocument(resource)

	result, err := New(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil))).Process(context.Background(), input{
		Resource:          &commonModels.Engine{ID: 7, EngineType: "static"},
		TenantID:          1,
		EngineID:          7,
		ParentNode:        parentNode,
		ItemType:          "object",
		ItemName:          "converted.json",
		FullName:          resource.FullPath,
		Attributes:        models.JSONMap{},
		Detected:          item,
		ContentReader:     staticObjectContentReader{content: `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`},
		EngineCatalogPath: resource.EngineCatalogPath,
		EngineCatalogPathFor: func(string) plugin.EngineCatalogPath {
			return resource.EngineCatalogPath
		},
		PhysicalPath:       resource.FullPath,
		IndexRootName:      resource.RootName,
		IndexPath:          resource.Path,
		IndexRelativePath:  resource.Path,
		SizeBytes:          resource.SizeBytes,
		ScanDepth:          models.ScannedDepthDeep,
		IncludeAccessIndex: false,
	})
	if err != nil {
		t.Fatalf("Processor.Process() error = %v", err)
	}
	attrs := result.Item.Attributes
	itemAttrs := attrs["item"].(map[string]interface{})
	if itemAttrs["data_type"] != string(datatype.Table) || itemAttrs["format"] != string(format.FormatJSON) {
		t.Fatalf("item attrs = %#v, want json table", itemAttrs)
	}
	typeInfo := attrs["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	if table["fields"] == nil {
		t.Fatalf("type_info.table.fields missing: %#v", table)
	}
}

func TestDocumentTextExtractionWritesExtractionFacts(t *testing.T) {
	t.Parallel()

	resource := textExtractionResource("docs/readme.txt", format.FormatText, 64)
	item := documentDetectedItem(format.FormatText)
	attrs := models.JSONMap{"item": map[string]interface{}{"format": string(format.FormatText)}}

	result := extractCatalogDocumentText(
		context.Background(),
		attrs,
		staticObjectContentReader{content: "hello document search"},
		nil,
		7,
		resource,
		item,
	)

	if result.Text != "hello document search" {
		t.Fatalf("extracted text = %q", result.Text)
	}
	if result.Counts.Documents != 1 || result.Counts.Extracted != 1 {
		t.Fatalf("counts = %#v", result.Counts)
	}
	if !commonJSON.Bool(attrs, "capabilities.extraction", "text_extracted") {
		t.Fatalf("capabilities.extraction = %#v", attrs["capabilities"])
	}
	if got := commonJSON.String(attrs, "capabilities.extraction", "plain_text_preview"); got != "hello document search" {
		t.Fatalf("plain_text_preview = %q", got)
	}
	if commonJSON.Bool(attrs, "type_info.document", "text_extracted") {
		t.Fatalf("type_info.document should not carry extraction status: %#v", attrs["type_info"])
	}
}

func TestDocumentTextExtractionReadsDOCX(t *testing.T) {
	t.Parallel()

	docxContent := minimalObjectCatalogDOCX(t, `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Hello</w:t></w:r><w:r><w:t> DOCX</w:t></w:r></w:p><w:p><w:r><w:t>Search body</w:t></w:r></w:p></w:body></w:document>`)
	resource := textExtractionResource("docs/search.docx", format.FormatDOCX, int64(len(docxContent)))
	item := documentDetectedItem(format.FormatDOCX)
	attrs := models.JSONMap{"item": map[string]interface{}{"format": string(format.FormatDOCX)}}

	result := extractCatalogDocumentText(
		context.Background(),
		attrs,
		staticObjectContentReader{content: string(docxContent)},
		nil,
		7,
		resource,
		item,
	)

	if result.Text != "Hello DOCX\nSearch body" {
		t.Fatalf("extracted text = %q", result.Text)
	}
	if result.Counts.Documents != 1 || result.Counts.Extracted != 1 {
		t.Fatalf("counts = %#v", result.Counts)
	}
	if got := commonJSON.String(attrs, "capabilities.extraction", "extractor"); got != "common_format:docx" {
		t.Fatalf("extractor = %q", got)
	}
	if got := commonJSON.String(attrs, "capabilities.extraction", "plain_text_preview"); got != "Hello DOCX\nSearch body" {
		t.Fatalf("plain_text_preview = %q", got)
	}
	if commonJSON.Bool(attrs, "capabilities.extraction", "text_truncated") {
		t.Fatalf("text_truncated = true, want false")
	}
	if commonJSON.Bool(attrs, "type_info.document", "text_extracted") {
		t.Fatalf("type_info.document should not carry extraction status: %#v", attrs["type_info"])
	}
}

func TestDocumentTextExtractionReadsPPTX(t *testing.T) {
	t.Parallel()

	pptxContent := minimalObjectCatalogPPTX(t, map[string]string{
		"ppt/slides/slide1.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>行业赛道</a:t></a:r><a:r><a:t>分析</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"ppt/slides/slide2.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>第二页</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
	})
	resource := textExtractionResource("docs/search.pptx", format.FormatPPTX, int64(len(pptxContent)))
	item := documentDetectedItem(format.FormatPPTX)
	attrs := models.JSONMap{"item": map[string]interface{}{"format": string(format.FormatPPTX)}}

	result := extractCatalogDocumentText(
		context.Background(),
		attrs,
		staticObjectContentReader{content: string(pptxContent)},
		nil,
		7,
		resource,
		item,
	)

	if result.Text != "行业赛道分析\n第二页" {
		t.Fatalf("extracted text = %q", result.Text)
	}
	if result.Counts.Documents != 1 || result.Counts.Extracted != 1 {
		t.Fatalf("counts = %#v", result.Counts)
	}
	if got := commonJSON.String(attrs, "capabilities.extraction", "extractor"); got != "common_format:pptx" {
		t.Fatalf("extractor = %q", got)
	}
	if got := commonJSON.String(attrs, "capabilities.extraction", "status"); got != "completed" {
		t.Fatalf("status = %q", got)
	}
	if commonJSON.Bool(attrs, "type_info.document", "text_extracted") {
		t.Fatalf("type_info.document should not carry extraction status: %#v", attrs["type_info"])
	}
}

func TestDocumentTextExtractionMarksUnsupportedWithoutReader(t *testing.T) {
	t.Parallel()

	resource := textExtractionResource("docs/raw.wps", format.FormatWPS, 16)
	item := documentDetectedItem(format.FormatWPS)
	attrs := models.JSONMap{"item": map[string]interface{}{"format": string(format.FormatWPS)}}

	result := extractCatalogDocumentText(
		context.Background(),
		attrs,
		staticObjectContentReader{content: "binary content"},
		nil,
		7,
		resource,
		item,
	)

	if result.Text != "" {
		t.Fatalf("extracted text = %q, want empty", result.Text)
	}
	if result.Counts.Documents != 1 || result.Counts.Unsupported != 1 {
		t.Fatalf("counts = %#v", result.Counts)
	}
	if commonJSON.Bool(attrs, "capabilities.extraction", "extractor_available") {
		t.Fatalf("extractor_available = true, want false")
	}
	if commonJSON.Bool(attrs, "capabilities.extraction", "text_extracted") {
		t.Fatalf("text_extracted = true, want false")
	}
	if got := commonJSON.String(attrs, "capabilities.extraction", "status"); got != "unsupported" {
		t.Fatalf("status = %q", got)
	}
	if got := commonJSON.String(attrs, "capabilities.extraction", "reason"); got != "document_text_reader_unavailable" {
		t.Fatalf("reason = %q", got)
	}
	if commonJSON.Bool(attrs, "type_info.document", "text_extracted") {
		t.Fatalf("type_info.document should not carry extraction status: %#v", attrs["type_info"])
	}
}

func TestContentSHA256WritesStorageContentHash(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	hash, err := computeContentSHA256(
		context.Background(),
		staticObjectContentReader{content: "binary content"},
		nil,
		plugin.ObjectItemPath(7, "addp", "docs/raw.wps"),
	)
	if err != nil {
		t.Fatalf("computeContentSHA256() error = %v", err)
	}
	setStorageContentHash(attrs, hash)

	if got := commonJSON.String(attrs, "storage", "content_hash"); got != "93a0b24644f2e0fd11d6b422c90275c482b0cc20be4a4e3f62148ed2932b4792" {
		t.Fatalf("storage.content_hash = %q", got)
	}
	if got := commonJSON.String(attrs, "storage", "content_hash_algorithm"); got != "sha256" {
		t.Fatalf("storage.content_hash_algorithm = %q", got)
	}
}

func textExtractionResource(path string, formatName format.FormatType, sizeBytes int64) scanresource.StorageResource {
	return scanresource.StorageResource{
		RootName:          "addp",
		Path:              path,
		FullPath:          "addp/" + path,
		SizeBytes:         sizeBytes,
		Format:            string(formatName),
		EngineCatalogPath: plugin.ObjectItemPath(7, "addp", path),
	}
}

func documentDetectedItem(formatName format.FormatType) *metaitem.DetectedItem {
	return &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			DataType: datatype.Document,
			Format:   string(formatName),
		},
	}
}

func metaitemForJSONDocument(resource scanresource.StorageResource) *metaitem.DetectedItem {
	return scanresource.InferObjectDataItem(resource, "converted.json")
}
