package duckdb

import (
	"testing"

	commonModels "github.com/addp/common/models"
)

func TestIsLakeTableItemUsesContentAttributes(t *testing.T) {
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

	if !isLakeTableItem(item) {
		t.Fatal("object table parquet item should be recognized as lake table")
	}

	item.ItemType = "table"
	if isLakeTableItem(item) {
		t.Fatal("catalog table item should not be recognized as object/file lake table")
	}
}
