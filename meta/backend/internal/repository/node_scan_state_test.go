package repository

import (
	"testing"
	"time"

	"github.com/addp/meta/internal/models"
)

func TestNodeScanFailurePreservesSuccessfulFacts(t *testing.T) {
	for _, hasSuccess := range []bool{false, true} {
		t.Run(map[bool]string{false: "never_successful", true: "previous_success"}[hasSuccess], func(t *testing.T) {
			db := openScanRepositoryTestDB(t)
			repo := NewScanRepository(db)
			node, err := repo.UpsertNode(1, 9, nil, "bucket", "addp", strPtr("addp"), models.JSONMap{})
			if err != nil {
				t.Fatal(err)
			}
			var lastSuccess *time.Time
			depth := models.ScannedDepthNone
			if hasSuccess {
				at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				lastSuccess, depth = &at, models.ScannedDepthBasic
				if err := db.Model(node).Updates(map[string]interface{}{"scanned_at": at, "scanned_depth": depth}).Error; err != nil {
					t.Fatal(err)
				}
			}
			for _, status := range []string{"running", "failed"} {
				if status == "running" {
					err = repo.ResetNodeState(node, status)
				} else {
					err = repo.FinalizeNodeStateWithDepth(node, status, "broken source", models.ScannedDepthDeep)
				}
				if err != nil {
					t.Fatal(err)
				}
				var got models.MetaNode
				if err := db.First(&got, node.ID).Error; err != nil {
					t.Fatal(err)
				}
				if got.ScanStatus != status || got.ScannedDepth != depth {
					t.Fatalf("%s changed successful depth: %#v", status, got)
				}
				if (got.ScannedAt == nil) != (lastSuccess == nil) || (lastSuccess != nil && !got.ScannedAt.Equal(*lastSuccess)) {
					t.Fatalf("%s changed successful time: %v", status, got.ScannedAt)
				}
			}
		})
	}
}
