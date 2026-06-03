package extractor

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExtractObjectMetadataOnDemandUsesUnifiedMediaEnrichment(t *testing.T) {
	db := openMetadataExtractorTestDB(t)
	extractor := NewMetadataExtractor(db)
	bucket := models.MetaNode{
		TenantID:   1,
		EngineID:   7,
		NodeType:   "bucket",
		Name:       "addp",
		Depth:      1,
		FullName:   "addp",
		Attributes: models.JSONMap{"schema_version": 1, "storage": map[string]interface{}{"bucket": "addp"}},
	}
	if err := db.Create(&bucket).Error; err != nil {
		t.Fatalf("create bucket node: %v", err)
	}

	size := int64(0)
	detected := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "pixel.png",
			SizeBytes:          &size,
		},
		PhysicalPath: "pixel.png",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(detected)))
	metaattr.SetStorage(attrs, "bucket", "addp")
	metaattr.SetStorage(attrs, "name", "pixel.png")

	item := models.MetaItem{
		TenantID:    1,
		EngineID:    7,
		NodeID:      bucket.ID,
		ItemType:    "object",
		Name:        "pixel.png",
		FullName:    "addp/pixel.png",
		Fingerprint: strings.Repeat("a", 64),
		Attributes:  attrs,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create meta item: %v", err)
	}

	content := metadataExtractorTestPNG(t, 2, 3)
	updated, err := extractor.ExtractObjectMetadataOnDemand(1, 7, "addp/pixel.png", "", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("ExtractObjectMetadataOnDemand() error = %v", err)
	}
	if got := commonJSON.String(updated, "item", "data_type"); got != string(datatype.Media) {
		t.Fatalf("item.data_type = %q, want media", got)
	}
	if got := commonJSON.String(updated, "item", "format"); got != string(format.FormatPNG) {
		t.Fatalf("item.format = %q, want png", got)
	}
	media := commonJSON.Section(updated, "type_info.media")
	if media["kind"] != "image" || commonJSON.InterfaceInt64(media["width"]) != 2 || commonJSON.InterfaceInt64(media["height"]) != 3 {
		t.Fatalf("type_info.media = %#v, want image 2x3", media)
	}
	if commonJSON.Bool(updated, "capabilities.extraction", "metadata_extracted") {
		t.Fatalf("capabilities.extraction should not carry deep-scan process marker: %#v", commonJSON.Section(updated, "capabilities.extraction"))
	}

	var stored models.MetaItem
	if err := db.First(&stored, item.ID).Error; err != nil {
		t.Fatalf("reload meta item: %v", err)
	}
	if got := commonJSON.String(stored.Attributes, "item", "data_type"); got != string(datatype.Media) {
		t.Fatalf("stored item.data_type = %q, want media", got)
	}
}

func TestExtractObjectMetadataOnDemandUsesUnifiedContainerEnrichment(t *testing.T) {
	db := openMetadataExtractorTestDB(t)
	extractor := NewMetadataExtractor(db)
	bucket := models.MetaNode{
		TenantID:   1,
		EngineID:   7,
		NodeType:   "bucket",
		Name:       "addp",
		Depth:      1,
		FullName:   "addp",
		Attributes: models.JSONMap{"schema_version": 1, "storage": map[string]interface{}{"bucket": "addp"}},
	}
	if err := db.Create(&bucket).Error; err != nil {
		t.Fatalf("create bucket node: %v", err)
	}

	size := int64(0)
	detected := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "archive.zip",
			SizeBytes:          &size,
		},
		PhysicalPath: "archive.zip",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(detected)))
	metaattr.SetStorage(attrs, "bucket", "addp")
	metaattr.SetStorage(attrs, "name", "archive.zip")

	item := models.MetaItem{
		TenantID:    1,
		EngineID:    7,
		NodeID:      bucket.ID,
		ItemType:    "object",
		Name:        "archive.zip",
		FullName:    "addp/archive.zip",
		Fingerprint: strings.Repeat("b", 64),
		Attributes:  attrs,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create meta item: %v", err)
	}

	content := metadataExtractorTestZIP(t, map[string]string{
		"data/cities.csv":  "id,name\n1,Hangzhou\n",
		"notes/readme.txt": "hello",
	})
	updated, err := extractor.ExtractObjectMetadataOnDemand(1, 7, "addp/archive.zip", "", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("ExtractObjectMetadataOnDemand() error = %v", err)
	}
	if got := commonJSON.String(updated, "item", "data_type"); got != string(datatype.Container) {
		t.Fatalf("item.data_type = %q, want container", got)
	}
	if got := commonJSON.String(updated, "item", "format"); got != string(format.FormatZIP) {
		t.Fatalf("item.format = %q, want zip", got)
	}
	container := commonJSON.Section(updated, "type_info.container")
	if commonJSON.InterfaceInt64(container["child_count"]) != 2 {
		t.Fatalf("type_info.container = %#v, want child_count 2", container)
	}
	children, ok := container["children"].([]map[string]interface{})
	if !ok || len(children) != 2 {
		t.Fatalf("type_info.container.children = %#v, want 2 entries", container["children"])
	}
	if commonJSON.Bool(updated, "capabilities.extraction", "metadata_extracted") {
		t.Fatalf("capabilities.extraction should not carry deep-scan process marker: %#v", commonJSON.Section(updated, "capabilities.extraction"))
	}

	var stored models.MetaItem
	if err := db.First(&stored, item.ID).Error; err != nil {
		t.Fatalf("reload meta item: %v", err)
	}
	if got := commonJSON.String(stored.Attributes, "item", "data_type"); got != string(datatype.Container) {
		t.Fatalf("stored item.data_type = %q, want container", got)
	}
}

func openMetadataExtractorTestDB(t *testing.T) *gorm.DB {
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

func metadataExtractorTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func metadataExtractorTestZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}
