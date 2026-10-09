package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"github.com/meilisearch/meilisearch-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerContentSnapshots(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Manager PostgreSQL gate")
	}
	endpoint, key := os.Getenv("MANAGER_MEILISEARCH_TEST_URL"), os.Getenv("MANAGER_MEILISEARCH_TEST_KEY")
	if endpoint == "" || key == "" {
		t.Fatal("owned Meilisearch fixture required")
	}
	db, err := gorm.Open(postgres.Open(os.Getenv("MANAGER_POSTGRES_TEST_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ContentIndexOutlet{}, &models.ContentIndexDelivery{}); err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.Migrate(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := meilisearch.New(endpoint, meilisearch.WithAPIKey(key), meilisearch.WithCustomClient(&http.Client{Timeout: 10 * time.Second}), meilisearch.DisableRetries())
	index := "snapshot-test-" + uuid.NewString()
	wait := func(ctx context.Context, task *meilisearch.TaskInfo, err error) {
		t.Helper()
		if err != nil || task == nil {
			t.Fatalf("fixture task: %v", err)
		}
		final, err := client.WaitForTaskWithContext(ctx, task.TaskUID, 20*time.Millisecond)
		if err != nil || final == nil || final.Status != meilisearch.TaskStatusSucceeded {
			t.Fatalf("fixture task terminal: %+v %v", final, err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	task, err := client.CreateIndexWithContext(ctx, &meilisearch.IndexConfig{Uid: index, PrimaryKey: "id"})
	wait(ctx, task, err)
	t.Cleanup(func() {
		clean, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		task, err := client.DeleteIndexWithContext(clean, index)
		if err != nil || task == nil {
			t.Errorf("delete fixture index: %v", err)
		} else {
			final, err := client.WaitForTaskWithContext(clean, task.TaskUID, 20*time.Millisecond)
			if err != nil || final == nil || final.Status != meilisearch.TaskStatusSucceeded {
				t.Errorf("delete fixture index terminal: %+v %v", final, err)
			}
		}
		for _, model := range []any{&models.ContentIndexDelivery{}, &models.ContentIndexOutlet{}} {
			if err := db.Where("index_name = ?", index).Delete(model).Error; err != nil {
				t.Error(err)
			}
			var count int64
			if err := db.Model(model).Where("index_name = ?", index).Count(&count).Error; err != nil || count != 0 {
				t.Errorf("fixture residue: %d %v", count, err)
			}
		}
	})
	attrs := []interface{}{"tenant_id", "engine_id", "document_id"}
	task, err = client.Index(index).UpdateFilterableAttributesWithContext(ctx, &attrs)
	wait(ctx, task, err)
	repo := repository.NewContentIndexDeliveryRepository(db, index)
	epoch, err := repo.Open(ctx, contentIndexEndpointID(endpoint), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateIfSettled(ctx, epoch); err != nil {
		t.Fatal(err)
	}
	svc := &HybridSearchService{client: client, contentIndex: index, enabled: true, deliveries: repo, protectionStore: store, epoch: epoch, endpointID: contentIndexEndpointID(endpoint), log: slog.Default()}
	svc.initialized.Store(true)
	read := func(t *testing.T, id string) map[string]interface{} {
		t.Helper()
		var doc map[string]interface{}
		if err := client.Index(index).GetDocumentWithContext(ctx, id, nil, &doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	for _, first := range []bool{false, true} {
		t.Run(fmt.Sprintf("content_first_%v", first), func(t *testing.T) {
			id := fmt.Sprintf("item-%v", first)
			exerciseContentSnapshots(t, svc, id, first, func() map[string]interface{} { return read(t, id) })
		})
	}
	t.Run("registered_field_matches", func(t *testing.T) {
		doc := commonClient.ManagerContentDocument{
			DocumentID: "field-search", PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata,
			EngineID: 11, DataItemType: "collection", Name: "Outdoors", FullName: "Outdoor.Outdoors",
			Fields: []commonClient.ManagerContentField{
				{Name: "aaMembers", DataType: "array", Comment: "Participants"},
				{Name: "leader.nickname", DataType: "string", Comment: "Team participant"},
				{Name: "unmatched", DataType: "integer"},
			},
		}
		if err := svc.UpsertContentDocument(ctx, 7, doc); err != nil {
			t.Fatal(err)
		}
		tenantID, engineID := uint(7), uint(11)
		for _, query := range []string{"aamember", "AAMEMBER", "aamembe", "aamemebr", "participant", "string"} {
			result, err := svc.SearchDocuments(ctx, &tenantID, &engineID, query, 1, 10)
			if err != nil || len(result.Hits) != 1 || len(result.Hits[0].FieldMatches) == 0 {
				t.Fatalf("query=%q result=%+v error=%v", query, result, err)
			}
			for _, field := range result.Hits[0].FieldMatches {
				if field.Name == "unmatched" {
					t.Fatalf("unmatched field reported for %q", query)
				}
			}
			if query != "participant" && query != "string" && (len(result.Hits[0].FieldMatches) != 1 || result.Hits[0].FieldMatches[0].Name != "aaMembers" || result.Hits[0].FieldMatches[0].DataType != "array") {
				t.Fatalf("query=%q lost the actual field definition: %+v", query, result.Hits[0].FieldMatches)
			}
		}
		for _, scope := range [][2]uint{{8, 11}, {7, 12}} {
			result, err := svc.SearchDocuments(ctx, &scope[0], &scope[1], "aamember", 1, 10)
			if err != nil || result.Total != 0 || len(result.Hits) != 0 {
				t.Fatalf("scope=%v returned unrelated fields: %+v %v", scope, result, err)
			}
		}
		if err := svc.DeleteContentDocuments(ctx, 7, ContentDocumentDeleteScope{EngineID: 11, DocumentID: doc.DocumentID}); err != nil {
			t.Fatal(err)
		}
	})
	// A successful complete snapshot replaces the nested metadata object as well.
	content := commonClient.ManagerContentDocument{DocumentID: "item-false", PayloadKind: commonClient.ManagerContentPayloadExtractedContent, EngineID: 9, DataItemType: "object", Name: "context", Metadata: map[string]interface{}{"old": "obsolete", "kept": "old"}}
	if err := svc.UpsertContentDocument(ctx, 7, content); err != nil {
		t.Fatal(err)
	}
	content.Metadata = map[string]interface{}{"kept": "current"}
	if err := svc.UpsertContentDocument(ctx, 7, content); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read(t, "item-false")["metadata"], map[string]interface{}{"kept": "current"}) {
		t.Fatal("metadata object retained obsolete fields")
	}
	// Exact deletion preserves the sibling snapshot and remains idempotent.
	before := read(t, "item-true")
	for range 2 {
		if err := svc.DeleteContentDocuments(ctx, 7, ContentDocumentDeleteScope{EngineID: 9, DocumentID: "item-false"}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(before, read(t, "item-true")) {
		t.Fatal("exact deletion changed sibling")
	}
	stats, err := client.Index(index).GetStatsWithContext(ctx, nil)
	if err != nil || stats.NumberOfDocuments != 1 {
		t.Fatalf("unexpected document count after exact delete: %+v %v", stats, err)
	}
	// Losing the index permits only this new write, not historical recovery.
	task, err = client.DeleteIndexWithContext(ctx, index)
	wait(ctx, task, err)
	recreated := commonClient.ManagerContentDocument{DocumentID: "after-index-loss", PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata, EngineID: 9, DataItemType: "collection", Name: "new-collection"}
	if err := svc.UpsertContentDocument(ctx, 7, recreated); err != nil {
		t.Fatal(err)
	}
	if read(t, recreated.DocumentID)["name"] != recreated.Name {
		t.Fatal("missing new document after index loss")
	}
	info, err := client.GetIndexWithContext(ctx, index)
	if err != nil || info.PrimaryKey != "id" {
		t.Fatalf("recreated index identity: %+v %v", info, err)
	}
	stats, err = client.Index(index).GetStatsWithContext(ctx, nil)
	if err != nil || stats.NumberOfDocuments != 1 {
		t.Fatalf("index loss restored unexpected historical content: %+v %v", stats, err)
	}
	var unresolved int64
	if err := db.Model(&models.ContentIndexDelivery{}).Where("index_name = ? AND status <> ?", index, repository.IndexDeliverySucceeded).Count(&unresolved).Error; err != nil || unresolved != 0 {
		t.Fatalf("unresolved delivery: %d %v", unresolved, err)
	}
}
