package scanprocessor

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/format/plugins/csv"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDelimitedEncodingRefresh(t *testing.T) {
	testDelimitedEncodingRefresh(t, openObjectCatalogProcessorTestDB)
}

func TestDelimitedEncodingRefreshAgainstPostgres(t *testing.T) {
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
	testDelimitedEncodingRefresh(t, func(t *testing.T) *gorm.DB {
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
		return tx
	})
}

func testDelimitedEncodingRefresh(t *testing.T, openDB func(*testing.T) *gorm.DB) {
	// Fixed GBK bytes for 姓名,城市 / 张三,北京; independent of the production encoder.
	gbk := "\xd0\xd5\xc3\xfb,\xb3\xc7\xca\xd0\n\xd5\xc5\xc8\xfd,\xb1\xb1\xbe\xa9\n"
	headerless := false
	for _, tc := range []struct {
		name, format, data, encoding, actual string
		valid                                bool
		header                               *bool
	}{
		{"GBK CSV", "csv", gbk, "gbk", "gbk", true, nil},
		{"GBK TSV", "tsv", "\xd0\xd5\xc3\xfb\t\xb3\xc7\xca\xd0\n\xd5\xc5\xc8\xfd\t\xb1\xb1\xbe\xa9\n", "gbk", "gbk", true, nil},
		{"BOM wins over choice", "csv", "\xef\xbb\xbf姓名,城市\n张三,北京\n", "gbk", "utf-8", true, nil},
		{"empty header keeps snapshot", "csv", "", "utf-8", "", false, nil},
		{"headerless CSV", "csv", "姓名,城市\n张三,北京\n", "utf-8", "utf-8", true, &headerless},
		{"headerless TSV", "tsv", "姓名\t城市\n张三\t北京\n", "utf-8", "utf-8", true, &headerless},
		{"unsupported BOM keeps snapshot", "csv", "\xff\xfe\x00\x00name", "utf-8", "", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openDB(t)
			repo := metaRepo.NewScanRepository(db)
			parent, err := repo.UpsertNode(1, 927, nil, "bucket", "encoding-test", strPtr("encoding-test"), scanresource.ObjectBucketNodeAttributes("encoding-test"))
			if err != nil {
				t.Fatal(err)
			}
			name := "sample." + tc.format
			fullName := "encoding-test/" + name
			oldRows, size := int64(99), int64(len(tc.data))
			item, err := repo.UpsertItemWithDepth(1, 927, parent, "object", name, fullName, models.JSONMap{
				"item":         map[string]interface{}{"layout": "single", "data_type": "table", "format": tc.format},
				"storage":      map[string]interface{}{"physical_path": fullName},
				"format_info":  map[string]interface{}{tc.format: map[string]interface{}{"encoding": "utf-8"}},
				"type_info":    map[string]interface{}{"table": map[string]interface{}{"row_count": 99, "fields": []interface{}{map[string]interface{}{"name": "old"}}}},
				"access_index": map[string]interface{}{"table": map[string]interface{}{"kind": "stale"}},
			}, &oldRows, &size, nil, models.ScannedDepthDeep)
			if err != nil {
				t.Fatal(err)
			}
			// Compare persisted snapshots: PostgreSQL stores timestamps at microsecond precision.
			item, err = repo.GetItemByID(1, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(item)
			process := func(current *models.MetaItem, data, encoding string, header *bool, rescan bool) error {
				detected := scanflow.KnownItemDetectedItem(current, scanflow.KnownItemDescriptorFromAttributes(current.Attributes))
				in := KnownItemInput(&commonModels.Engine{ID: 927}, 1, parent, *current, current.Attributes, detected, staticObjectContentReader{content: data}, nil,
					func(string) plugin.EngineCatalogPath { return plugin.ObjectItemPath(927, "encoding-test", name) }, fullName, "encoding-test", name, size, "deep")
				in.Encoding = encoding
				in.HasHeader = header
				if rescan { // Directory discovery has fresh basic facts, not the previous format section.
					in.ExistingItemID = 0
					in.StrictDeepEnrich = false
					in.Attributes = models.JSONMap{"item": map[string]interface{}{"layout": "single", "data_type": "table", "format": tc.format}}
				}
				_, err := New(repo, nil, nil).Process(context.Background(), in)
				return err
			}
			err = process(item, tc.data, tc.encoding, tc.header, false)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t error=%v", tc.valid, err)
			}
			stored, err := repo.GetItemByID(1, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.valid {
				after, _ := json.Marshal(stored)
				if string(before) != string(after) {
					t.Fatalf("failed refresh replaced snapshot: before=%s after=%s", before, after)
				}
				return
			}
			if stored.ID != item.ID || stored.Fingerprint != item.Fingerprint || stored.FullName != fullName {
				t.Fatal("refresh changed identity")
			}
			assertFacts := func(current *models.MetaItem) {
				t.Helper()
				if actual := commonJSON.String(current.Attributes, "format_info."+tc.format, "encoding"); actual != tc.actual {
					t.Fatalf("encoding=%q want %q", actual, tc.actual)
				}
				wantFields, wantRows, wantHeader := []string{"姓名", "城市"}, int64(1), true
				if tc.header != nil && !*tc.header {
					wantFields, wantRows, wantHeader = []string{"Column1", "Column2"}, 2, false
				}
				if native := commonJSON.Section(current.Attributes, "type_info.table.native"); native["has_header"] != wantHeader {
					t.Fatalf("header mode=%#v want %t", native, wantHeader)
				}
				fields := commonJSON.InterfaceSlice(commonJSON.Section(current.Attributes, "type_info.table")["fields"])
				if len(fields) != 2 || fields[0].(map[string]interface{})["name"] != wantFields[0] || fields[1].(map[string]interface{})["name"] != wantFields[1] {
					t.Fatalf("fields=%#v", fields)
				}
				if current.RowCount == nil || *current.RowCount != wantRows {
					t.Fatalf("rows=%v", current.RowCount)
				}
				if commonJSON.String(current.Attributes, "access_index.table", "kind") == "stale" {
					t.Fatal("old index survived codec change")
				}
			}
			assertFacts(stored)
			if err := process(stored, tc.data, "", nil, true); err != nil {
				t.Fatalf("directory rescan: %v", err)
			}
			stored, err = repo.GetItemByID(1, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertFacts(stored)
			before, _ = json.Marshal(stored)
			if err := process(stored, "", "", nil, true); err == nil {
				t.Fatal("rescan swallowed parse failure")
			}
			stored, err = repo.GetItemByID(1, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(stored)
			if string(before) != string(after) {
				t.Fatal("failed directory rescan erased selected codec or schema")
			}
			// Invalid persisted declarations are refused; an explicit valid choice can repair them.
			table := commonJSON.Section(stored.Attributes, "type_info.table")
			table["native"] = map[string]interface{}{"has_header": "false"}
			if err := db.Model(stored).Update("attributes", stored.Attributes).Error; err != nil {
				t.Fatal(err)
			}
			before, _ = json.Marshal(stored.Attributes)
			if err := process(stored, tc.data, "", nil, false); err == nil {
				t.Fatal("invalid known header mode accepted")
			}
			stored, err = repo.GetItemByID(1, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			after, _ = json.Marshal(stored.Attributes)
			if string(before) != string(after) {
				t.Fatal("rejected mode replaced persisted facts")
			}
			mode := true
			if tc.header != nil {
				mode = *tc.header
			}
			if err := process(stored, tc.data, "", &mode, false); err != nil {
				t.Fatal(err)
			}
			stored, err = repo.GetItemByID(1, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertFacts(stored)
		})
	}
}
