package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuthorizationFactsBatchKeepsTenantMissingAndFailureBoundaries(t *testing.T) {
	exerciseAuthorizationFactsBatch(t, metatest.OpenMetadataDB(t))
}

type factsQueryCounter struct {
	logger.Interface
	reads int
}

func (l *factsQueryCounter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "SELECT") {
		l.reads++
	}
	l.Interface.Trace(ctx, begin, func() (string, int64) { return sql, rows }, err)
}

func exerciseAuthorizationFactsBatch(t *testing.T, db *gorm.DB) {
	item := seedAuthorizationFacts(t, db, "schema", "table", 209)
	svc := NewMetadataQueryService(db)
	// A full batch sharing a parent must not issue one SQL transaction/chain per item.
	fingerprints := []string{item.Fingerprint}
	for i := 1; i < authorizationfacts.MaxAuthorizationFactsQueryItems; i++ {
		sibling := item
		sibling.ID = 0
		sibling.Name = fmt.Sprintf("batch_%d", i)
		sibling.FullName = "Out.door." + sibling.Name
		sibling.Fingerprint = commonModels.GenerateItemFingerprint(sibling.EngineID, sibling.FullName)
		if err := db.Create(&sibling).Error; err != nil {
			t.Fatal(err)
		}
		fingerprints = append(fingerprints, sibling.Fingerprint)
	}
	counter := &factsQueryCounter{Interface: db.Logger}
	counted := NewMetadataQueryService(db.Session(&gorm.Session{Logger: counter}))
	fullQuery := authorizationfacts.AuthorizationFactsQuery{Fingerprints: fingerprints}
	full, err := counted.QueryDataItemAuthorizationFacts(t.Context(), 7, fullQuery)
	if err != nil || full.Validate(fullQuery) != nil {
		t.Fatal("full batch rejected", err)
	}
	for _, entry := range full.Data {
		if entry.Facts == nil || entry.Facts.BasisState != authorizationfacts.AuthorizationBasisComplete {
			t.Fatal("full batch lost a proven sibling")
		}
	}
	if counter.reads > 4 {
		t.Fatalf("%d siblings used %d SELECTs; require a shared committed fact snapshot", len(fingerprints), counter.reads)
	}
	t.Logf("%d authorization facts: %d SELECTs", len(fingerprints), counter.reads)
	ids := make([]uint, 0, len(fingerprints))
	if err := db.Model(&item).Where("tenant_id=7 AND engine_id=?", item.EngineID).Pluck("id", &ids).Error; err != nil {
		t.Fatal(err)
	}
	counter.reads = 0
	tree := &ResourceTreeService{metadataQueryService: counted}
	targets, err := tree.metadataItemVisibilityTargets(t.Context(), 7, item.EngineID, ids)
	if err != nil || len(targets) != len(fingerprints) || counter.reads > 4 {
		t.Fatalf("%d navigation targets: %d SELECTs, %d targets, err=%v", len(fingerprints), counter.reads, len(targets), err)
	}
	t.Logf("%d navigation targets: %d SELECTs", len(fingerprints), counter.reads)

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
