package scanprocessor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type versionedDelimitedContent struct {
	staticObjectContentReader
	mtime            time.Time
	etag             string
	changeDuringRead bool
	factsError       error
}

func (p *versionedDelimitedContent) DescribeEngineCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	if p.factsError != nil {
		return nil, p.factsError
	}
	size, modified := int64(len(p.content)), p.mtime
	return &plugin.EngineCatalogFacts{Path: path, UpdatedAt: &modified, Storage: &plugin.EngineCatalogStorageFacts{SizeBytes: &size, ETag: p.etag}}, nil
}

func (p *versionedDelimitedContent) OpenContent(_ context.Context, _ plugin.ConnectionInfo, _ plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	if p.changeDuringRead {
		p.mtime = p.mtime.Add(time.Second)
	}
	return io.NopCloser(strings.NewReader(p.content)), nil
}

func TestDelimitedSourceRefreshBindsCurrentVersionAndKeepsFailedSnapshot(t *testing.T) {
	testDelimitedSourceVersionRefresh(t, openObjectCatalogProcessorTestDB)
}

func TestDelimitedSourceVersionRefreshAgainstPostgres(t *testing.T) {
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
	testDelimitedSourceVersionRefresh(t, func(t *testing.T) *gorm.DB {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		t.Cleanup(func() {
			if err := tx.Rollback().Error; err != nil {
				t.Errorf("rollback source-version fixture: %v", err)
			}
		})
		if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS meta").Error; err != nil {
			t.Fatal(err)
		}
		if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}); err != nil {
			t.Fatal(err)
		}
		return tx
	})
}

func testDelimitedSourceVersionRefresh(t *testing.T, openDB func(*testing.T) *gorm.DB) {
	for _, formatName := range []string{"csv", "tsv"} {
		for _, outcome := range []string{"stable", "changed_during_parse", "facts_unavailable"} {
			t.Run(formatName+"/"+outcome, func(t *testing.T) {
				db := openDB(t)
				repo := metaRepo.NewScanRepository(db)
				parent, err := repo.UpsertNode(1, 927, nil, "bucket", "samples", strPtr("samples"), scanresource.ObjectBucketNodeAttributes("samples"))
				if err != nil {
					t.Fatal(err)
				}
				oldTime := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
				rows, size := int64(99), int64(1)
				name := "data." + formatName
				item, err := repo.UpsertItemWithDepth(1, 927, parent, "object", name, "samples/"+name, models.JSONMap{
					"item":         map[string]interface{}{"layout": "single", "data_type": "table", "format": formatName},
					"storage":      map[string]interface{}{"physical_path": "samples/" + name, "bucket": "samples", "total_size": size, "etag": "old", "last_modified_at": oldTime.Format(time.RFC3339Nano)},
					"type_info":    map[string]interface{}{"table": map[string]interface{}{"row_count": 99, "fields": []interface{}{map[string]interface{}{"name": "old"}}}},
					"access_index": map[string]interface{}{"table": map[string]interface{}{"kind": "stale", "source": map[string]interface{}{"etag": "old"}}},
				}, &rows, &size, &oldTime, models.ScannedDepthDeep)
				if err != nil {
					t.Fatal(err)
				}
				item, err = repo.GetItemByID(1, item.ID)
				if err != nil {
					t.Fatal(err)
				}
				before, _ := json.Marshal(item)
				data := "id,name\n1,Alice\n2,Bobby\n"
				if formatName == "tsv" {
					data = strings.ReplaceAll(data, ",", "\t")
				}
				now := oldTime.Add(24 * time.Hour)
				content := &versionedDelimitedContent{staticObjectContentReader: staticObjectContentReader{content: data}, mtime: now, etag: "new"}
				if outcome == "changed_during_parse" {
					content.changeDuringRead = true
				}
				if outcome == "facts_unavailable" {
					content.factsError = fmt.Errorf("storage offline")
				}
				descriptor := scanflow.KnownItemDescriptorFromAttributes(item.Attributes)
				detected := scanflow.KnownItemDetectedItem(item, descriptor)
				in := KnownItemInput(&commonModels.Engine{ID: 927}, 1, parent, *item, item.Attributes, detected, content, nil, func(string) plugin.EngineCatalogPath { return plugin.ObjectItemPath(927, "samples", name) }, "samples/"+name, "samples", name, size)
				in.Encoding = "utf-8"
				_, err = New(repo, nil, nil).Process(t.Context(), in)
				if (err == nil) != (outcome == "stable") {
					t.Fatalf("unexpected refresh outcome %s: %v", outcome, err)
				}
				stored, readErr := repo.GetItemByID(1, item.ID)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if outcome != "stable" {
					after, _ := json.Marshal(stored)
					if string(after) != string(before) {
						t.Fatal("failed version check replaced the persisted snapshot")
					}
					return
				}
				if stored.SizeBytes == nil || *stored.SizeBytes != int64(len(data)) || stored.RowCount == nil || *stored.RowCount != 2 || stored.DataUpdatedAt == nil || !stored.DataUpdatedAt.Equal(now) {
					t.Fatalf("refresh kept stale size/row count/time: %#v", stored)
				}
				source := commonJSON.Section(stored.Attributes, "access_index.table.source")
				if commonJSON.InterfaceInt64(source["size_bytes"]) != int64(len(data)) || source["etag"] != "new" || source["last_modified_at"] != now.Format(time.RFC3339Nano) {
					t.Fatalf("index bound to stale source: %#v", source)
				}
				if commonJSON.String(stored.Attributes, "storage", "etag") != "new" {
					t.Fatal("storage facts were not refreshed")
				}
			})
		}
	}
}
