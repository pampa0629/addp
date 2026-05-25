package duckdb

import (
	"testing"

	commonModels "github.com/addp/common/models"
)

func TestIsObjectTableItemUsesContentAttributes(t *testing.T) {
	t.Parallel()

	item := commonModels.MetaItem{
		ItemType: "object",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "parquet",
			},
		},
	}

	if !IsObjectTableItem(item) {
		t.Fatal("object table parquet item should be recognized")
	}

	item.ItemType = "table"
	if IsObjectTableItem(item) {
		t.Fatal("catalog table item should not be recognized as object/file table")
	}
}
