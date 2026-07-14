package metaquery

import (
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestToMetaItemLiteKeepsFingerprint(t *testing.T) {
	t.Parallel()

	result := ToMetaItemLite(models.MetaItem{
		ID:          21,
		Fingerprint: "item-fingerprint-21",
	})

	if result.Fingerprint != "item-fingerprint-21" {
		t.Fatalf("Fingerprint = %q, want item-fingerprint-21", result.Fingerprint)
	}
}
