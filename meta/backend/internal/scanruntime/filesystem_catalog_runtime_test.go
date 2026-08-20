package scanruntime

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"gorm.io/gorm"
)

func TestEnsureFilesystemScanRootUsesDirectoryNodeForNonRootPath(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)

	resource := &commonModels.Engine{ID: 26, Name: "NFS Demo", EngineType: "nfs"}
	rootNode, scanNode, err := svc.ensureFilesystemScanRoot(1, resource, filesystemScanTestProvider{}, "shp")
	if err != nil {
		t.Fatalf("ensureFilesystemScanRoot() error = %v", err)
	}
	if rootNode.Name != "NFS Demo" || rootNode.FullName != "" {
		t.Fatalf("root node name/fullName = %q/%q, want engine name and empty full_name", rootNode.Name, rootNode.FullName)
	}
	if rootNode.Attributes["schema_version"] == nil {
		t.Fatalf("root schema_version missing: %#v", rootNode.Attributes)
	}
	catalogAttrs, ok := rootNode.Attributes["catalog"].(map[string]interface{})
	if !ok {
		t.Fatalf("root catalog attributes = %#v", rootNode.Attributes)
	}
	if catalogAttrs["root_term"] != plugin.CatalogTermRoot || catalogAttrs["native_name"] != "/" {
		t.Fatalf("root catalog attributes = %#v, want root_term=root and native_name=/", catalogAttrs)
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

func TestFilesystemScanRootDoesNotPromoteRootFilesToNodes(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	sizeBytes := int64(12)
	provider := filesystemScanTestProvider{
		entriesByPath: map[string][]plugin.CatalogEntry{
			"": {
				{
					Name: "docs",
					Path: plugin.FileDirectoryPath(26, "docs"),
					Term: plugin.CatalogTermDirectory,
					Kind: plugin.CatalogKindDirectory,
					Role: plugin.CatalogRoleBranch,
					Storage: &plugin.CatalogStorageFacts{
						Path: "docs",
					},
				},
				{
					Name: "README.md",
					Path: plugin.FileItemPath(26, "README.md"),
					Term: plugin.CatalogTermFile,
					Kind: plugin.CatalogKindFile,
					Role: plugin.CatalogRoleLeaf,
					Storage: &plugin.CatalogStorageFacts{
						Path:      "README.md",
						SizeBytes: &sizeBytes,
					},
				},
			},
			"docs": nil,
		},
		content: "hello world\n",
	}
	plugin.Register(provider)
	t.Cleanup(func() {
		plugin.Unregister(provider.Type())
	})
	resource := &commonModels.Engine{ID: 26, Name: "Business NFS", EngineType: provider.Type()}

	result, err := svc.ScanPaths(context.Background(), resource, 1, nil, models.ScannedDepthBasic, true, nil)
	if err != nil {
		t.Fatalf("ScanPaths() error = %v", err)
	}
	if result.CatalogNodes != 1 || result.Items != 1 {
		t.Fatalf("roots/items = %d/%d, want 1/1", result.CatalogNodes, result.Items)
	}
	var readmeNode models.MetaNode
	if err := db.Where("tenant_id = ? AND engine_id = ? AND name = ?", 1, 26, "README.md").First(&readmeNode).Error; err == nil {
		t.Fatalf("README.md was promoted to meta_node: %#v", readmeNode)
	} else if err != gorm.ErrRecordNotFound {
		t.Fatalf("query README node: %v", err)
	}
	item, ok, err := repo.FindItemByFullName(1, 26, "README.md")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok {
		t.Fatal("README.md meta_item not found")
	}
	var rootNode models.MetaNode
	if err := db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NULL", 1, 26).First(&rootNode).Error; err != nil {
		t.Fatalf("query root node: %v", err)
	}
	if item.NodeID != rootNode.ID {
		t.Fatalf("README node_id = %d, want root node %d", item.NodeID, rootNode.ID)
	}
	if rootNode.ScanStatus != "completed" || rootNode.ScannedDepth != models.ScannedDepthBasic {
		t.Fatalf("root scan status/depth = %q/%q, want completed/basic", rootNode.ScanStatus, rootNode.ScannedDepth)
	}
}

func TestFilesystemScanIgnoresSystemFiles(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	root, err := repo.UpsertNode(1, 26, nil, plugin.CatalogTermRoot, "Business NFS", strPtr(""), models.JSONMap{})
	if err != nil {
		t.Fatalf("create root node: %v", err)
	}
	if _, err := repo.UpsertItemWithDepth(1, 26, root, plugin.CatalogTermFile, ".DS_Store", ".DS_Store", models.JSONMap{}, nil, nil, nil, models.ScannedDepthDeep); err != nil {
		t.Fatalf("create old .DS_Store item: %v", err)
	}
	sizeBytes := int64(12)
	systemSizeBytes := int64(1)
	provider := filesystemScanTestProvider{
		entriesByPath: map[string][]plugin.CatalogEntry{
			"": {
				{
					Name: ".DS_Store",
					Path: plugin.FileItemPath(26, ".DS_Store"),
					Term: plugin.CatalogTermFile,
					Kind: plugin.CatalogKindFile,
					Role: plugin.CatalogRoleLeaf,
					Storage: &plugin.CatalogStorageFacts{
						Path:      ".DS_Store",
						SizeBytes: &systemSizeBytes,
					},
				},
				{
					Name: "README.md",
					Path: plugin.FileItemPath(26, "README.md"),
					Term: plugin.CatalogTermFile,
					Kind: plugin.CatalogKindFile,
					Role: plugin.CatalogRoleLeaf,
					Storage: &plugin.CatalogStorageFacts{
						Path:      "README.md",
						SizeBytes: &sizeBytes,
					},
				},
			},
		},
		content: "hello world\n",
	}
	plugin.Register(provider)
	t.Cleanup(func() {
		plugin.Unregister(provider.Type())
	})
	resource := &commonModels.Engine{ID: 26, Name: "Business NFS", EngineType: provider.Type()}

	result, err := svc.ScanPaths(context.Background(), resource, 1, nil, models.ScannedDepthBasic, false, nil)
	if err != nil {
		t.Fatalf("ScanPaths() error = %v", err)
	}
	if result.Items != 1 {
		t.Fatalf("items = %d, want only README.md", result.Items)
	}
	if _, ok, err := repo.FindItemByFullName(1, 26, ".DS_Store"); err != nil {
		t.Fatalf("FindItemByFullName(.DS_Store) error = %v", err)
	} else if ok {
		t.Fatal(".DS_Store meta_item should not be created")
	}
	if _, ok, err := repo.FindItemByFullName(1, 26, "README.md"); err != nil {
		t.Fatalf("FindItemByFullName(README.md) error = %v", err)
	} else if !ok {
		t.Fatal("README.md meta_item not found")
	}
}

func TestFilesystemForceScanReconcilesStaleRootFileNodes(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	root, err := repo.UpsertNode(1, 26, nil, plugin.CatalogTermRoot, "Business NFS", strPtr(""), models.JSONMap{})
	if err != nil {
		t.Fatalf("create root node: %v", err)
	}
	staleNode, err := repo.UpsertNode(1, 26, root, "dir", "README.md", strPtr("README.md"), models.JSONMap{"schema_version": 1, "storage": map[string]interface{}{"path": "README.md"}})
	if err != nil {
		t.Fatalf("create stale file node: %v", err)
	}
	keepNode, err := repo.UpsertNode(1, 26, root, "dir", "docs", strPtr("docs"), models.JSONMap{"schema_version": 1, "storage": map[string]interface{}{"path": "docs"}})
	if err != nil {
		t.Fatalf("create docs node: %v", err)
	}
	keepItem, err := repo.UpsertItemWithDepth(1, 26, keepNode, "file", "guide.md", "docs/guide.md", models.JSONMap{}, nil, nil, nil, models.ScannedDepthDeep)
	if err != nil {
		t.Fatalf("create docs item: %v", err)
	}
	staleItem, err := repo.UpsertItemWithDepth(1, 26, root, "file", "old.txt", "old.txt", models.JSONMap{}, nil, nil, nil, models.ScannedDepthDeep)
	if err != nil {
		t.Fatalf("create stale root item: %v", err)
	}
	sizeBytes := int64(12)
	provider := filesystemScanTestProvider{
		entriesByPath: map[string][]plugin.CatalogEntry{
			"": {
				{
					Name: "docs",
					Path: plugin.FileDirectoryPath(26, "docs"),
					Term: plugin.CatalogTermDirectory,
					Kind: plugin.CatalogKindDirectory,
					Role: plugin.CatalogRoleBranch,
					Storage: &plugin.CatalogStorageFacts{
						Path: "docs",
					},
				},
				{
					Name: "README.md",
					Path: plugin.FileItemPath(26, "README.md"),
					Term: plugin.CatalogTermFile,
					Kind: plugin.CatalogKindFile,
					Role: plugin.CatalogRoleLeaf,
					Storage: &plugin.CatalogStorageFacts{
						Path:      "README.md",
						SizeBytes: &sizeBytes,
					},
				},
			},
			"docs": {
				{
					Name: "guide.md",
					Path: plugin.FileItemPath(26, "docs/guide.md"),
					Term: plugin.CatalogTermFile,
					Kind: plugin.CatalogKindFile,
					Role: plugin.CatalogRoleLeaf,
					Storage: &plugin.CatalogStorageFacts{
						Path:      "docs/guide.md",
						SizeBytes: &sizeBytes,
					},
				},
			},
		},
		content: "hello world\n",
	}
	plugin.Register(provider)
	t.Cleanup(func() {
		plugin.Unregister(provider.Type())
	})
	resource := &commonModels.Engine{ID: 26, Name: "Business NFS", EngineType: provider.Type()}

	result, err := svc.ScanPaths(context.Background(), resource, 1, nil, models.ScannedDepthBasic, true, nil)
	if err != nil {
		t.Fatalf("ScanPaths() error = %v", err)
	}
	if result.Items != 2 {
		t.Fatalf("items = %d, want 2", result.Items)
	}
	var stale models.MetaNode
	if err := db.Where("id = ?", staleNode.ID).First(&stale).Error; err == nil {
		t.Fatalf("stale README.md node still exists: %#v", stale)
	} else if err != gorm.ErrRecordNotFound {
		t.Fatalf("query stale node: %v", err)
	}
	item, ok, err := repo.FindItemByFullName(1, 26, "README.md")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok {
		t.Fatal("README.md meta_item not found")
	}
	if item.NodeID != root.ID {
		t.Fatalf("README node_id = %d, want root node %d", item.NodeID, root.ID)
	}
	var keptNode models.MetaNode
	if err := db.Where("id = ?", keepNode.ID).First(&keptNode).Error; err != nil {
		t.Fatalf("docs node should be kept: %v", err)
	}
	var keptItem models.MetaItem
	if err := db.Where("id = ?", keepItem.ID).First(&keptItem).Error; err != nil {
		t.Fatalf("docs/guide.md item should be kept: %v", err)
	}
	var oldItem models.MetaItem
	if err := db.Where("id = ?", staleItem.ID).First(&oldItem).Error; err == nil {
		t.Fatalf("stale root item still exists: %#v", oldItem)
	} else if err != gorm.ErrRecordNotFound {
		t.Fatalf("query stale root item: %v", err)
	}
}

func TestFilesystemScanDeletesRootFileNodesWithoutForce(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	root, err := repo.UpsertNode(1, 26, nil, plugin.CatalogTermRoot, "Business NFS", strPtr(""), models.JSONMap{})
	if err != nil {
		t.Fatalf("create root node: %v", err)
	}
	staleNode, err := repo.UpsertNode(1, 26, root, "dir", "README.md", strPtr("README.md"), models.JSONMap{"schema_version": 1, "storage": map[string]interface{}{"path": "README.md"}})
	if err != nil {
		t.Fatalf("create stale file node: %v", err)
	}
	sizeBytes := int64(12)
	provider := filesystemScanTestProvider{
		entriesByPath: map[string][]plugin.CatalogEntry{
			"": {
				{
					Name: "README.md",
					Path: plugin.FileItemPath(26, "README.md"),
					Term: plugin.CatalogTermFile,
					Kind: plugin.CatalogKindFile,
					Role: plugin.CatalogRoleLeaf,
					Storage: &plugin.CatalogStorageFacts{
						Path:      "README.md",
						SizeBytes: &sizeBytes,
					},
				},
			},
		},
		content: "hello world\n",
	}
	plugin.Register(provider)
	t.Cleanup(func() {
		plugin.Unregister(provider.Type())
	})
	resource := &commonModels.Engine{ID: 26, Name: "Business NFS", EngineType: provider.Type()}

	result, err := svc.ScanPaths(context.Background(), resource, 1, nil, models.ScannedDepthBasic, false, nil)
	if err != nil {
		t.Fatalf("ScanPaths() error = %v", err)
	}
	if result.Items != 1 {
		t.Fatalf("items = %d, want 1", result.Items)
	}
	var stale models.MetaNode
	if err := db.Where("id = ?", staleNode.ID).First(&stale).Error; err == nil {
		t.Fatalf("stale README.md node still exists: %#v", stale)
	} else if err != gorm.ErrRecordNotFound {
		t.Fatalf("query stale node: %v", err)
	}
	item, ok, err := repo.FindItemByFullName(1, 26, "README.md")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok || item.NodeID != root.ID {
		t.Fatalf("README item = %#v, found=%v, want item under root %d", item, ok, root.ID)
	}
}

func TestFilesystemScanFilePathTargetDoesNotCreateNode(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	provider := filesystemScanTestProvider{
		entriesByPath: map[string][]plugin.CatalogEntry{
			"": nil,
		},
		missingDirectories: map[string]bool{
			"README.md": true,
		},
	}
	plugin.Register(provider)
	t.Cleanup(func() {
		plugin.Unregister(provider.Type())
	})
	resource := &commonModels.Engine{ID: 26, Name: "Business NFS", EngineType: provider.Type()}

	result, err := svc.ScanPaths(context.Background(), resource, 1, []string{"README.md"}, models.ScannedDepthBasic, false, nil)
	if err != nil {
		t.Fatalf("ScanPaths() error = %v", err)
	}
	if result.CatalogNodes != 0 || result.Items != 0 {
		t.Fatalf("roots/items = %d/%d, want 0/0", result.CatalogNodes, result.Items)
	}
	var count int64
	if err := db.Model(&models.MetaNode{}).
		Where("tenant_id = ? AND engine_id = ? AND full_name = ?", 1, 26, "README.md").
		Count(&count).Error; err != nil {
		t.Fatalf("count README node: %v", err)
	}
	if count != 0 {
		t.Fatalf("README.md node count = %d, want 0", count)
	}
}

func TestFilesystemDeepScanExtractsDOCXHeaderFooter(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	parentNode, err := repo.UpsertNode(1, 26, nil, "dir", "doc", strPtr("doc"), models.JSONMap{"schema_version": 1, "storage": map[string]interface{}{"path": "doc"}})
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
	sizeBytes := int64(len(docx))
	provider := filesystemScanTestProvider{
		files: []plugin.CatalogEntry{{
			Name: "时空数据中台产品详细设计V3.0.0.0（ChatBI部分）.docx",
			Path: plugin.FileItemPath(26, "doc/时空数据中台产品详细设计V3.0.0.0（ChatBI部分）.docx"),
			Term: plugin.CatalogTermFile,
			Kind: plugin.CatalogKindFile,
			Role: plugin.CatalogRoleLeaf,
			Storage: &plugin.CatalogStorageFacts{
				Path:      "doc/时空数据中台产品详细设计V3.0.0.0（ChatBI部分）.docx",
				SizeBytes: &sizeBytes,
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
	files              []plugin.CatalogEntry
	entriesByPath      map[string][]plugin.CatalogEntry
	missingDirectories map[string]bool
	content            string
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
func (p filesystemScanTestProvider) CatalogModel() plugin.CatalogModelSpec {
	return plugin.FileCatalogModel()
}
func (p filesystemScanTestProvider) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p filesystemScanTestProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	if p.missingDirectories[path.StringPath()] {
		return nil, fmt.Errorf("not a directory: %s", path.StringPath())
	}
	if p.entriesByPath != nil {
		return p.entriesByPath[path.StringPath()], nil
	}
	return p.files, nil
}
func (p filesystemScanTestProvider) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.CatalogPath) (*plugin.CatalogEntry, error) {
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
