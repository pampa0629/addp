package service

import (
	"io"
	"log/slog"
	"testing"

	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

func TestEnsureFilesystemScanRootUsesDirectoryNodeForNonRootPath(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := &FilesystemCatalogScanService{
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		repo: repo,
	}

	rootNode, scanNode, err := svc.ensureFilesystemScanRoot(1, 26, "", "shp")
	if err != nil {
		t.Fatalf("ensureFilesystemScanRoot() error = %v", err)
	}
	if rootNode.Name != "/" || rootNode.FullName != "" {
		t.Fatalf("root node name/fullName = %q/%q, want '/' and empty full_name", rootNode.Name, rootNode.FullName)
	}
	if rootNode.ID == scanNode.ID {
		t.Fatal("scan path shp should use shp directory node, not filesystem root node")
	}
	if scanNode.ParentNodeID == nil || *scanNode.ParentNodeID != rootNode.ID {
		t.Fatalf("scan node parent = %#v, want root id %d", scanNode.ParentNodeID, rootNode.ID)
	}
	if scanNode.NodeType != "dir" || scanNode.Name != "shp" || scanNode.FullName != "shp" {
		t.Fatalf("scan node = %#v, want dir shp with full_name shp", scanNode)
	}

	item, err := repo.UpsertItemWithDepth(
		1,
		26,
		scanNode,
		"file",
		"a3.shp",
		"shp/a3.shp",
		models.JSONMap{},
		nil,
		nil,
		nil,
		models.ScannedDepthDeep,
	)
	if err != nil {
		t.Fatalf("UpsertItemWithDepth() error = %v", err)
	}
	if item.NodeID != scanNode.ID {
		t.Fatalf("item node_id = %d, want scan node %d", item.NodeID, scanNode.ID)
	}
}
