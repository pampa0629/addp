package service

import (
	"context"
	"testing"
)

func TestSpatialPreviewInvalidatesTileCacheRuntimeCacheByResultID(t *testing.T) {
	svc := NewSpatialPreviewService(nil, nil)
	tenantID := uint(1)
	targetKey := svc.buildCacheKey(tenantID, "vector_tile_cache:2", 6, 51, 27)
	otherResultKey := svc.buildCacheKey(tenantID, "vector_tile_cache:3", 6, 51, 27)
	otherTenantKey := svc.buildCacheKey(2, "vector_tile_cache:2", 6, 51, 27)

	svc.backfillCache(context.Background(), targetKey, []byte("old tile"))
	svc.backfillCache(context.Background(), otherResultKey, []byte("other result"))
	svc.backfillCache(context.Background(), otherTenantKey, []byte("other tenant"))

	if _, ok := svc.memCache.Get(targetKey); !ok {
		t.Fatal("target tile was not cached before invalidation")
	}
	if err := svc.InvalidateTileCacheRuntimeCache(context.Background(), tenantID, 2); err != nil {
		t.Fatalf("invalidate runtime cache: %v", err)
	}
	if _, ok := svc.memCache.Get(targetKey); ok {
		t.Fatal("target tile still cached after invalidation")
	}
	if _, ok := svc.memCache.Get(otherResultKey); !ok {
		t.Fatal("other tile cache result was deleted unexpectedly")
	}
	if _, ok := svc.memCache.Get(otherTenantKey); !ok {
		t.Fatal("other tenant cache was deleted unexpectedly")
	}
}
