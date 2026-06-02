package scanchange

import (
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/models"
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

func TestShouldUpdateDynamicSchemaItemSizeThreshold(t *testing.T) {
	t.Parallel()

	oldSize := int64(100)
	item := &models.MetaItem{SizeBytes: &oldSize}
	if ShouldUpdateDynamicSchemaItem(item, 0, 105) {
		t.Fatal("5 percent size change should not update")
	}
	if !ShouldUpdateDynamicSchemaItem(item, 0, 120) {
		t.Fatal("20 percent size change should update")
	}
}

func TestShouldUpdateStorageResourceUsesExactSourceState(t *testing.T) {
	t.Parallel()

	modifiedAt := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	size := int64(100)
	item := &models.MetaItem{DataUpdatedAt: &modifiedAt, SizeBytes: &size}

	if ShouldUpdateStorageResource(item, metacatalog.StorageResource{LastModified: &modifiedAt, SizeBytes: size}) {
		t.Fatal("unchanged object should not update")
	}

	later := modifiedAt.Add(time.Second)
	if !ShouldUpdateStorageResource(item, metacatalog.StorageResource{LastModified: &later, SizeBytes: size}) {
		t.Fatal("object should update when last_modified changes")
	}

	if !ShouldUpdateStorageResource(item, metacatalog.StorageResource{LastModified: &modifiedAt, SizeBytes: size + 1}) {
		t.Fatal("object should update when size changes")
	}
}
