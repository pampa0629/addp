package repository

import (
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestWholeScopeReconciliationUsesOwnedParentEdges(t *testing.T) {
	db := openScanRepositoryTestDB(t)
	repo := NewScanRepository(db)
	root, err := repo.UpsertNode(7, 9, nil, "root", "NFS", strPtr(""), nil)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := repo.UpsertNode(7, 9, root, "dir", "统计 %", strPtr("统计 %"), nil)
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.UpsertNode(7, 9, scope, "dir", "old", strPtr("统计 %/old"), nil)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := repo.UpsertNode(7, 9, root, "dir", "统计 %-other", strPtr("统计 %-other"), nil)
	if err != nil {
		t.Fatal(err)
	}
	attrs := models.JSONMap{"item": map[string]any{"layout": "whole"}}
	whole, err := repo.UpsertItemWithDepth(7, 9, scope, "file", scope.Name, scope.FullName, attrs, nil, nil, nil, "basic")
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []*models.MetaNode{scope, child, sibling} {
		if _, err := repo.UpsertItemWithDepth(7, 9, owner, "file", "old.txt", owner.FullName+"/old.txt", models.JSONMap{}, nil, nil, nil, "basic"); err != nil {
			t.Fatal(err)
		}
	}
	// Cached hierarchy text cannot redirect cleanup to a same-prefix sibling.
	if err := db.Model(child).Update("path", sibling.Path+"/fake").Error; err != nil {
		t.Fatal(err)
	}
	var removed []models.MetaItem
	if err := repo.Transaction(func(r *ScanRepository) error { var err error; removed, err = r.ReconcileWholeScope(whole); return err }); err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 {
		t.Fatalf("removed %d items, want scope/internal components only", len(removed))
	}
	var nodes []models.MetaNode
	if err := db.Find(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("wrong remaining nodes: %+v", nodes)
	}
	var items []models.MetaItem
	if err := db.Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("whole/sibling not preserved: %+v", items)
	}
	if err := repo.HardDeleteInvalidEngineGraph(7, 9); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&models.MetaItem{}, whole.ID).Error; err != nil {
		t.Fatal("graph cleanup deleted legitimate whole", err)
	}
}
