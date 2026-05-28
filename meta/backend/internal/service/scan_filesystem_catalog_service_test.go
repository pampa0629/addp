package service

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

func TestEnsureFilesystemScanRootUsesDirectoryNodeForNonRootPath(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := &FilesystemCatalogScanService{
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		repo: repo,
	}

	rootNode, scanNode, err := svc.ensureFilesystemScanRoot(1, 26, "", "shp")
	if err != nil {
		t.Fatalf("ensureFilesystemScanRoot() error = %v", err)
	}
	if rootNode.Name != "/" || rootNode.FullName != "" {
		t.Fatalf("root node name/fullName = %q/%q, want '/' and empty full_name", rootNode.Name, rootNode.FullName)
	}
	if rootNode.ID == scanNode.ID {
		t.Fatal("scan path shp should use shp directory node, not filesystem root node")
	}
	if scanNode.ParentNodeID == nil || *scanNode.ParentNodeID != rootNode.ID {
		t.Fatalf("scan node parent = %#v, want root id %d", scanNode.ParentNodeID, rootNode.ID)
	}
	if scanNode.NodeType != "dir" || scanNode.Name != "shp" || scanNode.FullName != "shp" {
		t.Fatalf("scan node = %#v, want dir shp with full_name shp", scanNode)
	}

	item, err := repo.UpsertItemWithDepth(
		1,
		26,
		scanNode,
		"file",
		"a3.shp",
		"shp/a3.shp",
		models.JSONMap{},
		nil,
		nil,
		nil,
		models.ScannedDepthDeep,
	)
	if err != nil {
		t.Fatalf("UpsertItemWithDepth() error = %v", err)
	}
	if item.NodeID != scanNode.ID {
		t.Fatalf("item node_id = %d, want scan node %d", item.NodeID, scanNode.ID)
	}
}

func TestFilesystemDeepScanExtractsDOCXHeaderFooter(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := &FilesystemCatalogScanService{
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		repo: repo,
	}
	parentNode, err := repo.UpsertNode(1, 26, nil, "dir", "doc", strPtr("doc"), models.JSONMap{"path": "doc"})
	if err != nil {
		t.Fatalf("create root node: %v", err)
	}
	docx := minimalObjectCatalogDOCXWithParts(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>正文</w:t></w:r></w:p></w:body></w:document>`,
		"word/header1.xml":  `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>页眉1</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml":  `<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>注脚1</w:t></w:r></w:p></w:ftr>`,
		"docProps/core.xml": `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>ChatBI详细设计</dc:title><dc:language>zh-CN</dc:language></cp:coreProperties>`,
		"docProps/app.xml":  `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Pages>7</Pages><Words>1200</Words></Properties>`,
	})
	provider := filesystemScanTestProvider{
		files: []plugin.CatalogNode{{
			Name:   "时空数据中台产品详细设计V3.0.0.0（ChatBI部分）.docx",
			Path:   plugin.FileItemPath(26, "doc/时空数据中台产品详细设计V3.0.0.0（ChatBI部分）.docx"),
			Term:   plugin.CatalogTermFile,
			Kind:   plugin.CatalogKindFile,
			IsItem: true,
			Stats:  map[string]interface{}{"size_bytes": int64(len(docx))},
			Attributes: map[string]interface{}{
				"path": "doc/时空数据中台产品详细设计V3.0.0.0（ChatBI部分）.docx",
			},
		}},
		content: string(docx),
	}
	resource := &commonModels.Engine{ID: 26, EngineType: provider.Type()}

	items, extraction, err := svc.scanDirectory(context.Background(), provider, provider, nil, resource, 1, "doc", parentNode, false, plugin.CatalogTermFile, models.ScannedDepthDeep, true)
	if err != nil {
		t.Fatalf("scanDirectory() error = %v", err)
	}
	if items != 1 {
		t.Fatalf("items = %d, want 1", items)
	}
	if extraction.Documents != 1 || extraction.Extracted != 1 {
		t.Fatalf("extraction = %#v, want one extracted document", extraction)
	}
	item, ok, err := repo.FindItemByFullName(1, 26, "doc/时空数据中台产品详细设计V3.0.0.0（ChatBI部分）.docx")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok {
		t.Fatal("scanned file item not found")
	}
	preview := commonJSON.String(item.Attributes, "capabilities.extraction", "plain_text_preview")
	if !strings.Contains(preview, "页眉1") || !strings.Contains(preview, "注脚1") {
		t.Fatalf("plain_text_preview = %q, want header and footer text", preview)
	}
	if got := commonJSON.String(item.Attributes, "type_info.document", "title"); got != "ChatBI详细设计" {
		t.Fatalf("type_info.document.title = %q", got)
	}
	if got := commonJSON.Int64(item.Attributes, "type_info.document", "page_count"); got != 7 {
		t.Fatalf("type_info.document.page_count = %d", got)
	}
	if got := commonJSON.String(item.Attributes, "storage", "content_hash_algorithm"); got != "sha256" {
		t.Fatalf("storage.content_hash_algorithm = %q, want sha256", got)
	}
}

type filesystemScanTestProvider struct {
	files   []plugin.CatalogNode
	content string
}

func (p filesystemScanTestProvider) Type() string         { return "filesystem-scan-test" }
func (p filesystemScanTestProvider) DisplayName() string  { return "filesystem scan test" }
func (p filesystemScanTestProvider) EngineOrigin() string { return "general" }
func (p filesystemScanTestProvider) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p filesystemScanTestProvider) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (p filesystemScanTestProvider) DefaultPort() int                                   { return 0 }
func (p filesystemScanTestProvider) RequiredFields() []string                           { return nil }
func (p filesystemScanTestProvider) SensitiveFields() []string                          { return nil }
func (p filesystemScanTestProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p filesystemScanTestProvider) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p filesystemScanTestProvider) ListChildren(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return p.files, nil
}
func (p filesystemScanTestProvider) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return nil, nil
}
func (p filesystemScanTestProvider) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(p.content)), nil
}

func minimalObjectCatalogDOCXWithParts(t *testing.T, files map[string]string) []byte {
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
