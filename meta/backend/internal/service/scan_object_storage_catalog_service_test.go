package service

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnrichObjectStorageJSONTableUpdatesItemDataType(t *testing.T) {
	t.Parallel()

	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	parentNode, err := repo.UpsertNode(1, 7, nil, "bucket", "addp", strPtr("addp"), metacatalog.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatalf("create parent node: %v", err)
	}
	resource := metacatalog.StorageResource{
		RootName:    "addp",
		Path:        "datasets/converted.json",
		FullPath:    "addp/datasets/converted.json",
		SizeBytes:   64,
		Format:      string(format.FormatJSON),
		CatalogPath: plugin.ObjectItemPath(7, "addp", "datasets/converted.json"),
	}
	item := metaitemForJSONDocument(resource)

	result, err := catalogDataItemProcessor(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil))).Process(context.Background(), catalogSingleItemInput{
		Resource:      &commonModels.Engine{ID: 7, EngineType: "static"},
		TenantID:      1,
		EngineID:      7,
		ParentNode:    parentNode,
		ItemType:      "object",
		ItemName:      "converted.json",
		FullName:      resource.FullPath,
		Attributes:    models.JSONMap{},
		Detected:      item,
		ContentReader: staticObjectContentReader{content: `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`},
		CatalogPath:   resource.CatalogPath,
		CatalogPathFor: func(string) plugin.CatalogPath {
			return resource.CatalogPath
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
		t.Fatalf("catalogSingleItemProcessor.Process() error = %v", err)
	}
	attrs := result.Item.Attributes
	itemAttrs := attrs["item"].(map[string]interface{})
	if itemAttrs["data_type"] != string(dataitem.DataTypeTable) || itemAttrs["format"] != string(format.FormatJSON) {
		t.Fatalf("item attrs = %#v, want json table", itemAttrs)
	}
	typeInfo := attrs["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	if table["fields"] == nil {
		t.Fatalf("type_info.table.fields missing: %#v", table)
	}
}

func TestDetectObjectCatalogResourceFormatUsesCommonFormatSniffing(t *testing.T) {
	t.Parallel()

	resource := metacatalog.StorageResource{
		RootName:    "addp",
		Path:        "datasets/lake3",
		FullPath:    "addp/datasets/lake3",
		NodeType:    "object",
		SizeBytes:   8,
		CatalogPath: plugin.ObjectItemPath(7, "addp", "datasets/lake3"),
	}

	detected, err := detectObjectCatalogResourceFormat(
		context.Background(),
		staticObjectContentReader{content: "PAR1\x15\x04\x15\x00"},
		nil,
		resource,
	)
	if err != nil {
		t.Fatalf("detectObjectCatalogResourceFormat() error = %v", err)
	}
	if detected != string(format.FormatParquet) {
		t.Fatalf("detected format = %q, want parquet", detected)
	}
}

func TestDetectObjectCatalogResourceFormatPromotesUnknownText(t *testing.T) {
	t.Parallel()

	resource := metacatalog.StorageResource{
		RootName:    "addp",
		Path:        "docs/README",
		FullPath:    "addp/docs/README",
		NodeType:    "object",
		SizeBytes:   12,
		CatalogPath: plugin.ObjectItemPath(7, "addp", "docs/README"),
	}

	detected, err := detectObjectCatalogResourceFormat(
		context.Background(),
		staticObjectContentReader{content: "hello\nworld\n"},
		nil,
		resource,
	)
	if err != nil {
		t.Fatalf("detectObjectCatalogResourceFormat() error = %v", err)
	}
	if detected != string(format.FormatText) {
		t.Fatalf("detected format = %q, want text", detected)
	}
}

func TestDetectObjectCatalogResourceFormatKeepsUnknownBinary(t *testing.T) {
	t.Parallel()

	resource := metacatalog.StorageResource{
		RootName:    "addp",
		Path:        "docs/blob.binx",
		FullPath:    "addp/docs/blob.binx",
		NodeType:    "object",
		SizeBytes:   3,
		CatalogPath: plugin.ObjectItemPath(7, "addp", "docs/blob.binx"),
	}

	detected, err := detectObjectCatalogResourceFormat(
		context.Background(),
		staticObjectContentReader{content: string([]byte{0x00, 0x01, 0x02})},
		nil,
		resource,
	)
	if err != nil {
		t.Fatalf("detectObjectCatalogResourceFormat() error = %v", err)
	}
	if detected != "" {
		t.Fatalf("detected format = %q, want empty unknown", detected)
	}
}

func TestExtractCatalogDocumentTextWritesExtractionFacts(t *testing.T) {
	t.Parallel()

	resource := metacatalog.StorageResource{
		RootName:    "addp",
		Path:        "docs/readme.txt",
		FullPath:    "addp/docs/readme.txt",
		SizeBytes:   64,
		Format:      string(format.FormatText),
		CatalogPath: plugin.ObjectItemPath(7, "addp", "docs/readme.txt"),
	}
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			DataType: datatype.DataTypeDocument,
			Format:   string(format.FormatText),
		},
	}
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

func TestExtractCatalogDocumentTextReadsDOCX(t *testing.T) {
	t.Parallel()

	docxContent := minimalObjectCatalogDOCX(t, `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Hello</w:t></w:r><w:r><w:t> DOCX</w:t></w:r></w:p><w:p><w:r><w:t>Search body</w:t></w:r></w:p></w:body></w:document>`)
	resource := metacatalog.StorageResource{
		RootName:    "addp",
		Path:        "docs/search.docx",
		FullPath:    "addp/docs/search.docx",
		SizeBytes:   int64(len(docxContent)),
		Format:      string(format.FormatDOCX),
		CatalogPath: plugin.ObjectItemPath(7, "addp", "docs/search.docx"),
	}
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			DataType: datatype.DataTypeDocument,
			Format:   string(format.FormatDOCX),
		},
	}
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
	if !commonJSON.Bool(attrs, "capabilities.extraction", "text_extracted") {
		t.Fatalf("capabilities.extraction = %#v", attrs["capabilities"])
	}
	if got := commonJSON.String(attrs, "capabilities.extraction", "extractor"); got != "common_format:docx" {
		t.Fatalf("extractor = %q", got)
	}
	if got := commonJSON.String(attrs, "capabilities.extraction", "plain_text_preview"); got != "Hello DOCX\nSearch body" {
		t.Fatalf("plain_text_preview = %q", got)
	}
	if got := commonJSON.String(attrs, "capabilities.extraction", "index_ref"); !strings.HasPrefix(got, "meilisearch:assets:") {
		t.Fatalf("index_ref = %q", got)
	}
	if commonJSON.Bool(attrs, "capabilities.extraction", "text_truncated") {
		t.Fatalf("text_truncated = true, want false")
	}
	if commonJSON.Bool(attrs, "type_info.document", "text_extracted") {
		t.Fatalf("type_info.document should not carry extraction status: %#v", attrs["type_info"])
	}
}

func TestExtractCatalogDocumentTextReadsPPTX(t *testing.T) {
	t.Parallel()

	pptxContent := minimalObjectCatalogPPTX(t, map[string]string{
		"ppt/slides/slide1.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>行业赛道</a:t></a:r><a:r><a:t>分析</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"ppt/slides/slide2.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>第二页</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
	})
	resource := metacatalog.StorageResource{
		RootName:    "addp",
		Path:        "docs/search.pptx",
		FullPath:    "addp/docs/search.pptx",
		SizeBytes:   int64(len(pptxContent)),
		Format:      string(format.FormatPPTX),
		CatalogPath: plugin.ObjectItemPath(7, "addp", "docs/search.pptx"),
	}
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			DataType: datatype.DataTypeDocument,
			Format:   string(format.FormatPPTX),
		},
	}
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

func TestExtractCatalogDocumentTextMarksUnsupportedWithoutReader(t *testing.T) {
	t.Parallel()

	resource := metacatalog.StorageResource{
		RootName:    "addp",
		Path:        "docs/raw.wps",
		FullPath:    "addp/docs/raw.wps",
		SizeBytes:   16,
		Format:      string(format.FormatWPS),
		CatalogPath: plugin.ObjectItemPath(7, "addp", "docs/raw.wps"),
	}
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			DataType: datatype.DataTypeDocument,
			Format:   string(format.FormatWPS),
		},
	}
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

func TestComputeContentSHA256WritesStorageContentHash(t *testing.T) {
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

func TestEnsureObjectCatalogPrefixNodesUsesCompositeItemParentPath(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := &ObjectStorageCatalogScanService{
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		repo: repo,
	}

	bucketNode, err := repo.UpsertNode(1, 9, nil, "bucket", "addp", strPtr("addp"), metacatalog.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatalf("create bucket node: %v", err)
	}

	stats := map[uint]*objectCatalogNodeAggregate{}
	parentNode, err := svc.ensureObjectCatalogPrefixNodes(1, 9, bucketNode, bucketNode, "gis/", "", stats)
	if err != nil {
		t.Fatalf("ensure prefix nodes: %v", err)
	}

	if parentNode.ID == bucketNode.ID {
		t.Fatal("composite item under addp/gis should attach to gis prefix, not bucket scope")
	}
	if parentNode.NodeType != "prefix" || parentNode.Name != "gis" || parentNode.FullName != "addp/gis" {
		t.Fatalf("parent node = %#v, want addp/gis prefix", parentNode)
	}
	if _, ok := stats[parentNode.ID]; !ok {
		t.Fatalf("gis prefix aggregate was not initialized")
	}
}

func TestResolveObjectCatalogTargetDistinguishesObjectAndPrefix(t *testing.T) {
	t.Parallel()

	sizeBytes := int64(100)
	provider := objectScanTargetProvider{
		items: map[string]plugin.CatalogEntry{
			"addp/contain/shapefile.zip": {
				Name: "shapefile.zip",
				Path: plugin.ObjectItemPath(9, "addp", "contain/shapefile.zip"),
				Kind: plugin.CatalogKindObject,
				Term: plugin.CatalogTermObject,
				Role: plugin.CatalogRoleLeaf,
				Storage: &plugin.CatalogStorageFacts{
					Path:      "addp/contain/shapefile.zip",
					SizeBytes: &sizeBytes,
				},
			},
		},
	}
	resource := &commonModels.Engine{ID: 9, EngineType: provider.Type()}

	objectTarget, err := resolveObjectCatalogTarget(context.Background(), resource, provider, "addp/contain/shapefile.zip")
	if err != nil {
		t.Fatalf("resolve object target: %v", err)
	}
	if objectTarget.Bucket != "addp" || objectTarget.Object != "contain/shapefile.zip" || objectTarget.Prefix != "" {
		t.Fatalf("object target = %#v, want exact object", objectTarget)
	}

	prefixTarget, err := resolveObjectCatalogTarget(context.Background(), resource, provider, "addp/contain")
	if err != nil {
		t.Fatalf("resolve prefix target: %v", err)
	}
	if prefixTarget.Bucket != "addp" || prefixTarget.Prefix != "contain" || prefixTarget.Object != "" {
		t.Fatalf("prefix target = %#v, want prefix", prefixTarget)
	}
}

func TestObjectCatalogScanFinalizesCatalogRoot(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewScanService(db, nil)
	svc.repo = repo
	svc.objectStorageCatalogScanService = NewObjectStorageCatalogScanService(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	svc.log = slog.New(slog.NewTextHandler(io.Discard, nil))

	sizeBytes := int64(12)
	provider := objectScanTargetProvider{
		items: map[string]plugin.CatalogEntry{
			"addp/test2.csv": {
				Name: "test2.csv",
				Path: plugin.ObjectItemPath(9, "addp", "test2.csv"),
				Kind: plugin.CatalogKindObject,
				Term: plugin.CatalogTermObject,
				Role: plugin.CatalogRoleLeaf,
				Storage: &plugin.CatalogStorageFacts{
					Path:      "addp/test2.csv",
					SizeBytes: &sizeBytes,
				},
			},
		},
		listChildren: map[string][]plugin.CatalogEntry{
			plugin.ObjectRootPath(9).StringPath(): {
				{
					Name: "addp",
					Path: plugin.ObjectDirectoryPath(9, "addp", ""),
					Kind: plugin.CatalogKindBucket,
					Term: plugin.CatalogTermBucket,
					Role: plugin.CatalogRoleBranch,
				},
			},
			plugin.ObjectDirectoryPath(9, "addp", "").StringPath(): {
				{
					Name: "test2.csv",
					Path: plugin.ObjectItemPath(9, "addp", "test2.csv"),
					Kind: plugin.CatalogKindObject,
					Term: plugin.CatalogTermObject,
					Role: plugin.CatalogRoleLeaf,
					Storage: &plugin.CatalogStorageFacts{
						Path:      "addp/test2.csv",
						SizeBytes: &sizeBytes,
					},
				},
			},
		},
	}
	plugin.Register(provider)
	t.Cleanup(func() {
		plugin.Unregister(provider.Type())
	})

	resource := &commonModels.Engine{ID: 9, Name: "Business MinIO", EngineType: provider.Type()}
	result, err := svc.scanObjectStorageCatalogResourceResultWithReporter(resource, 1, nil, models.ScannedDepthDeep, true, nil)
	if err != nil {
		t.Fatalf("scan object catalog: %v", err)
	}
	if result.Items != 1 {
		t.Fatalf("result.Items = %d, want 1", result.Items)
	}

	var root models.MetaNode
	if err := db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NULL", 1, 9).First(&root).Error; err != nil {
		t.Fatalf("query root node: %v", err)
	}
	if root.ScanStatus != "completed" || root.ScannedDepth != models.ScannedDepthDeep {
		t.Fatalf("root scan status/depth = %q/%q, want completed/deep", root.ScanStatus, root.ScannedDepth)
	}
	if root.ItemCount != 1 {
		t.Fatalf("root item_count = %d, want 1", root.ItemCount)
	}
}

func metaitemForJSONDocument(resource metacatalog.StorageResource) *metaitem.DetectedItem {
	return metacatalog.InferObjectCatalogDataItem(resource, "converted.json")
}

func openObjectCatalogScanTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS metadata").Error; err != nil {
		t.Fatalf("attach metadata schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE metadata.meta_node (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			engine_id INTEGER NOT NULL,
			parent_node_id INTEGER,
			node_type TEXT NOT NULL,
			name TEXT NOT NULL,
			depth INTEGER NOT NULL,
			path TEXT,
			full_name TEXT,
			scan_status TEXT,
			scanned_depth TEXT,
			scanned_at DATETIME,
			scan_error TEXT,
			item_count INTEGER,
			total_size_bytes INTEGER,
			attributes JSON,
			created_at DATETIME,
			deleted_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create meta_node table: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE metadata.meta_item (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			engine_id INTEGER NOT NULL,
			node_id INTEGER NOT NULL,
			item_type TEXT NOT NULL,
			name TEXT NOT NULL,
			full_name TEXT,
			fingerprint TEXT NOT NULL,
			row_count INTEGER,
			size_bytes INTEGER,
			data_updated_at DATETIME,
			scanned_at DATETIME,
			scanned_depth TEXT,
			attributes JSON,
			created_at DATETIME,
			deleted_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create meta_item table: %v", err)
	}
	return db
}

func strPtr(s string) *string {
	return &s
}

func minimalObjectCatalogDOCX(t *testing.T, documentXML string) []byte {
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

func minimalObjectCatalogPPTX(t *testing.T, files map[string]string) []byte {
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

type staticObjectContentReader struct {
	content string
}

func (r staticObjectContentReader) Type() string         { return "static" }
func (r staticObjectContentReader) DisplayName() string  { return "static" }
func (r staticObjectContentReader) EngineOrigin() string { return "general" }
func (r staticObjectContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r staticObjectContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r staticObjectContentReader) DefaultPort() int                                   { return 0 }
func (r staticObjectContentReader) RequiredFields() []string                           { return nil }
func (r staticObjectContentReader) SensitiveFields() []string                          { return nil }
func (r staticObjectContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r staticObjectContentReader) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (r staticObjectContentReader) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(r.content)), nil
}

type objectScanTargetProvider struct {
	items        map[string]plugin.CatalogEntry
	listChildren map[string][]plugin.CatalogEntry
}

func (p objectScanTargetProvider) Type() string         { return "object-scan-target-test" }
func (p objectScanTargetProvider) DisplayName() string  { return "object scan target test" }
func (p objectScanTargetProvider) EngineOrigin() string { return "general" }
func (p objectScanTargetProvider) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p objectScanTargetProvider) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (p objectScanTargetProvider) DefaultPort() int                                   { return 0 }
func (p objectScanTargetProvider) RequiredFields() []string                           { return nil }
func (p objectScanTargetProvider) SensitiveFields() []string                          { return nil }
func (p objectScanTargetProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.NewObjectCapabilities(p.Type())
}
func (p objectScanTargetProvider) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p objectScanTargetProvider) CatalogModel() plugin.CatalogModelSpec {
	return plugin.ObjectCatalogModel()
}
func (p objectScanTargetProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	nodes := p.listChildren[path.StringPath()]
	return nodes, nil
}
func (p objectScanTargetProvider) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	node, ok := p.items[path.StringPath()]
	if !ok {
		return nil, nil
	}
	return &node, nil
}
