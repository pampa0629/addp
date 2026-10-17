package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
)

type sourceEngineResolverFunc func(context.Context, int64, []int64) ([]EngineReferenceResolution, error)

func (f sourceEngineResolverFunc) ResolveEngineReferences(ctx context.Context, tenantID int64, ids []int64) ([]EngineReferenceResolution, error) {
	return f(ctx, tenantID, ids)
}

func TestEntryDetailResolvesSourceEngineWithoutPersistingOwnerFacts(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	snapshot := commonModels.JSONMap{"engine_id": 14, "item_id": 8, "name": "orders", "item_type": "table", "full_name": "sales.orders"}
	if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ?", entry.ID).Update("observed_snapshot", snapshot).Error; err != nil {
		t.Fatal(err)
	}
	resolver := sourceEngineResolverFunc(func(_ context.Context, tenantID int64, ids []int64) ([]EngineReferenceResolution, error) {
		if tenantID != 7 || !reflect.DeepEqual(ids, []int64{14}) {
			t.Fatalf("unexpected scope: tenant=%d ids=%v", tenantID, ids)
		}
		return []EngineReferenceResolution{{ID: ids[0], Found: true, Name: "业务 PostgreSQL", EngineType: "postgresql", LifecycleState: "active"}}, nil
	})
	svc := NewEntryService(db, nil, nil).WithEngineReferenceResolver(resolver)
	detail, err := svc.Get(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.SourceEngine == nil || detail.SourceEngine.Status != "current" || detail.SourceEngine.Name != "业务 PostgreSQL" || detail.SourceEngine.EngineType != "postgresql" || detail.SourceEngine.LifecycleState != "active" {
		t.Fatalf("source engine = %#v", detail.SourceEngine)
	}
	encoded, err := json.Marshal(detail.SourceEngine)
	if err != nil || !strings.Contains(string(encoded), `"id":"14"`) {
		t.Fatalf("engine ID must be a string: %s, %v", encoded, err)
	}
	var persisted models.SourceBinding
	if err := db.Where("catalog_entry_id = ?", entry.ID).First(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if len(persisted.ObservedSnapshot) != len(snapshot) || persisted.ObservedSnapshot["engine_name"] != nil || persisted.ObservedSnapshot["full_name"] != "sales.orders" {
		t.Fatalf("dynamic facts persisted: %#v", persisted.ObservedSnapshot)
	}

	svc.WithEngineReferenceResolver(sourceEngineResolverFunc(func(context.Context, int64, []int64) ([]EngineReferenceResolution, error) {
		return nil, errors.New("System unavailable")
	}))
	detail, err = svc.Get(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID)
	if err != nil || detail.SourceEngine.Status != "unavailable" || detail.SourceEngine.Name != "" {
		t.Fatalf("owner failure blocked or misrepresented detail: %#v, %v", detail, err)
	}
}

func TestSourceEngineResolutionDistinguishesMissingFromUnavailable(t *testing.T) {
	for _, test := range []struct {
		name    string
		results []EngineReferenceResolution
		status  string
	}{
		{"missing", []EngineReferenceResolution{{ID: 2}}, "missing"},
		{"wrong identity", []EngineReferenceResolution{{ID: 3, Found: true, Name: "Other engine"}}, "unavailable"},
		{"empty response", nil, "unavailable"},
		{"empty name", []EngineReferenceResolution{{ID: 2, Found: true}}, "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := NewEntryService(nil, nil, nil).WithEngineReferenceResolver(sourceEngineResolverFunc(func(context.Context, int64, []int64) ([]EngineReferenceResolution, error) {
				return test.results, nil
			}))
			result := svc.resolveSourceEngine(context.Background(), 7, models.SourceBinding{SourceModule: "meta", ObservedSnapshot: commonModels.JSONMap{"engine_id": 2}})
			if result.Status != test.status || result.Name != "" {
				t.Fatalf("result = %#v", result)
			}
		})
	}
	// Professional owners and malformed references must not trigger an owner read.
	svc := NewEntryService(nil, nil, nil).WithEngineReferenceResolver(sourceEngineResolverFunc(func(context.Context, int64, []int64) ([]EngineReferenceResolution, error) {
		t.Fatal("unexpected owner read")
		return nil, nil
	}))
	for _, source := range []models.SourceBinding{
		{SourceModule: "model", ObservedSnapshot: commonModels.JSONMap{"engine_id": 2}},
		{SourceModule: "meta", ObservedSnapshot: commonModels.JSONMap{"engine_id": "invalid"}},
		{SourceModule: "meta", ObservedSnapshot: commonModels.JSONMap{"engine_id": 0}},
	} {
		if result := svc.resolveSourceEngine(context.Background(), 7, source); result != nil {
			t.Fatalf("unexpected reference: %#v", result)
		}
	}
}

func TestProfessionalEngineReferencesResolveExactStringIDsTogether(t *testing.T) {
	svc := NewEntryService(nil, nil, nil).WithEngineReferenceResolver(sourceEngineResolverFunc(func(_ context.Context, tenant int64, ids []int64) ([]EngineReferenceResolution, error) {
		if tenant != 7 || !reflect.DeepEqual(ids, []int64{9007199254740993, 5}) {
			t.Fatalf("scope tenant=%d ids=%v", tenant, ids)
		}
		return []EngineReferenceResolution{{ID: ids[0], Found: true, Name: "业务源", EngineType: "postgresql"}, {ID: 5, Found: true, Name: "计算引擎", EngineType: "spark"}}, nil
	}))
	summary := map[string]any{"engine_id": "9007199254740993", "runtime_engine_id": "5"}
	refs := svc.resolveSummaryEngineReferences(context.Background(), 7, summary)
	if refs["engine_id"].ID != "9007199254740993" || refs["engine_id"].Name != "业务源" || refs["runtime_engine_id"].Name != "计算引擎" {
		t.Fatalf("refs=%+v", refs)
	}
	if len(summary) != 2 {
		t.Fatal("owner summary was rewritten")
	}
}
