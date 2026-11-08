package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/addp/common/dataprotection"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
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
	if err := repository.MigrateContentIndexDeliverySchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Embedding{}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MigrateSearchSchema(db); err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.Migrate(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(t.Context(), 8, "", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1}, time.Now().UTC()); err != nil {
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
		for _, model := range []any{&models.SearchDocumentOrdinal{}, &models.ContentIndexDelivery{}, &models.ContentIndexOutlet{}} {
			if err := db.Where("index_name = ?", index).Delete(model).Error; err != nil {
				t.Error(err)
			}
			var count int64
			if err := db.Model(model).Where("index_name = ?", index).Count(&count).Error; err != nil || count != 0 {
				t.Errorf("fixture residue: %d %v", count, err)
			}
		}
	})
	attrs := []interface{}{"tenant_id", "engine_id", "document_id", "search_ordinal"}
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
	svc.ordinalReady.Store(true)
	svc.searchSessions = newSearchSessionRegistry()
	read := func(t *testing.T, id string) map[string]interface{} {
		t.Helper()
		var doc map[string]interface{}
		if err := client.Index(index).GetDocumentWithContext(ctx, id, nil, &doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	t.Run("batch_has_one_receipt_and_all_documents", func(t *testing.T) {
		documents := make([]commonClient.ManagerContentDocument, commonClient.ManagerContentBatchLimit)
		for i := range documents {
			documents[i] = commonClient.ManagerContentDocument{DocumentID: fmt.Sprintf("batch-%03d", i), PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata, EngineID: 9, DataItemType: "table", Name: fmt.Sprintf("table_%03d", i)}
		}
		if err := svc.UpsertContentDocuments(ctx, 7, documents); err != nil {
			t.Fatal(err)
		}
		var ops []models.ContentIndexDelivery
		if err := db.Where("index_name = ? AND kind = ?", index, repository.IndexDeliveryWrite).Find(&ops).Error; err != nil || len(ops) != 1 || len(ops[0].DocumentIDs) != len(documents) || ops[0].Status != repository.IndexDeliverySucceeded {
			t.Fatalf("batch receipt: count=%d error=%v", len(ops), err)
		}
		inventory, err := svc.ReadContentIndexInventory(ctx, 7)
		if err != nil || len(inventory.Documents) != len(documents) {
			t.Fatalf("native inventory: %+v %v", inventory, err)
		}
		for _, identity := range inventory.Documents {
			if identity.TenantID != 7 || identity.EngineID != 9 {
				t.Fatal("native inventory crossed scope", identity)
			}
		}
		empty, err := svc.ReadContentIndexInventory(ctx, 8)
		if err != nil || len(empty.Documents) != 0 || empty.Digest == inventory.Digest {
			t.Fatal("native inventory Tenant binding", empty, err)
		}
		var afterInventory int64
		if err := db.Model(&models.ContentIndexDelivery{}).Where("index_name = ?", index).Count(&afterInventory).Error; err != nil || afterInventory != int64(len(ops)) {
			t.Fatal("inventory mutated delivery ledger", afterInventory, err)
		}
		for _, document := range documents {
			if actual := read(t, document.DocumentID); actual["name"] != document.Name {
				t.Fatalf("missing batch member %s", document.DocumentID)
			}
		}
		if err := svc.DeleteContentDocuments(ctx, 7, ContentDocumentDeleteScope{EngineID: 9}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exact_delete_batch_preserves_tenant_engine_and_sibling", func(t *testing.T) {
		docs := []commonClient.ManagerContentDocument{}
		ids := []string{}
		for i := 0; i < 5; i++ {
			id := fmt.Sprintf("delete-batch-%d", i)
			ids = append(ids, id)
			docs = append(docs, commonClient.ManagerContentDocument{DocumentID: id, PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata, EngineID: 9, DataItemType: "file", Name: id})
		}
		if err := svc.UpsertContentDocuments(ctx, 7, docs); err != nil {
			t.Fatal(err)
		}
		var before int64
		db.Model(&models.ContentIndexDelivery{}).Where("index_name = ? AND kind = ?", index, repository.IndexDeliveryDelete).Count(&before)
		if err := svc.DeleteContentDocuments(ctx, 7, ContentDocumentDeleteScope{EngineID: 10, DocumentIDs: ids[:3]}); err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			read(t, id)
		}
		if err := svc.DeleteContentDocuments(ctx, 8, ContentDocumentDeleteScope{EngineID: 9, DocumentIDs: ids[:3]}); err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			read(t, id)
		}
		if err := svc.DeleteContentDocuments(ctx, 7, ContentDocumentDeleteScope{EngineID: 9, DocumentIDs: ids[:3]}); err != nil {
			t.Fatal(err)
		}
		for _, id := range ids[:3] {
			var document map[string]interface{}
			if err := client.Index(index).GetDocumentWithContext(ctx, id, nil, &document); err == nil {
				t.Fatalf("member survived exact batch: %s", id)
			}
		}
		for _, id := range ids[3:] {
			read(t, id)
		}
		var after int64
		db.Model(&models.ContentIndexDelivery{}).Where("index_name = ? AND kind = ?", index, repository.IndexDeliveryDelete).Count(&after)
		if after-before != 3 {
			t.Fatalf("expected one delivery per request: %d", after-before)
		}
		if err := svc.DeleteContentDocuments(ctx, 7, ContentDocumentDeleteScope{EngineID: 9, DocumentIDs: ids[3:]}); err != nil {
			t.Fatal(err)
		}
	})
	for _, first := range []bool{false, true} {
		t.Run(fmt.Sprintf("content_first_%v", first), func(t *testing.T) {
			id := fmt.Sprintf("item-%v", first)
			exerciseContentSnapshots(t, svc, id, first, func() map[string]interface{} { return read(t, id) })
		})
	}
	t.Run("registered_field_matches", func(t *testing.T) {
		identity := testSearchIdentity(t, 7, 11, 80, "Outdoors")
		doc := commonClient.ManagerContentDocument{
			DocumentID: identity.ID, EngineType: identity.EngineType, Locator: identity.Locator, PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata,
			EngineID: 11, DataItemType: "collection", Name: "Outdoors", FullName: "Outdoor.Outdoors",
			Fields: []commonClient.ManagerContentField{
				{Name: "aaMembers", DataType: "array", Comment: "Participants"},
				{Name: "leader.nickname", DataType: "string", Comment: "Team participant"},
				{Name: "unmatched", DataType: "integer"},
			},
		}
		if err := svc.UpsertContentDocuments(ctx, 7, []commonClient.ManagerContentDocument{doc}); err != nil {
			t.Fatal(err)
		}
		tenantID, engineID := uint(7), uint(11)
		for _, query := range []string{"aamember", "AAMEMBER", "aamembe", "aamemebr", "participant", "string"} {
			result, err := testSearchDocuments(t, svc, ctx, tenantID, &engineID, searchVisibilityFixture{}, query, 10, "")
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
			result, err := testSearchDocuments(t, svc, ctx, scope[0], &scope[1], searchVisibilityFixture{}, "aamember", 10, "")
			if err != nil || len(result.Hits) != 0 {
				t.Fatalf("scope=%v returned unrelated fields: %+v %v", scope, result, err)
			}
		}
		if err := svc.DeleteContentDocuments(ctx, 7, ContentDocumentDeleteScope{EngineID: 11, DocumentIDs: []string{doc.DocumentID}}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("search_visibility_filters_before_ranking_and_continuation", func(t *testing.T) {
		tenantID, engineID := uint(7), uint(11)
		metadata := testSearchIdentity(t, tenantID, engineID, 84, "BudgetMeta")
		content := testSearchIdentity(t, tenantID, engineID, 85, "BudgetRead")
		hidden := testSearchIdentity(t, tenantID, engineID, 86, "BudgetHidden")
		t.Cleanup(func() {
			if err := svc.DeleteContentDocuments(ctx, tenantID, ContentDocumentDeleteScope{EngineID: engineID, DocumentIDs: []string{metadata.ID, content.ID, hidden.ID}}); err != nil {
				t.Error(err)
			}
		})
		for i, identity := range []indexedSearchIdentity{metadata, content, hidden} {
			name := []string{"BudgetMeta", "BudgetRead", "BudgetHidden"}[i]
			size := int64(99)
			technical := commonClient.ManagerContentDocument{DocumentID: identity.ID, EngineType: identity.EngineType, Locator: identity.Locator, EngineID: engineID,
				PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata, DataItemType: "collection", Name: name, FullName: "Outdoor." + name, SizeBytes: &size,
				Fields: []commonClient.ManagerContentField{{Name: "userInfo.phone", DataType: "string", Comment: "annotationtoken"}}}
			body := commonClient.ManagerContentDocument{DocumentID: identity.ID, EngineID: engineID, PayloadKind: commonClient.ManagerContentPayloadExtractedContent, DataItemType: "collection", Name: name,
				Content: "secretonly", ContentPreview: "secretonly"}
			for _, doc := range []commonClient.ManagerContentDocument{technical, body} {
				if err := svc.UpsertContentDocuments(ctx, tenantID, []commonClient.ManagerContentDocument{doc}); err != nil {
					t.Fatal(err)
				}
			}
		}
		checker := searchVisibilityFixture{classify: func(target plugin.EngineCatalogPath) (bool, bool) {
			name := target.Segments[len(target.Segments)-1].Name
			return name != "BudgetHidden", name == "BudgetRead"
		}}
		found := map[string]SearchDocument{}
		cursor := ""
		for page := 1; page <= 20; page++ {
			result, err := testSearchDocuments(t, svc, ctx, tenantID, &engineID, checker, "Budget", 1, cursor)
			if err != nil || len(result.Hits) > 1 {
				t.Fatalf("page %d: %+v %v", page, result, err)
			}
			for _, hit := range result.Hits {
				if _, duplicate := found[hit.DocumentID]; duplicate {
					t.Fatal("duplicate continued result")
				}
				found[hit.DocumentID] = hit
			}
			if result.Exhausted {
				break
			}
			if result.NextCursor == "" || page == 20 {
				t.Fatal("continuation did not finish")
			}
			cursor = result.NextCursor
		}
		if len(found) != 2 || found[metadata.ID].Name != "BudgetMeta" || found[metadata.ID].FileSize != 0 || found[metadata.ID].ContentPreview != "" || found[content.ID].ContentPreview != "secretonly" {
			t.Fatalf("wrong search projections: %+v", found)
		}
		result, err := testSearchDocuments(t, svc, ctx, tenantID, &engineID, checker, "secretonly", 10, "")
		if err != nil || len(result.Hits) != 1 || result.Hits[0].DocumentID != content.ID {
			t.Fatalf("content-only match escaped access filter: %+v %v", result, err)
		}
		result, err = testSearchDocuments(t, svc, ctx, tenantID, &engineID, checker, "phone", 10, "")
		if err != nil || len(result.Hits) != 2 {
			t.Fatalf("metadata field-name query: %+v %v", result, err)
		}
		for _, hit := range result.Hits {
			if len(hit.FieldMatches) != 1 || hit.FieldMatches[0].Name != "userInfo.phone" || hit.FieldMatches[0].Highlights["name"] != "userInfo.<mark>phone</mark>" {
				t.Fatalf("lost actual nested field-name match: %+v", hit.FieldMatches)
			}
			if hit.DocumentID == metadata.ID && (hit.ContentPreview != "" || hit.Metadata != nil || hit.FieldMatches[0].DataType != "" || hit.FieldMatches[0].Comment != "" || len(hit.FieldMatches[0].Highlights) != 1) {
				t.Fatalf("metadata-only field query disclosed content or field annotations: %+v", hit)
			}
			if hit.DocumentID == content.ID && (hit.FieldMatches[0].DataType != "string" || hit.FieldMatches[0].Comment != "annotationtoken") {
				t.Fatalf("content-readable field context changed: %+v", hit)
			}
		}
		for _, query := range []string{"annotationtoken", "string"} {
			result, err = testSearchDocuments(t, svc, ctx, tenantID, &engineID, checker, query, 10, "")
			if err != nil || len(result.Hits) != 1 || result.Hits[0].DocumentID != content.ID {
				t.Fatalf("metadata-only query searched annotations or types (%q): %+v %v", query, result, err)
			}
		}
		withdrawn := searchVisibilityFixture{classify: func(target plugin.EngineCatalogPath) (bool, bool) {
			name := target.Segments[len(target.Segments)-1].Name
			return name == "BudgetRead", name == "BudgetRead"
		}}
		result, err = testSearchDocuments(t, svc, ctx, tenantID, &engineID, withdrawn, "phone", 10, "")
		if err != nil || len(result.Hits) != 1 || result.Hits[0].DocumentID != content.ID {
			t.Fatalf("metadata withdrawal retained field-name hit: %+v %v", result, err)
		}
		profileID := uuid.NewString()
		t.Cleanup(func() {
			if err := db.Exec("DELETE FROM manager.embeddings WHERE tenant_id = ? AND model_profile_id = ?", tenantID, profileID).Error; err != nil {
				t.Error(err)
			}
		})
		for i, identity := range []indexedSearchIdentity{content, hidden} {
			// The hidden vector is an exact match. It must not consume top-1.
			coordinates := []string{"1,1", "1,0"}[i]
			vector := "[" + coordinates + strings.Repeat(",0", 2558) + "]"
			if err := db.Exec(`INSERT INTO manager.embeddings (tenant_id,item_fingerprint,item_id,engine_id,locator,source_version,embedding,model_profile_id,profile_version,deployment_id,dimension,status,created_at,updated_at)
				VALUES (?, ?, ?, ?, ?, 'test', ?::manager.vector, ?, 1, ?, 2560, 'ready', now(), now())`, tenantID, identity.ID, 85+i, engineID, identity.Locator, vector, profileID, uuid.NewString()).Error; err != nil {
				t.Fatal(err)
			}
		}
		queryVector := make([]float32, 2560)
		queryVector[0] = 1
		svc.vectorRepo = repository.NewEmbeddingRepository(db)
		session := &searchSession{service: svc, tenant: tenantID, nonce: uuid.New(), vectorQuery: &repository.SearchVectorQuery{TenantID: tenantID, EngineID: &engineID, Vector: queryVector, ModelProfileID: profileID, ProfileVersion: 1, Dimension: 2560}}
		if err := svc.searchSessions.create(session); err != nil {
			t.Fatal(err)
		}
		defer session.close()
		step, err := session.newStep("addp_at_test", checker)
		if err != nil {
			t.Fatal(err)
		}
		defer step.finish()
		stream := &searchAuthorizedStream{session: session, raw: &searchRawVector{session: session}}
		first, ok, err := stream.peek(ctx, step)
		expected, _ := decodeSearchIdentity(content.ID)
		if err != nil || !ok || first.ID != expected {
			t.Fatalf("unauthorized vector affected top-1: %+v %v %v", first, ok, err)
		}
	})
	t.Run("protection_purge_preserves_technical_projection", func(t *testing.T) {
		identity := testSearchIdentity(t, 7, 9, 81, "Persons")
		id := identity.ID
		technical := commonClient.ManagerContentDocument{DocumentID: id, EngineType: identity.EngineType, Locator: identity.Locator, PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata, EngineID: 9, DataItemType: "collection", Name: "Persons", FullName: "Outdoor.Persons", Fields: []commonClient.ManagerContentField{{Name: "userInfo.phone", DataType: "string"}}, ProjectionTime: time.Now().UTC()}
		if err := svc.UpsertContentDocuments(ctx, 7, []commonClient.ManagerContentDocument{technical}); err != nil {
			t.Fatal(err)
		}
		content := commonClient.ManagerContentDocument{DocumentID: id, PayloadKind: commonClient.ManagerContentPayloadExtractedContent, EngineID: 9, DataItemType: "collection", Name: "Persons", Content: "sensitive sentinel", Title: "sensitive title", Metadata: map[string]interface{}{"secret": "sensitive metadata"}}
		if err := svc.UpsertContentDocuments(ctx, 7, []commonClient.ManagerContentDocument{content}); err != nil {
			t.Fatal(err)
		}
		// Unknown historical fields and undeclared nested field values must disappear.
		task, err := client.Index(index).UpdateDocumentsWithContext(ctx, []map[string]interface{}{{"id": id, "old_unknown": "sensitive old field", "fields": []map[string]interface{}{{"name": "userInfo.phone", "data_type": "string", "sample_value": "sensitive nested value"}}}}, nil)
		wait(ctx, task, err)
		before := read(t, id)
		for range 2 {
			if err := db.Transaction(func(tx *gorm.DB) error {
				return repo.QueueProtectionPurges(ctx, tx, 7, []string{id, "missing-protected"})
			}); err != nil {
				t.Fatal(err)
			}
			// A new startup generation must finish the queued purge without payload storage.
			svc.epoch, err = repo.Open(ctx, svc.endpointID, true)
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 100; attempt++ {
				err = svc.reconcileContentDeliveries(ctx)
				if err == nil {
					break
				}
				if !errors.Is(err, repository.ErrContentIndexIsolated) {
					t.Fatal(err)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err != nil {
				t.Fatal(err)
			}
			after := read(t, id)
			for key := range contentSnapshotPayload(7, technical) {
				if key == "fields" {
					continue
				}
				if !reflect.DeepEqual(before[key], after[key]) {
					t.Fatalf("technical field %s changed", key)
				}
			}
			for _, key := range []string{"content", "content_preview", "metadata", "title", "old_unknown", "content_hash", "content_projection_time"} {
				if _, ok := after[key]; ok {
					t.Fatalf("purge retained %s", key)
				}
			}
			fields := after["fields"].([]interface{})
			if len(fields) != 1 || !reflect.DeepEqual(fields[0], map[string]interface{}{"name": "userInfo.phone", "data_type": "string"}) {
				t.Fatal("nested technical field retained a value")
			}
			var absent map[string]interface{}
			if err := client.Index(index).GetDocumentWithContext(ctx, "missing-protected", nil, &absent); err == nil {
				t.Fatal("purge created missing-document placeholder")
			}
			tenantID := uint(7)
			result, err := testSearchDocuments(t, svc, ctx, tenantID, nil, searchVisibilityFixture{}, "userInfo.phone", 10, "")
			if err != nil || len(result.Hits) != 1 || result.Hits[0].DocumentID != id {
				t.Fatalf("technical search lost after purge: %+v %v", result, err)
			}
			result, err = testSearchDocuments(t, svc, ctx, tenantID, nil, searchVisibilityFixture{}, "sensitive", 10, "")
			if err != nil || len(result.Hits) != 0 {
				t.Fatalf("body remains searchable after purge: %+v %v", result, err)
			}
		}
		if err := svc.DeleteContentDocuments(ctx, 7, ContentDocumentDeleteScope{EngineID: 9, DocumentIDs: []string{id}}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("purge_content_only_does_not_invent_technical_facts", func(t *testing.T) {
		id := "content-only"
		body := commonClient.ManagerContentDocument{DocumentID: id, PayloadKind: commonClient.ManagerContentPayloadExtractedContent, EngineID: 9, DataItemType: "object", Name: "body context", Content: "sensitive content only"}
		if err := svc.UpsertContentDocuments(ctx, 7, []commonClient.ManagerContentDocument{body}); err != nil {
			t.Fatal(err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error { return repo.QueueProtectionPurges(ctx, tx, 7, []string{id}) }); err != nil {
			t.Fatal(err)
		}
		for attempt := 0; attempt < 100; attempt++ {
			if err := svc.reconcileContentDeliveries(ctx); err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		doc := read(t, id)
		if !reflect.DeepEqual(doc, map[string]interface{}{"id": id, "document_id": id, "tenant_id": float64(7), "engine_id": float64(9), "search_ordinal": doc["search_ordinal"]}) {
			t.Fatalf("purge invented technical facts: %+v", doc)
		}
		if err := svc.DeleteContentDocuments(ctx, 7, ContentDocumentDeleteScope{EngineID: 9, DocumentIDs: []string{id}}); err != nil {
			t.Fatal(err)
		}
	})
	// A successful complete snapshot replaces the nested metadata object as well.
	content := commonClient.ManagerContentDocument{DocumentID: "item-false", PayloadKind: commonClient.ManagerContentPayloadExtractedContent, EngineID: 9, DataItemType: "object", Name: "context", Metadata: map[string]interface{}{"old": "obsolete", "kept": "old"}}
	if err := svc.UpsertContentDocuments(ctx, 7, []commonClient.ManagerContentDocument{content}); err != nil {
		t.Fatal(err)
	}
	content.Metadata = map[string]interface{}{"kept": "current"}
	if err := svc.UpsertContentDocuments(ctx, 7, []commonClient.ManagerContentDocument{content}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read(t, "item-false")["metadata"], map[string]interface{}{"kept": "current"}) {
		t.Fatal("metadata object retained obsolete fields")
	}
	// Exact deletion preserves the sibling snapshot and remains idempotent.
	before := read(t, "item-true")
	for range 2 {
		if err := svc.DeleteContentDocuments(ctx, 7, ContentDocumentDeleteScope{EngineID: 9, DocumentIDs: []string{"item-false"}}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(before, read(t, "item-true")) {
		t.Fatal("exact deletion changed sibling")
	}
	t.Run("bounded_cross_window_stream_replay_and_material_invalidation", func(t *testing.T) {
		const hiddenCount = 1100
		const allowedCount = 30
		docs := make([]commonClient.ManagerContentDocument, 0, hiddenCount+allowedCount)
		ids := make([]string, 0, cap(docs))
		expected := []string{}
		for i := 0; i < hiddenCount+allowedCount; i++ {
			identity := testSearchIdentity(t, 7, 11, uint(10000+i), fmt.Sprintf("Needle%04d", i))
			ids = append(ids, identity.ID)
			if i >= hiddenCount {
				expected = append(expected, identity.ID)
			}
			docs = append(docs, commonClient.ManagerContentDocument{DocumentID: identity.ID, EngineType: identity.EngineType, Locator: identity.Locator, EngineID: 11, PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata, DataItemType: "collection", Name: "needle", FullName: "Outdoor." + fmt.Sprintf("Needle%04d", i)})
		}
		for start := 0; start < len(docs); start += commonClient.ManagerContentBatchLimit {
			if err := svc.UpsertContentDocuments(ctx, 7, docs[start:min(start+commonClient.ManagerContentBatchLimit, len(docs))]); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() {
			for start := 0; start < len(ids); start += commonClient.ManagerContentBatchLimit {
				if err := svc.DeleteContentDocuments(ctx, 7, ContentDocumentDeleteScope{EngineID: 11, DocumentIDs: ids[start:min(start+commonClient.ManagerContentBatchLimit, len(ids))]}); err != nil {
					t.Error(err)
				}
			}
		})
		var ordinalRows []models.SearchDocumentOrdinal
		if err := db.Where("index_name=? AND document_id IN ?", index, expected).Order("search_ordinal").Find(&ordinalRows).Error; err != nil {
			t.Fatal(err)
		}
		expected = expected[:0]
		for _, row := range ordinalRows {
			expected = append(expected, row.DocumentID)
		}
		original, err := client.Index(index).GetPaginationWithContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		task, err := client.Index(index).UpdatePaginationWithContext(ctx, &meilisearch.Pagination{MaxTotalHits: 100})
		wait(ctx, task, err)
		t.Cleanup(func() {
			task, err := client.Index(index).UpdatePaginationWithContext(ctx, original)
			wait(ctx, task, err)
		})
		checks := 0
		checker := searchVisibilityFixture{classify: func(target plugin.EngineCatalogPath) (bool, bool) {
			checks++
			name := target.Segments[len(target.Segments)-1].Name
			visible := name >= fmt.Sprintf("Needle%04d", hiddenCount)
			return visible, visible
		}}
		var before int64
		if err := db.Model(&models.ContentIndexDelivery{}).Where("index_name=?", index).Count(&before).Error; err != nil {
			t.Fatal(err)
		}
		cursor := ""
		found := []string{}
		empty := 0
		steps := 0
		for ; steps < 100; steps++ {
			input := cursor
			batch, err := testSearchDocuments(t, svc, ctx, 7, nil, checker, "needle", 7, cursor)
			if err != nil {
				t.Fatal(err)
			}
			if len(batch.Hits) == 0 && !batch.Exhausted {
				empty++
			}
			for _, hit := range batch.Hits {
				found = append(found, hit.DocumentID)
			}
			if steps == 1 && input != "" {
				replay, err := testSearchDocuments(t, svc, ctx, 7, nil, checker, "needle", 7, input)
				if err != nil || !reflect.DeepEqual(searchIDs(batch), searchIDs(replay)) || batch.NextCursor != replay.NextCursor || batch.Exhausted != replay.Exhausted {
					t.Fatalf("native replay differs: %+v %+v %v", batch, replay, err)
				}
			}
			if batch.Exhausted {
				break
			}
			if batch.NextCursor == "" {
				t.Fatal("budget was treated as EOF")
			}
			cursor = batch.NextCursor
		}
		if steps == 100 || empty == 0 || !reflect.DeepEqual(found, expected) {
			t.Fatalf("cross-window lost/order changed: steps=%d empty=%d found=%v expected=%v", steps, empty, found, expected)
		}
		var after int64
		if err := db.Model(&models.ContentIndexDelivery{}).Where("index_name=?", index).Count(&after).Error; err != nil || after != before {
			t.Fatal("query wrote indexing/query state", before, after, err)
		}
		if checks >= 3*len(docs) {
			t.Fatalf("candidate authorization unexpectedly replays source: checks=%d docs=%d", checks, len(docs))
		}
		t.Logf("native window=100 docs=%d authorized=%d continuation_steps=%d empty_batches=%d checked_targets=%d retained_bytes=%d", len(docs), len(found), steps+1, empty, checks, svc.searchSessions.bytes)
		begun, err := testSearchDocuments(t, svc, ctx, 7, nil, checker, "needle", 7, "")
		if err != nil || begun.NextCursor == "" {
			t.Fatalf("new stream: %+v %v", begun, err)
		}
		// An indexing update changes the common material basis; never partially re-rank.
		if err := svc.UpsertContentDocuments(ctx, 7, docs[:1]); err != nil {
			t.Fatal(err)
		}
		if changed, err := testSearchDocuments(t, svc, ctx, 7, nil, checker, "needle", 7, begun.NextCursor); changed != nil || !errors.Is(err, ErrSearchContextChanged) {
			t.Fatalf("changed material continued: %+v %v", changed, err)
		}
	})
	stats, err := client.Index(index).GetStatsWithContext(ctx, nil)
	if err != nil || stats.NumberOfDocuments != 1 {
		t.Fatalf("unexpected document count after exact delete: %+v %v", stats, err)
	}
	// Losing the index permits only this new write, not historical recovery.
	task, err = client.DeleteIndexWithContext(ctx, index)
	wait(ctx, task, err)
	recreated := commonClient.ManagerContentDocument{DocumentID: "after-index-loss", PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata, EngineID: 9, DataItemType: "collection", Name: "new-collection"}
	if err := svc.UpsertContentDocuments(ctx, 7, []commonClient.ManagerContentDocument{recreated}); err != nil {
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
