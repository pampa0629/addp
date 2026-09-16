package scanprocessor

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
	"github.com/xuri/excelize/v2"
)

func TestKnownContainerRefreshReplacesMisclassifiedTableFacts(t *testing.T) {
	workbook := excelize.NewFile()
	defer workbook.Close()
	content, err := workbook.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	for _, valid := range []bool{true, false} {
		name := "valid"
		if !valid {
			name = "broken"
		}
		t.Run(name, func(t *testing.T) {
			db := openObjectCatalogProcessorTestDB(t)
			repo := metaRepo.NewScanRepository(db)
			parent, err := repo.UpsertNode(1, 7, nil, "bucket", "addp", strPtr("addp"), scanresource.ObjectBucketNodeAttributes("addp"))
			if err != nil {
				t.Fatal(err)
			}
			oldRows, size := int64(99), int64(content.Len())
			item, err := repo.UpsertItemWithDepth(1, 7, parent, "object", "sample.xlsx", "addp/sample.xlsx", models.JSONMap{
				"item":         map[string]interface{}{"layout": "single", "data_type": "table", "format": "excel"},
				"storage":      map[string]interface{}{"physical_path": "addp/sample.xlsx"},
				"type_info":    map[string]interface{}{"table": map[string]interface{}{"row_count": 99}},
				"access_index": map[string]interface{}{"table": map[string]interface{}{"kind": "stale"}},
			}, &oldRows, &size, nil, models.ScannedDepthDeep)
			if err != nil {
				t.Fatal(err)
			}
			detected := scanflow.KnownItemDetectedItem(item, scanflow.KnownItemDescriptorFromAttributes(item.Attributes))
			data := content.String()
			if !valid {
				data = "broken workbook"
			}
			_, err = New(repo, nil, nil).Process(context.Background(), KnownItemInput(
				&commonModels.Engine{ID: 7}, 1, parent, *item, item.Attributes, detected,
				staticObjectContentReader{content: data}, nil,
				func(string) plugin.EngineCatalogPath { return plugin.ObjectItemPath(7, "addp", "sample.xlsx") },
				"addp/sample.xlsx", "addp", "sample.xlsx", size,
			))
			if valid && err != nil {
				t.Fatal(err)
			}
			if !valid && err == nil {
				t.Fatal("broken container refresh must fail")
			}
			var stored models.MetaItem
			if err := db.First(&stored, item.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !valid {
				if stored.RowCount == nil || *stored.RowCount != 99 {
					t.Fatal("failed refresh overwrote previous snapshot")
				}
				if commonJSON.String(stored.Attributes, "item", "data_type") != "table" || len(commonJSON.Section(stored.Attributes, "access_index.table")) == 0 {
					t.Fatal("failed refresh partially replaced previous attributes")
				}
				return
			}
			if stored.FullName != "addp/sample.xlsx" || commonJSON.String(stored.Attributes, "storage", "physical_path") != "addp/sample.xlsx" {
				t.Fatal("container correction changed resource identity or storage path")
			}
			if got := commonJSON.String(stored.Attributes, "item", "data_type"); got != "container" {
				t.Fatalf("data_type=%s", got)
			}
			if stored.RowCount != nil {
				t.Fatalf("parent row_count=%v, want nil", *stored.RowCount)
			}
			if len(commonJSON.Section(stored.Attributes, "type_info.table")) != 0 || len(commonJSON.Section(stored.Attributes, "access_index.table")) != 0 {
				t.Fatal("stale table facts persisted")
			}
			if commonJSON.InterfaceInt64(commonJSON.Section(stored.Attributes, "type_info.container")["child_count"]) != 1 {
				t.Fatal("missing container children")
			}
			var count int64
			if err := db.Model(&models.MetaItem{}).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("item count=%d err=%v", count, err)
			}
		})
	}
}
