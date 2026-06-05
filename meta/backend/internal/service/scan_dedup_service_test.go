package service

import (
	"strings"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestGenerateExecutionLockKeyUsesScopePriority(t *testing.T) {
	t.Parallel()

	svc := NewScanDedupService(nil)

	engineKey := svc.GenerateExecutionLockKey(1, 9, 0, nil, nil)
	itemKey := svc.GenerateExecutionLockKey(1, 9, 42, []string{"a", "b"}, []models.ScanRefGroup{{Primary: "x"}})

	if engineKey == itemKey {
		t.Fatalf("engine and item lock keys should differ: %q", engineKey)
	}
	if !strings.Contains(itemKey, ":item:42") {
		t.Fatalf("item lock key = %q, want item scope", itemKey)
	}
}

func TestGenerateExecutionLockKeyNormalizesCatalogPathsAndRefGroups(t *testing.T) {
	t.Parallel()

	svc := NewScanDedupService(nil)

	leftPaths := svc.GenerateExecutionLockKey(1, 9, 0, []string{"b", "a", "a"}, nil)
	rightPaths := svc.GenerateExecutionLockKey(1, 9, 0, []string{"a", "b"}, nil)
	if leftPaths != rightPaths {
		t.Fatalf("catalog path lock keys should normalize ordering: %q vs %q", leftPaths, rightPaths)
	}

	leftRefs := svc.GenerateExecutionLockKey(1, 9, 0, nil, []models.ScanRefGroup{
		{
			Primary: "bucket/a.shp",
			Refs: []models.ScanRef{
				{Path: "bucket/a.dbf", Role: "sidecar", Required: true},
				{Path: "bucket/a.shp", Role: "main", Required: true},
			},
		},
	})
	rightRefs := svc.GenerateExecutionLockKey(1, 9, 0, nil, []models.ScanRefGroup{
		{
			Primary: "bucket/a.shp",
			Refs: []models.ScanRef{
				{Path: "bucket/a.shp", Role: "main", Required: true},
				{Path: "bucket/a.dbf", Role: "sidecar", Required: true},
			},
		},
	})
	if leftRefs != rightRefs {
		t.Fatalf("ref group lock keys should normalize ordering: %q vs %q", leftRefs, rightRefs)
	}
}
