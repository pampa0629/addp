package service

import (
	"testing"

	"github.com/addp/meta/internal/metatest"
	"gorm.io/gorm"
)

func openObjectCatalogScanTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return metatest.OpenMetadataDB(t)
}
