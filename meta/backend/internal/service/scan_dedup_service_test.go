package service

import (
	"context"
	"time"

	"github.com/addp/common/infra/redistest"
	"strings"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestLeafScopeLockRetainsTargetIdentity(t *testing.T) {
	s := NewScanDedupService(nil)
	a := "addp://engine/9/path/public/A?type=table"
	b := "addp://engine/9/path/public/B?type=table"
	key := s.GenerateExecutionLockKey(1, 9, 0, nil, nil, []string{a})
	for _, other := range []string{s.GenerateExecutionLockKey(1, 9, 0, nil, nil, []string{b}), s.GenerateExecutionLockKey(1, 9, 0, []string{"public"}, nil, nil), s.GenerateExecutionLockKey(1, 9, 0, nil, nil, nil)} {
		if key == other {
			t.Fatal("leaf lock collapsed to sibling or parent range")
		}
	}
}

func TestGenerateExecutionLockKeyUsesScopePriority(t *testing.T) {
	t.Parallel()

	svc := NewScanDedupService(nil)

	engineKey := svc.GenerateExecutionLockKey(1, 9, 0, nil, nil, nil)
	itemKey := svc.GenerateExecutionLockKey(1, 9, 42, []string{"a", "b"}, []models.ScanRefGroup{{Primary: "x"}}, nil)

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

	leftPaths := svc.GenerateExecutionLockKey(1, 9, 0, []string{"b", "a", "a"}, nil, nil)
	rightPaths := svc.GenerateExecutionLockKey(1, 9, 0, []string{"a", "b"}, nil, nil)
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
	}, nil)
	rightRefs := svc.GenerateExecutionLockKey(1, 9, 0, nil, []models.ScanRefGroup{
		{
			Primary: "bucket/a.shp",
			Refs: []models.ScanRef{
				{Path: "bucket/a.shp", Role: "main", Required: true},
				{Path: "bucket/a.dbf", Role: "sidecar", Required: true},
			},
		},
	}, nil)
	if leftRefs != rightRefs {
		t.Fatalf("ref group lock keys should normalize ordering: %q vs %q", leftRefs, rightRefs)
	}
}

func TestScanExecutionLockAgainstRedis(t *testing.T) {
	cfg := redistest.Config(t)
	first := NewScanDedupService(redistest.Open(t, cfg))
	second := NewScanDedupService(redistest.Open(t, cfg))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := first.GenerateExecutionLockKey(1, 9, 42, nil, nil, nil)
	ttl := 500 * time.Millisecond
	acquired, err := first.TryAcquireOwnedLock(ctx, key, "first", ttl)
	if err != nil || !acquired {
		t.Fatalf("first execution failed to acquire its range: %v", err)
	}
	if acquired, err := second.TryAcquireOwnedLock(ctx, key, "second", time.Minute); err != nil || acquired {
		t.Fatalf("second execution did not observe the range in use: acquired=%v err=%v", acquired, err)
	}
	if released, err := second.ReleaseOwnedLock(ctx, key, "second"); err != nil || released {
		t.Fatalf("wrong execution released the range: released=%v err=%v", released, err)
	}
	for {
		acquired, err = second.TryAcquireOwnedLock(ctx, key, "second", time.Minute)
		if err != nil {
			t.Fatalf("waiting for range expiry: %v", err)
		}
		if acquired {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if released, err := first.ReleaseOwnedLock(ctx, key, "first"); err != nil || released {
		t.Fatalf("expired execution released a reacquired range: released=%v err=%v", released, err)
	}
	if released, err := second.ReleaseOwnedLock(ctx, key, "second"); err != nil || !released {
		t.Fatalf("current execution could not release its range: released=%v err=%v", released, err)
	}
}
