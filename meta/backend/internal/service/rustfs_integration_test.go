package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/rustfs"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanruntime"
)

func TestIntegrationRustFSDeepScan(t *testing.T) {
	if os.Getenv("ADDP_RUSTFS_INTEGRATION") != "1" {
		t.Skip("owned RustFS T2 required")
	}
	p := &rustfs.RustFSPlugin{}
	plugin.Register(p)
	db := metatest.OpenMetadataDB(t)
	repo := metaRepo.NewScanRepository(db)
	engine := &commonModels.Engine{ID: 91, Name: "RustFS T2", EngineType: p.Type(), ConnectionInfo: commonModels.ConnectionInfo{
		"endpoint": os.Getenv("ADDP_RUSTFS_TEST_ENDPOINT"), "access_key": os.Getenv("ADDP_RUSTFS_TEST_ACCESS_KEY"), "secret_key": os.Getenv("ADDP_RUSTFS_TEST_SECRET_KEY"), "use_ssl": false,
	}}
	runtime := scanruntime.NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	bucket := os.Getenv("ADDP_RUSTFS_TEST_BUCKET")
	result, err := runtime.ScanPaths(context.Background(), engine, 1, []string{bucket}, nil, models.ScannedDepthDeep, true, nil)
	if err != nil || result.Items != 1 {
		t.Fatalf("deep scan result=%#v error=%v", result, err)
	}
	item, found, err := repo.FindItemByFullName(1, 91, bucket+"/样例/订单.csv")
	if err != nil || !found || item.ItemType != "object" {
		t.Fatal(item, found, err)
	}
	if commonJSON.String(item.Attributes, "item", "format") != "csv" || commonJSON.String(item.Attributes, "item", "data_type") != "table" {
		t.Fatalf("CSV content was not classified: %#v", item.Attributes)
	}
	facts, err := NewMetadataQueryService(db).GetDataItemAuthorizationFacts(t.Context(), 1, item.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := facts.ProvePrimaryTarget(plugin.ObjectItemPath(91, bucket, "样例/订单.csv")); err != nil {
		t.Fatal("real scanned object has no exact content boundary", err)
	}
	rawFacts, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("ADDP_RUSTFS_TEST_META_ITEM")+".facts", rawFacts, 0600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("ADDP_RUSTFS_TEST_META_ITEM"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
