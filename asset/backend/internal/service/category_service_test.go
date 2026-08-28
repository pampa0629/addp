package service

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/addp/asset/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openCategoryServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS asset").Error; err != nil {
		t.Fatalf("attach asset schema: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE asset.categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			parent_id INTEGER,
			sort_order INTEGER NOT NULL DEFAULT 0,
			description TEXT NOT NULL DEFAULT '',
			version INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE asset.assets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			category_id INTEGER,
			status TEXT NOT NULL DEFAULT 'draft'
		)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create category service table: %v", err)
		}
	}
	return db
}

func TestCategoryServiceUsesOptimisticConcurrency(t *testing.T) {
	service := NewCategoryService(openCategoryServiceTestDB(t))
	category, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Education"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if category.Version != 1 {
		t.Fatalf("created version = %d, want 1", category.Version)
	}

	name := "Schools"
	updated, err := service.Update(7, category.ID, &UpdateAssetCategoryRequest{Version: 1, Name: name, Description: "School assets", SortOrder: 2})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != name || updated.Description != "School assets" || updated.SortOrder != 2 || updated.Version != 2 {
		t.Fatalf("updated category = %#v", updated)
	}
	if _, err := service.Update(7, category.ID, &UpdateAssetCategoryRequest{Version: 1, Name: name}); !errors.Is(err, ErrAssetCategoryVersionConflict) {
		t.Fatalf("stale Update() error = %v", err)
	}
	if err := service.Delete(7, category.ID, 1); !errors.Is(err, ErrAssetCategoryVersionConflict) {
		t.Fatalf("stale Delete() error = %v", err)
	}
	if err := service.Delete(7, category.ID, 2); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestCategoryServiceMovesCategoryWithHierarchyValidation(t *testing.T) {
	db := openCategoryServiceTestDB(t)
	service := NewCategoryService(db)
	root, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Government"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Education", ParentID: &root.ID})
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Schools", ParentID: &child.ID})
	if err != nil {
		t.Fatal(err)
	}
	otherRoot, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Public Services"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Conflict", ParentID: &otherRoot.ID}); err != nil {
		t.Fatal(err)
	}
	otherTenant, err := service.Create(8, &CreateAssetCategoryRequest{Name: "Other tenant"})
	if err != nil {
		t.Fatal(err)
	}

	invalidTargets := []struct {
		name     string
		parentID int64
		wantErr  error
	}{
		{name: "self", parentID: child.ID, wantErr: ErrAssetCategoryInvalidParent},
		{name: "descendant", parentID: grandchild.ID, wantErr: ErrAssetCategoryInvalidParent},
		{name: "cross tenant", parentID: otherTenant.ID, wantErr: ErrAssetCategoryParentNotFound},
	}
	for _, testCase := range invalidTargets {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := service.Update(7, child.ID, &UpdateAssetCategoryRequest{
				Version: 1, Name: child.Name, ParentID: &testCase.parentID,
			})
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Update() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
	if _, err := service.Update(7, child.ID, &UpdateAssetCategoryRequest{
		Version: 1, Name: "Conflict", ParentID: &otherRoot.ID,
	}); !errors.Is(err, ErrAssetCategoryDuplicateName) {
		t.Fatalf("duplicate move error = %v", err)
	}

	moved, err := service.Update(7, child.ID, &UpdateAssetCategoryRequest{
		Version: 1, Name: "Education", ParentID: &otherRoot.ID, Description: "Moved", SortOrder: 3,
	})
	if err != nil {
		t.Fatalf("move category: %v", err)
	}
	if moved.ParentID == nil || *moved.ParentID != otherRoot.ID || moved.Version != 2 || moved.Description != "Moved" {
		t.Fatalf("moved category = %#v", moved)
	}
	if _, err := service.Update(7, child.ID, &UpdateAssetCategoryRequest{
		Version: 1, Name: moved.Name, ParentID: nil,
	}); !errors.Is(err, ErrAssetCategoryVersionConflict) {
		t.Fatalf("stale move error = %v", err)
	}
	rooted, err := service.Update(7, child.ID, &UpdateAssetCategoryRequest{
		Version: 2, Name: moved.Name, ParentID: nil, Description: moved.Description, SortOrder: moved.SortOrder,
	})
	if err != nil {
		t.Fatalf("move category to root: %v", err)
	}
	if rooted.ParentID != nil || rooted.Version != 3 {
		t.Fatalf("rooted category = %#v", rooted)
	}
}

func TestCategoryServiceSubtreeIDsAreTenantScoped(t *testing.T) {
	db := openCategoryServiceTestDB(t)
	service := NewCategoryService(db)
	root, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Government"})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Education", ParentID: &root.ID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	grandchild, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Schools", ParentID: &child.ID})
	if err != nil {
		t.Fatalf("create grandchild: %v", err)
	}
	otherRoot, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Healthcare"})
	if err != nil {
		t.Fatalf("create other root: %v", err)
	}
	otherTenant, err := service.Create(8, &CreateAssetCategoryRequest{Name: "Other tenant"})
	if err != nil {
		t.Fatalf("create other tenant root: %v", err)
	}

	ids, err := service.SubtreeIDs(7, root.ID)
	if err != nil {
		t.Fatalf("SubtreeIDs() error = %v", err)
	}
	want := []int64{root.ID, child.ID, grandchild.ID}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("SubtreeIDs() = %v, want %v", ids, want)
	}
	if slices.Contains(ids, otherRoot.ID) || slices.Contains(ids, otherTenant.ID) {
		t.Fatalf("SubtreeIDs() leaked unrelated categories: %v", ids)
	}
	if _, err := service.SubtreeIDs(7, otherTenant.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant SubtreeIDs() error = %v", err)
	}

	// 历史脏数据即使形成环，递归查询也必须按 ID 去重后终止。
	if err := db.Model(&models.AssetCategory{}).Where("id = ?", root.ID).Update("parent_id", grandchild.ID).Error; err != nil {
		t.Fatalf("seed cyclic category relation: %v", err)
	}
	ids, err = service.SubtreeIDs(7, root.ID)
	if err != nil {
		t.Fatalf("SubtreeIDs() with cycle error = %v", err)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("SubtreeIDs() with cycle = %v, want %v", ids, want)
	}
}

func TestCategoryServicePublishedTreeUsesSubtreeCounts(t *testing.T) {
	db := openCategoryServiceTestDB(t)
	service := NewCategoryService(db)
	root, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Government"})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Education", ParentID: &root.ID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	empty, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Empty"})
	if err != nil {
		t.Fatalf("create empty root: %v", err)
	}
	if err := db.Exec(`INSERT INTO asset.assets (tenant_id, category_id, status) VALUES
		(7, ?, 'published'), (7, ?, 'published'), (7, ?, 'draft'), (8, ?, 'published')`,
		root.ID, child.ID, child.ID, empty.ID).Error; err != nil {
		t.Fatalf("seed assets: %v", err)
	}

	tree, err := service.GetPublishedTree(7)
	if err != nil {
		t.Fatalf("GetPublishedTree() error = %v", err)
	}
	if len(tree) != 1 || tree[0].ID != root.ID || tree[0].Count != 2 {
		t.Fatalf("published tree root = %#v", tree)
	}
	if len(tree[0].Children) != 1 || tree[0].Children[0].ID != child.ID || tree[0].Children[0].Count != 1 {
		t.Fatalf("published tree children = %#v", tree[0].Children)
	}
}
