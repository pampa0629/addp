package scanchange

import (
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanresource"
)

func TestShouldUpdateTableUsesUpdatedAt(t *testing.T) {
	t.Parallel()

	oldTime := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Hour)
	item := &models.MetaItem{DataUpdatedAt: &oldTime}
	table := datatype.TableInfo{UpdatedAt: &newTime}

	if !ShouldUpdateTable(item, table) {
		t.Fatal("table should update when source updated_at is newer")
	}
}

func TestShouldUpdateTableUsesEstimatedRowCount(t *testing.T) {
	t.Parallel()

	oldEstimate := int64(5)
	newEstimate := int64(10)
	item := &models.MetaItem{Attributes: models.JSONMap{
		"type_info": map[string]interface{}{
			"table": map[string]interface{}{"estimated_row_count": oldEstimate},
		},
	}}
	table := datatype.TableInfo{EstimatedRowCount: &newEstimate}

	if !ShouldUpdateTable(item, table) {
		t.Fatal("table should update when estimated row count changes")
	}
}

func TestShouldUpdateDynamicSchemaItemSizeThreshold(t *testing.T) {
	t.Parallel()

	oldSize := int64(100)
	item := &models.MetaItem{
		SizeBytes: &oldSize,
		Attributes: models.JSONMap{
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{"estimated_row_count": int64(0)},
			},
		},
	}
	estimatedCount := int64(0)
	if ShouldUpdateDynamicSchemaItem(item, &estimatedCount, 105) {
		t.Fatal("5 percent size change should not update")
	}
	if !ShouldUpdateDynamicSchemaItem(item, &estimatedCount, 120) {
		t.Fatal("20 percent size change should update")
	}
}

func TestShouldUpdateDynamicSchemaItemDoesNotInventMissingEstimate(t *testing.T) {
	t.Parallel()

	item := &models.MetaItem{Attributes: models.JSONMap{}}
	if ShouldUpdateDynamicSchemaItem(item, nil, 0) {
		t.Fatal("missing estimate and unchanged size should not force an update")
	}
}

func TestShouldUpdateStorageResourceUsesExactSourceState(t *testing.T) {
	t.Parallel()

	modifiedAt := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	size := int64(100)
	item := &models.MetaItem{DataUpdatedAt: &modifiedAt, SizeBytes: &size}

	if ShouldUpdateStorageResource(item, scanresource.StorageResource{LastModified: &modifiedAt, SizeBytes: size}) {
		t.Fatal("unchanged object should not update")
	}

	later := modifiedAt.Add(time.Second)
	if !ShouldUpdateStorageResource(item, scanresource.StorageResource{LastModified: &later, SizeBytes: size}) {
		t.Fatal("object should update when last_modified changes")
	}

	if !ShouldUpdateStorageResource(item, scanresource.StorageResource{LastModified: &modifiedAt, SizeBytes: size + 1}) {
		t.Fatal("object should update when size changes")
	}
}
