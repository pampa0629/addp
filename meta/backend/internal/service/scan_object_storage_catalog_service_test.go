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

	resource := metacatalog.StorageResource{
		RootName:  "addp",
		Path:      "datasets/converted.json",
		FullPath:  "addp/datasets/converted.json",
		SizeBytes: 64,
		Format:    string(format.FormatJSON),
	}
	item := metaitemForJSONDocument(resource)

	attrs := models.JSONMap{}
	err := enrichObjectCatalogSingleResourceAttributes(
		context.Background(),
		attrs,
		staticObjectContentReader{content: `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`},
		nil,
		7,
		resource,
		item,
		false,
	)
	if err != nil {
		t.Fatalf("enrichObjectCatalogSingleResourceAttributes() error = %v", err)
	}
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

func TestExtractObjectCatalogDocumentTextWritesExtractionFacts(t *testing.T) {
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

	text := extractObjectCatalogDocumentText(
		context.Background(),
		attrs,
		staticObjectContentReader{content: "hello document search"},
		nil,
		7,
		resource,
		item,
	)

	if text != "hello document search" {
		t.Fatalf("extracted text = %q", text)
	}
	if !commonJSON.Bool(attrs, "capabilities.extraction", "text_extracted") {
		t.Fatalf("capabilities.extraction = %#v", attrs["capabilities"])
	}
	if got := commonJSON.String(attrs, "capabilities.extraction", "plain_text_preview"); got != "hello document search" {
		t.Fatalf("plain_text_preview = %q", got)
	}
	if !commonJSON.Bool(attrs, "type_info.document", "text_extracted") {
		t.Fatalf("type_info.document = %#v", attrs["type_info"])
	}
}

func TestExtractObjectCatalogDocumentTextReadsDOCX(t *testing.T) {
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

	text := extractObjectCatalogDocumentText(
		context.Background(),
		attrs,
		staticObjectContentReader{content: string(docxContent)},
		nil,
		7,
		resource,
		item,
	)

	if text != "Hello DOCX\nSearch body" {
		t.Fatalf("extracted text = %q", text)
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
	if !commonJSON.Bool(attrs, "type_info.document", "text_extracted") {
		t.Fatalf("type_info.document = %#v", attrs["type_info"])
	}
}

func TestEnsureObjectCatalogPrefixNodesUsesCompositeItemParentPath(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := &ObjectStorageCatalogScanService{
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		repo: repo,
	}

	bucketNode, err := repo.UpsertNode(1, 9, nil, "bucket", "addp", strPtr("addp"), models.JSONMap{"bucket": "addp"})
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

	provider := objectScanTargetProvider{
		items: map[string]plugin.CatalogNode{
			"addp/contain/shapefile.zip": {
				Name:   "shapefile.zip",
				Path:   plugin.ObjectItemPath(9, "addp", "contain/shapefile.zip"),
				Kind:   plugin.CatalogKindObject,
				Term:   plugin.CatalogTermObject,
				IsItem: true,
				Stats:  map[string]interface{}{"size_bytes": int64(100)},
				Attributes: map[string]interface{}{
					"path": "addp/contain/shapefile.zip",
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
	items map[string]plugin.CatalogNode
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
	return plugin.EngineCapabilities{}
}
func (p objectScanTargetProvider) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p objectScanTargetProvider) ListChildren(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return nil, nil
}
func (p objectScanTargetProvider) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogNode, error) {
	node, ok := p.items[path.StringPath()]
	if !ok {
		return nil, nil
	}
	return &node, nil
}
