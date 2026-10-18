package scanruntime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanresource"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestObjectModelRelatedRefsPersistSizeAndClaims(t *testing.T) {
	testObjectModelRelatedRefsPersistSizeAndClaims(t, openObjectCatalogScanTestDB(t))
}

func TestObjectModelRelatedRefsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("META_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}); err != nil {
		t.Fatal(err)
	}
	testObjectModelRelatedRefsPersistSizeAndClaims(t, tx)
}

func testObjectModelRelatedRefsPersistSizeAndClaims(t *testing.T, db *gorm.DB) {
	t.Helper()
	metaenrich.RegisterItemResolvers()
	obj := "mtllib model.mtl\no panel\nv 0 0 0\nv 2 0 0\nv 0 3 0\nvt 0 0\nvt 1 0\nvt 0 1\nusemtl paint\nf 1/1 2/2 3/3\n"
	files := map[string]string{
		"model.obj": obj,
		"model.mtl": "newmtl paint\nKd 1 1 1\nd 1\nmap_Kd texture.png\n",
		// The resolver only needs the texture's declared path and catalog size.
		"texture.png": "texture bytes",
	}
	reader := objectCatalogScanTestProvider{contentByPath: map[string]string{}}
	resources := []scanresource.StorageResource{}
	var total int64
	modified := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	for _, name := range []string{"model.obj", "model.mtl", "texture.png"} {
		data := files[name]
		reader.contentByPath["addp/models/"+name] = data
		source := objectResourceForTest(9, "addp", "models/"+name, int64(len(data)), "")
		source.LastModified, source.ETag, source.ContentType = &modified, "primary-etag", "application/x-tgif"
		resources = append(resources, source)
		total += int64(len(data))
	}
	pluginRegisterForTest(t, reader)
	repo := metaRepo.NewScanRepository(db)
	bucket, err := repo.UpsertNode(1, 9, nil, "bucket", "addp", strPtr("addp"), scanresource.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := repo.EnsureObjectCatalogPrefixPath(1, 9, bucket, "models")
	if err != nil {
		t.Fatal(err)
	}
	// A previous basic scan may have registered the MTL and PNG separately.
	for _, source := range resources[1:] {
		insertObjectItemForTest(t, repo, 1, 9, parent, "addp", source.Path, source.SizeBytes)
	}
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	var previousID uint
	for scan := 0; scan < 2; scan++ {
		count, _, err := runtime.persistObjectResources(context.Background(), &commonModels.Engine{ID: 9, EngineType: reader.Type()},
			1, 9, bucket, resources, newObjectCatalogScanState(), true, models.ScannedDepthDeep, true, "", map[string]bool{}, "object")
		if err != nil || count != 1 {
			t.Fatalf("scan %d: count=%d err=%v", scan, count, err)
		}
		var item models.MetaItem
		if err := db.Where("tenant_id = ? AND engine_id = ? AND full_name = ?", 1, 9, "addp/models/model.obj").First(&item).Error; err != nil {
			t.Fatal(err)
		}
		if item.SizeBytes == nil {
			t.Fatal("item size missing")
		}
		if *item.SizeBytes != total || commonJSON.Int64(item.Attributes, "storage", "total_size") != total {
			t.Fatalf("item size=%d storage total=%d, want component total %d", *item.SizeBytes, commonJSON.Int64(item.Attributes, "storage", "total_size"), total)
		}
		if commonJSON.String(item.Attributes, "item", "layout") != "single" || commonJSON.String(item.Attributes, "item", "format") != "obj" {
			t.Fatalf("unexpected item identity: %v", item.Attributes)
		}
		if item.DataUpdatedAt == nil || !item.DataUpdatedAt.Equal(modified) ||
			commonJSON.String(item.Attributes, "storage", "etag") != "primary-etag" ||
			commonJSON.String(item.Attributes, "storage", "content_type") != "application/x-tgif" ||
			commonJSON.String(item.Attributes, "storage", "content_hash") != fmt.Sprintf("%x", sha256.Sum256([]byte(obj))) {
			t.Fatalf("primary source facts/hash changed: %v", item.Attributes["storage"])
		}
		if scan > 0 && item.ID != previousID {
			t.Fatal("repeated scan changed model identity")
		}
		previousID = item.ID
		var active int64
		if err := db.Model(&models.MetaItem{}).Where("tenant_id = ? AND engine_id = ?", 1, 9).Count(&active).Error; err != nil || active != 1 {
			t.Fatalf("active items=%d err=%v; claimed MTL/texture must be removed", active, err)
		}
	}
}
