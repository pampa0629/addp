package service

import (
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
	contentIndex := refreshed.Attributes["content_index"].(map[string]interface{})["table"].(map[string]interface{})
	if contentIndex["kind"] != "sparse_row_index" {
		t.Fatalf("content_index.table = %#v, want sparse_row_index", contentIndex)
	}
	if _, ok := contentIndex["anchors"]; !ok {
		t.Fatalf("content_index.table.anchors missing: %#v", contentIndex)
	}
}

type refreshContentReader struct {
	content map[string][]byte
}

func (r refreshContentReader) Type() string         { return "known-refresh-test" }
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
