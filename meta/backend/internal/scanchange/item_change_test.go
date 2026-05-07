package scanchange

import (
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/models"
)

func TestShouldUpdateTableUsesLastModified(t *testing.T) {
	t.Parallel()

	oldTime := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Hour)
	item := &models.MetaItem{DataUpdatedAt: &oldTime}
	table := plugin.TableInfo{LastModified: &newTime}

	if !ShouldUpdateTable(item, table) {
		t.Fatal("table should update when source last_modified is newer")
	}
}

func TestShouldUpdateCollectionSizeThreshold(t *testing.T) {
	t.Parallel()

	oldSize := int64(100)
	item := &models.MetaItem{SizeBytes: &oldSize}
	if ShouldUpdateCollection(item, plugin.CollectionInfo{SizeBytes: 105}) {
		t.Fatal("5 percent size change should not update")
	}
	if !ShouldUpdateCollection(item, plugin.CollectionInfo{SizeBytes: 120}) {
		t.Fatal("20 percent size change should update")
	}
}

func TestShouldUpdateObjectUsesExactSourceState(t *testing.T) {
	t.Parallel()

	modifiedAt := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	size := int64(100)
	item := &models.MetaItem{DataUpdatedAt: &modifiedAt, SizeBytes: &size}

	if ShouldUpdateObject(item, format.ObjectMetadata{LastModified: &modifiedAt, SizeBytes: size}) {
		t.Fatal("unchanged object should not update")
	}

	later := modifiedAt.Add(time.Second)
	if !ShouldUpdateObject(item, format.ObjectMetadata{LastModified: &later, SizeBytes: size}) {
		t.Fatal("object should update when last_modified changes")
	}

	if !ShouldUpdateObject(item, format.ObjectMetadata{LastModified: &modifiedAt, SizeBytes: size + 1}) {
		t.Fatal("object should update when size changes")
	}
}
