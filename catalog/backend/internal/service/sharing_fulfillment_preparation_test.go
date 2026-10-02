package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func preparationFixture(t *testing.T, decision *SharingDecisionResult) sharingFulfillmentPreparation {
	t.Helper()
	var path plugin.EngineCatalogPath
	if err := json.Unmarshal(decision.CatalogPath, &path); err != nil {
		t.Fatal(err)
	}
	return sharingFulfillmentPreparation{RequestID: uuid.New(), CallerPrincipalID: 60,
		Operator:           sharingFulfillmentOperator{PrincipalID: 50, MembershipID: 51, AuthorizationVersion: 52},
		RequirementVersion: 1, Path: path}
}

func openSharingPreparationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openCatalogServiceTestDB(t)
	// Like the existing service fixture, declare attached-schema SQLite tables
	// explicitly. GORM SQLite AutoMigrate creates indexes in main, not catalog.
	if err := db.Exec(`CREATE TABLE catalog.fulfillment_checks (
		request_id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, catalog_entry_id TEXT NOT NULL,
		request_binding JSON NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		resolved_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSharingFulfillmentPreparationBindingRetryAndRollback(t *testing.T) {
	db := openSharingPreparationTestDB(t)
	entry, input := seedSharingEntry(t, db)
	input.ExpiryMode, input.ExpiresAt = authorization.SharingExpiryUntilRevoked, nil
	resolver := &fakeSharingTargetResolver{}
	s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(resolver)
	decision, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth())
	if err != nil {
		t.Fatal(err)
	}
	request := preparationFixture(t, decision)
	prepare := func(in sharingFulfillmentPreparation) (*models.FulfillmentCheck, bool, error) {
		var row *models.FulfillmentCheck
		var created bool
		err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			row, created, err = prepareSharingFulfillment(context.Background(), tx, 7, entry.ID, decision.ID, in)
			return err
		})
		return row, created, err
	}
	first, created, err := prepare(request)
	if err != nil || !created || first.ResolvedAt != nil || first.CreatedAt.IsZero() {
		t.Fatalf("prepare=%+v created=%v err=%v", first, created, err)
	}
	var binding sharingFulfillmentBinding
	if err := json.Unmarshal(first.RequestBinding, &binding); err != nil || binding.DecisionID != decision.ID || binding.Operator != request.Operator ||
		binding.ExpiryMode != authorization.SharingExpiryUntilRevoked || binding.ExpiresAt != nil || binding.RecipientID != decision.RecipientID || binding.Action != "read" {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
	for _, mutate := range []func(*sharingFulfillmentPreparation){
		func(i *sharingFulfillmentPreparation) { i.CallerPrincipalID++ },
		func(i *sharingFulfillmentPreparation) { i.Operator.PrincipalID++ },
		func(i *sharingFulfillmentPreparation) { i.Operator.MembershipID++ },
		func(i *sharingFulfillmentPreparation) { i.Operator.AuthorizationVersion++ },
		func(i *sharingFulfillmentPreparation) { i.RequirementVersion++ },
		func(i *sharingFulfillmentPreparation) { i.Path.EngineID++ },
		func(i *sharingFulfillmentPreparation) {
			i.Path.Segments = append([]plugin.EngineCatalogSegment(nil), i.Path.Segments...)
			i.Path.Segments[len(i.Path.Segments)-1].Name = "Orders"
		},
	} {
		changed := request
		mutate(&changed)
		if row, _, err := prepare(changed); !errors.Is(err, ErrSharingDecisionConflict) || row != nil {
			t.Fatalf("changed binding accepted: %+v err=%v", row, err)
		}
	}
	// History recovery is not another current approval. SQLite mutation is only
	// a fixture; PostgreSQL guards prohibit this while a check is unresolved.
	if err := db.Model(&models.Responsibility{}).Where("id = ?", decision.ResponsibilityID).Update("status", "needs_transfer").Error; err != nil {
		t.Fatal(err)
	}
	retry, created, err := prepare(request)
	if err != nil || created || !retry.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("retry refreshed history: %+v created=%v err=%v", retry, created, err)
	}
	newRequest := request
	newRequest.RequestID = uuid.New()
	if _, _, err := prepare(newRequest); !errors.Is(err, ErrSharingConfirmationForbidden) {
		t.Fatalf("new preparation revived invalid decision: %v", err)
	}
	resolvedAt := first.CreatedAt.Add(time.Minute)
	if err := db.Model(&models.FulfillmentCheck{}).Where("request_id = ?", first.RequestID).Update("resolved_at", resolvedAt).Error; err != nil {
		t.Fatal(err)
	}
	retry, created, err = prepare(request)
	if err != nil || created || retry.ResolvedAt == nil || !retry.ResolvedAt.Equal(resolvedAt) {
		t.Fatalf("resolved check reopened: %+v created=%v err=%v", retry, created, err)
	}
	var count int64
	if err := db.Model(&models.FulfillmentCheck{}).Count(&count).Error; err != nil || count != 1 || resolver.calls != 1 {
		t.Fatalf("unexpected check/remote side effect: count=%d calls=%d err=%v", count, resolver.calls, err)
	}
	if _, _, err := prepareSharingFulfillment(context.Background(), db, 7, entry.ID, decision.ID, request); !errors.Is(err, ErrInvalidEntryUpdate) {
		t.Fatalf("preparation outside transaction: %v", err)
	}
}

func TestSharingFulfillmentPreparationRollsBackAndRejectsInvalidReferences(t *testing.T) {
	db := openSharingPreparationTestDB(t)
	entry, input := seedSharingEntry(t, db)
	s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
	decision, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth())
	if err != nil {
		t.Fatal(err)
	}
	request := preparationFixture(t, decision)
	rollback := errors.New("simulate preparation failure before commit")
	err = db.Transaction(func(tx *gorm.DB) error {
		if _, _, err := prepareSharingFulfillment(context.Background(), tx, 7, entry.ID, decision.ID, request); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.FulfillmentCheck{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("uncommitted check survived: count=%d err=%v", count, err)
	}
	for _, fixture := range []struct {
		tenant          int64
		entry, decision uuid.UUID
		want            error
	}{
		{8, entry.ID, decision.ID, ErrEntryNotFound}, {7, uuid.New(), decision.ID, ErrEntryNotFound},
		{7, entry.ID, uuid.New(), ErrEntryNotFound}, {7, entry.ID, uuid.Nil, ErrInvalidEntryUpdate},
	} {
		err := db.Transaction(func(tx *gorm.DB) error {
			_, _, err := prepareSharingFulfillment(context.Background(), tx, fixture.tenant, fixture.entry, fixture.decision, request)
			return err
		})
		if !errors.Is(err, fixture.want) {
			t.Fatalf("invalid reference: %v want=%v", err, fixture.want)
		}
	}
}

func TestSharingFulfillmentPreparationRejectsSystemUnsupportedTargetBeforePending(t *testing.T) {
	for name, mutate := range map[string]func(*plugin.EngineCatalogPath){
		"too deep": func(p *plugin.EngineCatalogPath) {
			leaf := p.Segments[len(p.Segments)-1]
			for len(p.Segments) < 64 {
				p.Segments = append(p.Segments, plugin.EngineCatalogSegment{Term: "directory", Kind: "directory", Name: "node"})
			}
			p.Segments = append(p.Segments, leaf)
		},
		"too large":       func(p *plugin.EngineCatalogPath) { p.Segments[2].Name = strings.Repeat("甲", 6000) },
		"engine overflow": func(p *plugin.EngineCatalogPath) { p.EngineID = math.MaxInt64 + 1 },
		"invalid root":    func(p *plugin.EngineCatalogPath) { p.Segments[0].Term = "schema" },
	} {
		t.Run(name, func(t *testing.T) {
			db := openSharingPreparationTestDB(t)
			entry, input := seedSharingEntry(t, db)
			s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
			decision, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth())
			if err != nil {
				t.Fatal(err)
			}
			request := preparationFixture(t, decision)
			mutate(&request.Path)
			// SQLite negative fixture only: production PostgreSQL decision history
			// is immutable. Both values deliberately agree, so equality alone is
			// insufficient to establish a target the authority can consume.
			pathJSON, _ := json.Marshal(request.Path)
			if err := db.Model(&models.SharingDecision{}).Where("id = ?", decision.ID).Update("catalog_path", pathJSON).Error; err != nil {
				t.Fatal(err)
			}
			err = db.Transaction(func(tx *gorm.DB) error {
				_, _, err := prepareSharingFulfillment(context.Background(), tx, 7, entry.ID, decision.ID, request)
				return err
			})
			if !errors.Is(err, ErrSharingDecisionConflict) {
				t.Fatalf("unsupported target reached pending storage: %v", err)
			}
			var count int64
			if err := db.Model(&models.FulfillmentCheck{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("unsupported target froze basis: %d %v", count, err)
			}
			binding := sharingFulfillmentBinding{CallerPrincipalID: request.CallerPrincipalID, Operator: request.Operator, Path: request.Path,
				DecisionID: decision.ID, RequirementVersion: 1, RecipientType: "user", RecipientID: decision.RecipientID,
				Action: "read", ExpiryMode: decision.ExpiryMode, ExpiresAt: decision.ExpiresAt}
			raw, _ := json.Marshal(binding)
			if _, err := decodeSharingFulfillmentBinding(raw); !errors.Is(err, ErrSharingDecisionConflict) {
				t.Fatalf("recovery accepted authority-unsupported target: %v", err)
			}
		})
	}
}
