package service

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/format/builtin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/jonas-p/go-shp"
)

func TestRefreshKnownMultiItemUsesStoredRefsWithoutCatalogRediscovery(t *testing.T) {
	t.Parallel()

	db := openObjectCatalogScanTestDB(t)
	tenantID := uint(1)
	engineID := uint(77)
	engineSvc := NewEngineService(db, "", "", nil)
	engineSvc.engineCache[engineID] = &engineCacheEntry{
		resource: &commonModels.Engine{
			ID:         engineID,
			TenantID:   &tenantID,
			EngineType: "known-refresh-test",
			IsActive:   true,
		},
		expiresAt: time.Now().Add(time.Hour),
	}

	base := createRefreshTestShapefile(t)
	content := map[string][]byte{}
	var total int64
	for _, ext := range []string{".shp", ".shx", ".dbf"} {
		data, err := os.ReadFile(base + ext)
		if err != nil {
			t.Fatalf("read shapefile ref %s: %v", ext, err)
		}
		content["addp/gis/roads"+ext] = data
		total += int64(len(data))
	}
	plugin.Register(refreshContentReader{content: content})

	svc := NewScanService(db, engineSvc)
	svc.log = slog.Default()
	bucketNode := models.MetaNode{TenantID: tenantID, EngineID: engineID, NodeType: "bucket", Name: "addp", FullName: "addp", Depth: 0}
	if err := db.Create(&bucketNode).Error; err != nil {
		t.Fatalf("create bucket node: %v", err)
	}
	item := models.MetaItem{
		TenantID:    tenantID,
		EngineID:    engineID,
		NodeID:      bucketNode.ID,
		ItemType:    "object",
		Name:        "roads.shp",
		FullName:    "addp/gis/roads.shp",
		Fingerprint: "known-refresh-shp",
		SizeBytes:   &total,
		Attributes: models.JSONMap{
			"item": map[string]interface{}{
				"layout":    string(dataitem.LayoutMulti),
				"data_type": string(dataitem.DataTypeTable),
				"format":    "shapefile",
				"refs": []map[string]interface{}{
					{"path": "addp/gis/roads.shp", "role": "main", "primary": true, "required": true, "extension": ".shp"},
					{"path": "addp/gis/roads.shx", "role": "index", "required": true, "extension": ".shx"},
					{"path": "addp/gis/roads.dbf", "role": "attributes", "required": true, "extension": ".dbf"},
				},
			},
			"storage": map[string]interface{}{
				"bucket":     "addp",
				"path":       "gis/",
				"name":       "roads.shp",
				"total_size": total,
			},
			"access_index": map[string]interface{}{
				"table": map[string]interface{}{
					"kind": "sparse_row_index",
				},
			},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}

	resp, err := svc.RefreshItem(context.Background(), engineID, tenantID, item.ID, "", true)
	if err != nil {
		t.Fatalf("RefreshItem() error = %v", err)
	}
	if resp.ItemsScanned != 1 || resp.FieldsScanned == 0 {
		t.Fatalf("response = %#v, want refreshed item and fields", resp)
	}

	var refreshed models.MetaItem
	if err := db.First(&refreshed, item.ID).Error; err != nil {
		t.Fatalf("load refreshed item: %v", err)
	}
	if accessIndex, ok := refreshed.Attributes["access_index"].(map[string]interface{}); ok {
		if tableIndex, exists := accessIndex["table"]; exists {
			t.Fatalf("access_index.table = %#v, want stale shapefile table index removed", tableIndex)
		}
	}
}

func TestRefreshKnownPDFItemWritesDocumentAndFormatInfo(t *testing.T) {
	t.Parallel()

	db := openObjectCatalogScanTestDB(t)
	tenantID := uint(1)
	engineID := uint(78)
	engineSvc := NewEngineService(db, "", "", nil)
	engineSvc.engineCache[engineID] = &engineCacheEntry{
		resource: &commonModels.Engine{
			ID:         engineID,
			TenantID:   &tenantID,
			EngineType: "known-refresh-pdf-test",
			IsActive:   true,
		},
		expiresAt: time.Now().Add(time.Hour),
	}

	content := []byte("%PDF-1.4\n/Info 1 0 obj << /Title (Plan) /Author (Ada) /Creator (Writer) /Producer (PDFLib) >>\n1 0 obj << /Type /Page >>\n")
	plugin.Register(refreshContentReader{engineType: "known-refresh-pdf-test", content: map[string][]byte{"docs/plan.pdf": content}})

	svc := NewScanService(db, engineSvc)
	svc.log = slog.Default()
	node := models.MetaNode{TenantID: tenantID, EngineID: engineID, NodeType: "bucket", Name: "docs", FullName: "docs", Depth: 0}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	size := int64(len(content))
	item := models.MetaItem{
		TenantID:    tenantID,
		EngineID:    engineID,
		NodeID:      node.ID,
		ItemType:    "object",
		Name:        "plan.pdf",
		FullName:    "docs/plan.pdf",
		Fingerprint: "known-refresh-pdf",
		SizeBytes:   &size,
		Attributes: models.JSONMap{
			"item": map[string]interface{}{
				"layout":    string(dataitem.LayoutSingle),
				"data_type": string(dataitem.DataTypeDocument),
				"format":    "pdf",
			},
			"storage": map[string]interface{}{
				"bucket":     "docs",
				"path":       "",
				"name":       "plan.pdf",
				"size_bytes": size,
			},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}

	resp, err := svc.RefreshItem(context.Background(), engineID, tenantID, item.ID, "", true)
	if err != nil {
		t.Fatalf("RefreshItem() error = %v", err)
	}
	if resp.ItemsScanned != 1 {
		t.Fatalf("ItemsScanned = %d, want 1", resp.ItemsScanned)
	}

	var refreshed models.MetaItem
	if err := db.First(&refreshed, item.ID).Error; err != nil {
		t.Fatalf("load refreshed item: %v", err)
	}
	typeInfo := refreshed.Attributes["type_info"].(map[string]interface{})
	document := typeInfo["document"].(map[string]interface{})
	if document["title"] != "Plan" || document["page_count"] != float64(1) {
		t.Fatalf("type_info.document = %#v", document)
	}
	if document["author"] != nil || document["creator"] != nil {
		t.Fatalf("type_info.document should not contain PDF native fields: %#v", document)
	}
	pdfInfo := refreshed.Attributes["format_info"].(map[string]interface{})["pdf"].(map[string]interface{})
	if pdfInfo["author"] != "Ada" || pdfInfo["creator"] != "Writer" || pdfInfo["producer"] != "PDFLib" {
		t.Fatalf("format_info.pdf = %#v", pdfInfo)
	}
}

func TestRefreshKnownDOCXItemExtractsTextFacts(t *testing.T) {
	t.Parallel()

	db := openObjectCatalogScanTestDB(t)
	tenantID := uint(1)
	engineID := uint(79)
	engineSvc := NewEngineService(db, "", "", nil)
	engineSvc.engineCache[engineID] = &engineCacheEntry{
		resource: &commonModels.Engine{
			ID:         engineID,
			TenantID:   &tenantID,
			EngineType: "known-refresh-docx-test",
			IsActive:   true,
		},
		expiresAt: time.Now().Add(time.Hour),
	}

	content := minimalRefreshDOCX(t, `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>行业赛道</w:t></w:r><w:r><w:t>分析</w:t></w:r></w:p></w:body></w:document>`)
	plugin.Register(refreshContentReader{engineType: "known-refresh-docx-test", content: map[string][]byte{"addp/doc/关于时空底座.docx": content}})

	svc := NewScanService(db, engineSvc)
	svc.log = slog.Default()
	node := models.MetaNode{TenantID: tenantID, EngineID: engineID, NodeType: "bucket", Name: "addp", FullName: "addp", Depth: 0}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	size := int64(len(content))
	item := models.MetaItem{
		TenantID:    tenantID,
		EngineID:    engineID,
		NodeID:      node.ID,
		ItemType:    "object",
		Name:        "关于时空底座.docx",
		FullName:    "addp/doc/关于时空底座.docx",
		Fingerprint: "known-refresh-docx",
		SizeBytes:   &size,
		Attributes: models.JSONMap{
			"item": map[string]interface{}{
				"layout":    string(dataitem.LayoutSingle),
				"data_type": string(dataitem.DataTypeDocument),
				"format":    "docx",
			},
			"storage": map[string]interface{}{
				"bucket":        "addp",
				"path":          "doc/",
				"name":          "关于时空底座.docx",
				"physical_path": "addp/doc/关于时空底座.docx",
				"size_bytes":    size,
			},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}

	resp, err := svc.RefreshItem(context.Background(), engineID, tenantID, item.ID, "", true)
	if err != nil {
		t.Fatalf("RefreshItem() error = %v", err)
	}
	if resp.ItemsScanned != 1 {
		t.Fatalf("ItemsScanned = %d, want 1", resp.ItemsScanned)
	}

	var refreshed models.MetaItem
	if err := db.First(&refreshed, item.ID).Error; err != nil {
		t.Fatalf("load refreshed item: %v", err)
	}
	if !commonJSON.Bool(refreshed.Attributes, "capabilities.extraction", "text_extracted") {
		t.Fatalf("capabilities.extraction = %#v", refreshed.Attributes["capabilities"])
	}
	if got := commonJSON.String(refreshed.Attributes, "capabilities.extraction", "plain_text_preview"); got != "行业赛道分析" {
		t.Fatalf("plain_text_preview = %q", got)
	}
	if got := commonJSON.String(refreshed.Attributes, "capabilities.extraction", "extractor"); got != "common_format:docx" {
		t.Fatalf("extractor = %q", got)
	}
	if commonJSON.Bool(refreshed.Attributes, "type_info.document", "text_extracted") {
		t.Fatalf("type_info.document should not carry extraction status: %#v", refreshed.Attributes["type_info"])
	}
	if got := commonJSON.String(refreshed.Attributes, "storage", "content_hash"); got == "" {
		t.Fatal("storage.content_hash missing")
	}
	if got := commonJSON.String(refreshed.Attributes, "storage", "content_hash_algorithm"); got != "sha256" {
		t.Fatalf("storage.content_hash_algorithm = %q", got)
	}
}

func TestRefreshKnownZIPItemWritesContainerInfo(t *testing.T) {
	t.Parallel()

	db := openObjectCatalogScanTestDB(t)
	tenantID := uint(1)
	engineID := uint(80)
	engineSvc := NewEngineService(db, "", "", nil)
	engineSvc.engineCache[engineID] = &engineCacheEntry{
		resource: &commonModels.Engine{
			ID:         engineID,
			TenantID:   &tenantID,
			EngineType: "known-refresh-zip-test",
			IsActive:   true,
		},
		expiresAt: time.Now().Add(time.Hour),
	}

	content := minimalRefreshZIP(t, map[string]string{
		"data/cities.csv":  "id,name\n1,Hangzhou\n",
		"notes/readme.txt": "hello",
	})
	plugin.Register(refreshContentReader{engineType: "known-refresh-zip-test", content: map[string][]byte{"addp/archive.zip": content}})

	svc := NewScanService(db, engineSvc)
	svc.log = slog.Default()
	node := models.MetaNode{TenantID: tenantID, EngineID: engineID, NodeType: "bucket", Name: "addp", FullName: "addp", Depth: 0}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	size := int64(len(content))
	item := models.MetaItem{
		TenantID:    tenantID,
		EngineID:    engineID,
		NodeID:      node.ID,
		ItemType:    "object",
		Name:        "archive.zip",
		FullName:    "addp/archive.zip",
		Fingerprint: "known-refresh-zip",
		SizeBytes:   &size,
		Attributes: models.JSONMap{
			"item": map[string]interface{}{
				"layout":    string(dataitem.LayoutSingle),
				"data_type": string(dataitem.DataTypeContainer),
				"format":    "zip",
			},
			"storage": map[string]interface{}{
				"bucket":        "addp",
				"path":          "",
				"name":          "archive.zip",
				"physical_path": "addp/archive.zip",
				"size_bytes":    size,
			},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}

	resp, err := svc.RefreshItem(context.Background(), engineID, tenantID, item.ID, "", true)
	if err != nil {
		t.Fatalf("RefreshItem() error = %v", err)
	}
	if resp.ItemsScanned != 1 {
		t.Fatalf("ItemsScanned = %d, want 1", resp.ItemsScanned)
	}

	var refreshed models.MetaItem
	if err := db.First(&refreshed, item.ID).Error; err != nil {
		t.Fatalf("load refreshed item: %v", err)
	}
	if got := commonJSON.String(refreshed.Attributes, "item", "data_type"); got != string(dataitem.DataTypeContainer) {
		t.Fatalf("item.data_type = %q, want container", got)
	}
	if got := commonJSON.String(refreshed.Attributes, "item", "format"); got != "zip" {
		t.Fatalf("item.format = %q, want zip", got)
	}
	container := commonJSON.Section(refreshed.Attributes, "type_info.container")
	if commonJSON.InterfaceInt64(container["child_count"]) != 2 {
		t.Fatalf("type_info.container = %#v, want child_count 2", container)
	}
	if children, ok := container["children"].([]interface{}); !ok || len(children) != 2 {
		t.Fatalf("type_info.container.children = %#v, want 2 entries", container["children"])
	}
	if got := commonJSON.String(refreshed.Attributes, "storage", "content_hash"); got == "" {
		t.Fatal("storage.content_hash missing")
	}
	if got := commonJSON.String(refreshed.Attributes, "storage", "content_hash_algorithm"); got != "sha256" {
		t.Fatalf("storage.content_hash_algorithm = %q", got)
	}
}

type refreshContentReader struct {
	engineType string
	content    map[string][]byte
}

func (r refreshContentReader) Type() string {
	if r.engineType != "" {
		return r.engineType
	}
	return "known-refresh-test"
}
func (r refreshContentReader) DisplayName() string  { return "known refresh test" }
func (r refreshContentReader) EngineOrigin() string { return "general" }
func (r refreshContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r refreshContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r refreshContentReader) DefaultPort() int                                   { return 0 }
func (r refreshContentReader) RequiredFields() []string                           { return nil }
func (r refreshContentReader) SensitiveFields() []string                          { return nil }
func (r refreshContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r refreshContentReader) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (r refreshContentReader) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	data, ok := r.content[path.StringPath()]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func createRefreshTestShapefile(t *testing.T) string {
	t.Helper()
	base := filepath.Join(t.TempDir(), "roads")
	writer, err := shp.Create(base+".shp", shp.POINT)
	if err != nil {
		t.Fatalf("create shapefile failed: %v", err)
	}
	writer.SetFields([]shp.Field{shp.StringField("NAME", 16)})
	for i, name := range []string{"a", "b"} {
		row := writer.Write(&shp.Point{X: float64(i + 1), Y: float64(i + 2)})
		if err := writer.WriteAttribute(int(row), 0, name); err != nil {
			t.Fatalf("write attribute failed: %v", err)
		}
	}
	writer.Close()
	if _, err := os.Stat(strings.TrimSuffix(base, filepath.Ext(base)) + "dbf"); err == nil {
		if err := os.Rename(base+"dbf", base+".dbf"); err != nil {
			t.Fatalf("rename dbf failed: %v", err)
		}
	}
	return base
}

func minimalRefreshDOCX(t *testing.T, documentXML string) []byte {
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

func minimalRefreshZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, body := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := file.Write([]byte(body)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}
