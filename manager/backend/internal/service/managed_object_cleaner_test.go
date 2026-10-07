package service

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/manager/internal/cog"
)

func TestPPTXPDFStorageRefIsRejectedByPMTilesCleaner(t *testing.T) {
	ref := cog.ObjectStorageRef("manager", "tenant_7/document-preview/fingerprint/slides.pdf")
	err := (&SpatialPreviewService{bucket: "manager"}).DeleteByStorageRef(context.Background(), ref)
	if err == nil || !strings.Contains(err.Error(), ".pmtiles") {
		t.Fatalf("PMTiles format constraint = %v", err)
	}
}

func TestManagedObjectCleanerRequiresConfiguredStore(t *testing.T) {
	ref := cog.ObjectStorageRef("manager", "tenant_7/document-preview/fingerprint/slides.pdf")
	for _, cleaner := range []*MinIOManagedObjectCleaner{nil, NewMinIOManagedObjectCleaner(nil, "manager")} {
		if err := cleaner.DeleteByStorageRef(context.Background(), ref); err == nil {
			t.Fatal("unconfigured cleaner reported success")
		}
	}
}
