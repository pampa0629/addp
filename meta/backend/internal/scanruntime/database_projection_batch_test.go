package scanruntime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

type projectionBatchCatalog struct {
	boundaryScanProvider
	count int
}

func (p projectionBatchCatalog) ListChildren(_ context.Context, _ plugin.ConnectionInfo, _ plugin.EngineCatalogPath, _ plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	entries := make([]plugin.EngineCatalogEntry, p.count)
	for i := range entries {
		name := fmt.Sprintf("table_%03d", i)
		entries[i] = plugin.EngineCatalogEntry{Name: name, Role: plugin.EngineCatalogRoleLeaf, Kind: "table", Table: &datatype.TableInfo{Name: name, Kind: "table"}}
	}
	return entries, nil
}

type projectionBatchIndexer struct {
	boundaryIndexer
	batches [][]string
}

func (i *projectionBatchIndexer) IndexTechnicalMetadata(_ context.Context, _ *commonModels.Engine, _ uint, _ *models.MetaNode, items ...*models.MetaItem) bool {
	batch := make([]string, len(items))
	for index, item := range items {
		batch[index] = item.Fingerprint
	}
	i.batches = append(i.batches, batch)
	return true
}

func TestDatabaseProjectionBatchesIncludeUnchangedItemsAndFlushRemainder(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	node, err := repo.UpsertNode(7, 9, nil, "schema", "public", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	indexer := &projectionBatchIndexer{}
	runtime := NewDatabaseRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, indexer)
	resource := &commonModels.Engine{ID: 9, EngineType: "postgresql"}
	catalog := databaseScanCatalog{catalogProvider: projectionBatchCatalog{count: 201}, namespaceTerm: "schema", itemTerm: "table"}
	var first []string
	for _, force := range []bool{true, false} {
		indexer.batches = nil
		items, fields, err := runtime.scanTables(t.Context(), resource, catalog, 7, 9, node, "public", "basic", force, nil)
		if err != nil || items != 201 || fields != 0 {
			t.Fatalf("scan: items=%d fields=%d error=%v", items, fields, err)
		}
		var sizes []int
		var fingerprints []string
		for _, batch := range indexer.batches {
			sizes = append(sizes, len(batch))
			fingerprints = append(fingerprints, batch...)
		}
		if !reflect.DeepEqual(sizes, []int{100, 100, 1}) {
			t.Fatalf("batch sizes=%v", sizes)
		}
		if force {
			first = fingerprints
		} else if !reflect.DeepEqual(first, fingerprints) {
			t.Fatal("unchanged items lost their projection or identity")
		}
	}
}
