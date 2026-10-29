package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

func TestContentIndexInventoryCompleteness(t *testing.T) {
	for _, scenario := range []string{"complete", "empty", "duplicate", "wrong_tenant", "fractional_engine", "total_changed", "short_empty_page", "rapid_completed_write", "pending"} {
		t.Run(scenario, func(t *testing.T) {
			var db *gorm.DB
			calls := 0
			svc, database := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/indexes/content/documents/fetch" {
					t.Errorf("unexpected mutation/route: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
					return
				}
				var query struct {
					Offset int
					Limit  int
					Fields []string
					Filter string
				}
				if json.NewDecoder(r.Body).Decode(&query) != nil || query.Filter != "tenant_id = 7" || query.Limit != 256 || len(query.Fields) != 4 {
					t.Error("unbounded or unscoped inventory request")
				}
				total := 258
				if scenario == "empty" {
					total = 0
				}
				rows := []map[string]any{}
				for i := query.Offset; i < total && i < query.Offset+query.Limit; i++ {
					id := fmt.Sprintf("doc-%d", i)
					tenant := 7
					var engine any = uint64(9007199254740993)
					if scenario == "duplicate" && i == 257 {
						id = "doc-0"
					}
					if scenario == "wrong_tenant" {
						tenant = 8
					}
					if scenario == "fractional_engine" {
						engine = 1.5
					}
					rows = append(rows, map[string]any{"id": id, "document_id": id, "tenant_id": tenant, "engine_id": engine})
				}
				if scenario == "total_changed" && query.Offset > 0 {
					total++
				}
				if scenario == "short_empty_page" && query.Offset > 0 {
					rows = nil
				}
				if scenario == "rapid_completed_write" && calls == 1 {
					uid := int64(1)
					err := db.Create(&models.ContentIndexDelivery{ID: "concurrent-operation", IndexName: "content", TenantID: 7, DocumentID: "another", DocumentIDs: []string{"another"}, Kind: "write", Status: "succeeded", EndpointID: "endpoint", TaskCorrelation: "concurrent-operation", TaskUID: &uid, TaskEnqueuedAt: time.Now().UTC().Format(time.RFC3339Nano)}).Error
					if err != nil {
						t.Error(err)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"results": rows, "offset": query.Offset, "limit": query.Limit, "total": total})
			}))
			db = database
			if scenario == "pending" {
				if err := db.Create(&models.ContentIndexDelivery{ID: "pending-operation", IndexName: "content", TenantID: 7, Kind: "write", Status: "unknown", DocumentIDs: []string{}}).Error; err != nil {
					t.Fatal(err)
				}
			}
			got, err := svc.ReadContentIndexInventory(t.Context(), 7)
			if scenario == "complete" || scenario == "empty" {
				if err != nil || got == nil || len(got.Digest) != 64 {
					t.Fatalf("inventory=%+v err=%v", got, err)
				}
				if scenario == "complete" && (len(got.Documents) != 258 || got.Documents[0].EngineID != uint(9007199254740993) || calls != 2) {
					t.Fatal("truncated identities or numeric precision loss")
				}
				var mutations int64
				db.Model(&models.ContentIndexDelivery{}).Count(&mutations)
				if mutations != 0 {
					t.Fatal("scan wrote delivery facts")
				}
			} else if err == nil || got != nil {
				t.Fatal("unverified inventory returned")
			}
			if scenario == "pending" && calls != 0 {
				t.Fatal("read index with unresolved delivery")
			}
		})
	}
}
