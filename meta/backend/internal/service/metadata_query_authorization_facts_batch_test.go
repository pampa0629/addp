package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/meta/internal/metatest"
	"gorm.io/gorm"
)

func TestAuthorizationFactsBatchKeepsTenantMissingAndFailureBoundaries(t *testing.T) {
	exerciseAuthorizationFactsBatch(t, metatest.OpenMetadataDB(t))
}

func exerciseAuthorizationFactsBatch(t *testing.T, db *gorm.DB) {
	item := seedAuthorizationFacts(t, db, "schema", "table", 209)
	svc := NewMetadataQueryService(db)
	query := authorizationfacts.AuthorizationFactsQuery{Fingerprints: []string{strings.Repeat("b", 64), item.Fingerprint}}
	batch, err := svc.QueryDataItemAuthorizationFacts(t.Context(), 7, query)
	if err != nil || batch.Validate(query) != nil || batch.Data[0].Facts != nil || batch.Data[1].Facts == nil {
		t.Fatal("missing sibling poisoned exact fact", err)
	}
	batch, err = svc.QueryDataItemAuthorizationFacts(t.Context(), 8, query)
	if err != nil || batch.Data[0].Facts != nil || batch.Data[1].Facts != nil {
		t.Fatal("cross-tenant fact leaked", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if batch, err = svc.QueryDataItemAuthorizationFacts(ctx, 7, query); !errors.Is(err, context.Canceled) || batch != nil {
		t.Fatal("canceled facts returned partial batch", err)
	}
	if err := db.Exec("DROP TABLE meta.meta_node").Error; err != nil {
		t.Fatal(err)
	}
	if batch, err = svc.QueryDataItemAuthorizationFacts(t.Context(), 7, query); err == nil || batch != nil {
		t.Fatal("database failure became missing fact", err)
	}
}
