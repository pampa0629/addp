package resourcetree

import (
	"github.com/addp/common/models"
	"testing"
	"time"
)

func TestTreePreservesIndependentNodeScanFacts(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	node := &models.MetaNode{ID: 7, NodeType: "bucket", Name: "addp", FullName: "addp", ScanStatus: "failed", ScannedDepth: "basic", LastScanAt: &at, Attributes: map[string]interface{}{"scanned_depth": "deep"}}
	builder := NewTreeBuilder()
	tree := builder.ConvertMetaNodes(&models.Engine{ID: 9}, []*models.MetaNode{node})[0]
	if tree.Metadata["scan_status"] != "failed" || tree.Metadata["scanned_depth"] != "basic" || tree.Metadata["scanned_at"] != at.Format(time.RFC3339) {
		t.Fatalf("independent facts lost: %#v", tree.Metadata)
	}
	node.ScannedDepth, node.LastScanAt = "none", nil
	builder.MergeMetadata(tree, []*models.MetaNode{node})
	if tree.Metadata["scanned_depth"] != "none" || tree.Metadata["scanned_at"] != nil {
		t.Fatalf("stale successful facts remain: %#v", tree.Metadata)
	}
}
