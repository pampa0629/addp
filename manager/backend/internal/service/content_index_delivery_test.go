package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/testfixture"
	"github.com/meilisearch/meilisearch-go"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func contentDeliveryServiceFixture(t *testing.T, handler http.Handler) (*HybridSearchService, *gorm.DB) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	testfixture.ContentIndexSQLite(t, db)
	repo := repository.NewContentIndexDeliveryRepository(db, "content")
	store, err := projectionstore.Migrate(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := repo.Open(t.Context(), contentIndexEndpointID(server.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateIfSettled(t.Context(), epoch); err != nil {
		t.Fatal(err)
	}
	svc := &HybridSearchService{client: meilisearch.New(server.URL, meilisearch.WithCustomClient(&http.Client{Timeout: time.Second}), meilisearch.DisableRetries()), contentIndex: "content", enabled: true, deliveries: repo, protectionStore: store, epoch: epoch, endpointID: contentIndexEndpointID(server.URL), log: slog.Default()}
	svc.initialized.Store(true)
	return svc, db
}

func contentDeliveryDocument() commonClient.ManagerContentDocument {
	return commonClient.ManagerContentDocument{DocumentID: "item", PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata, EngineID: 9, DataItemType: "table", Name: "persons"}
}

func TestContentDeliveryTimeoutRecoversKnownTaskWithoutResending(t *testing.T) {
	var writes atomic.Int32
	var complete atomic.Bool
	var correlation string
	at := "2026-10-04T00:00:00.123456789Z"
	svc, db := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/indexes/content/documents":
			writes.Add(1)
			correlation = req.URL.Query().Get("customMetadata")
			if correlation == "" {
				t.Error("missing pre-submission correlation")
			}
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, `{"taskUid":0,"indexUid":"content","type":"documentAdditionOrUpdate","status":"enqueued","enqueuedAt":%q}`, at)
		case req.URL.Path == "/tasks/0":
			status := "processing"
			if complete.Load() {
				status = "succeeded"
			}
			fmt.Fprintf(w, `{"uid":0,"indexUid":"content","type":"documentAdditionOrUpdate","status":%q,"enqueuedAt":%q,"customMetadata":%q}`, status, at, correlation)
		default:
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
			w.WriteHeader(500)
		}
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	if err := svc.UpsertContentDocument(ctx, 7, contentDeliveryDocument()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("enqueue treated as success: %v", err)
	}
	var op models.ContentIndexDelivery
	if err := db.First(&op).Error; err != nil {
		t.Fatal(err)
	}
	if op.Status != repository.IndexDeliverySubmitted || op.TaskUID == nil || *op.TaskUID != 0 {
		t.Fatalf("receipt not durable: %#v", op)
	}
	// New startup generation resumes polling; no original payload is available.
	next, err := svc.deliveries.Open(t.Context(), svc.endpointID, true)
	if err != nil {
		t.Fatal(err)
	}
	svc.epoch = next
	complete.Store(true)
	if err := svc.reconcileContentDeliveries(t.Context()); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.deliveries.Get(t.Context(), 7, op.ID)
	if err != nil || stored.Status != repository.IndexDeliverySucceeded || writes.Load() != 1 {
		t.Fatalf("recovery replayed write: %#v, writes=%d err=%v", stored, writes.Load(), err)
	}
	if err := svc.deliveries.RequireActive(t.Context(), next); err != nil {
		t.Fatal(err)
	}
}

func TestContentDeliveryLostResponseStaysUnknownAndCanAcknowledgeIsolation(t *testing.T) {
	var requests atomic.Int32
	var lookups atomic.Int32
	svc, db := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/tasks" {
			lookups.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"results":[],"total":0,"next":null}`)
			return
		}
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	if err := svc.UpsertContentDocument(t.Context(), 7, contentDeliveryDocument()); err == nil {
		t.Fatal("lost response treated as success")
	}
	var op models.ContentIndexDelivery
	if err := db.First(&op).Error; err != nil {
		t.Fatal(err)
	}
	if op.Status != repository.IndexDeliveryUnknown || op.TaskUID != nil {
		t.Fatalf("unknown receipt: %#v", op)
	}
	if err := svc.reconcileContentDeliveries(t.Context()); !errors.Is(err, repository.ErrContentIndexIsolated) {
		t.Fatal(err)
	}
	if requests.Load() != 1 || lookups.Load() != 1 {
		t.Fatal("unknown write was resubmitted")
	}
	if err := svc.ReadyToAcknowledge(t.Context(), 7, "cursor"); err != nil {
		t.Fatal(err)
	}
	state, err := svc.deliveries.State(t.Context(), svc.epoch)
	if err != nil || !state.Isolated {
		t.Fatal("isolation not durable")
	}
	tenant := uint(7)
	if _, err := svc.SearchDocuments(t.Context(), &tenant, nil, "persons", 1, 10); !errors.Is(err, ErrSearchIsolated) {
		t.Fatal("isolated index queried")
	}
	if requests.Load() != 1 {
		t.Fatal("isolated search contacted upstream")
	}
	if err := svc.UpsertContentDocument(t.Context(), 7, contentDeliveryDocument()); err == nil {
		t.Fatal("isolated outlet accepted new write")
	}
}

func TestContentDeliveryRejectsReusedTaskIdentityAndDifferentEndpoint(t *testing.T) {
	at := "2026-10-04T00:00:00Z"
	svc, db := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, `{"taskUid":1,"indexUid":"content","type":"documentAdditionOrUpdate","enqueuedAt":%q}`, at)
			return
		}
		fmt.Fprint(w, `{"uid":1,"indexUid":"content","type":"documentAdditionOrUpdate","status":"succeeded","enqueuedAt":"2026-10-04T00:00:01Z"}`)
	}))
	if err := svc.UpsertContentDocument(t.Context(), 7, contentDeliveryDocument()); err == nil {
		t.Fatal("reused UID counted as success")
	}
	var op models.ContentIndexDelivery
	if err := db.First(&op).Error; err != nil {
		t.Fatal(err)
	}
	if op.Status != repository.IndexDeliverySubmitted {
		t.Fatal("identity mismatch changed durable result")
	}
	svc.endpointID = "another-endpoint"
	if err := svc.pollContentDelivery(t.Context(), &op); !errors.Is(err, repository.ErrContentIndexIsolated) {
		t.Fatal("different endpoint accepted old UID")
	}
}

func TestContentSearchDiscardsResultsIfProtectionFencesDuringExternalRead(t *testing.T) {
	var db *gorm.DB
	var svc *HybridSearchService
	var requests atomic.Int32
	installErrors := make(chan error, 1)
	svc, db = contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		err := db.Transaction(func(tx *gorm.DB) error {
			return svc.deliveries.QueueProtectionPurges(req.Context(), tx, 7, []string{"item"})
		})
		installErrors <- err
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"hits":[{"id":"item","name":"old-content"}],"estimatedTotalHits":1}`)
	}))
	tenant := uint(7)
	result, err := svc.SearchDocuments(t.Context(), &tenant, nil, "persons", 1, 10)
	if !errors.Is(err, ErrSearchIsolated) || result != nil || requests.Load() != 1 {
		t.Fatalf("old external result escaped fence: %#v %v", result, err)
	}
	if err := <-installErrors; err != nil {
		t.Fatal(err)
	}
}

func TestContentDeliveryCanceledCallerStillPersistsReceipt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	svc, db := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { t.Error("receipt persistence called upstream") }))
	op := &models.ContentIndexDelivery{TenantID: 7, DocumentID: "item", Kind: repository.IndexDeliveryWrite}
	if err := db.Transaction(func(tx *gorm.DB) error { return svc.deliveries.Register(t.Context(), tx, svc.epoch, op) }); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := svc.recordContentReceipt(ctx, op, &meilisearch.TaskInfo{TaskUID: 5, IndexUID: "content", Type: meilisearch.TaskTypeDocumentAdditionOrUpdate, EnqueuedAt: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.First(op).Error; err != nil {
		t.Fatal(err)
	}
	if op.Status != repository.IndexDeliverySubmitted || op.TaskUID == nil || *op.TaskUID != 5 {
		t.Fatal("canceled caller lost received UID")
	}
}

func TestContentDeliveryPurgeWaitsForActualLateWriteAndDeletion(t *testing.T) {
	var writes, deletes atomic.Int32
	var complete atomic.Bool
	var writeCorrelation, deleteCorrelation string
	at := "2026-10-04T00:00:00Z"
	svc, db := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/indexes/content/documents":
			writes.Add(1)
			writeCorrelation = req.URL.Query().Get("customMetadata")
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, `{"taskUid":1,"indexUid":"content","type":"documentAdditionOrUpdate","enqueuedAt":%q}`, at)
		case "/tasks/1":
			status := "processing"
			if complete.Load() {
				status = "succeeded"
			}
			fmt.Fprintf(w, `{"uid":1,"indexUid":"content","type":"documentAdditionOrUpdate","status":%q,"enqueuedAt":%q,"customMetadata":%q}`, status, at, writeCorrelation)
		case "/indexes/content/documents/delete":
			deletes.Add(1)
			deleteCorrelation = req.URL.Query().Get("customMetadata")
			if deleteCorrelation == "" || deleteCorrelation == writeCorrelation {
				t.Error("purge lacks distinct correlation")
			}
			var body map[string]string
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body["filter"] != "tenant_id = 7 AND document_id = 'item'" {
				t.Errorf("unsafe purge filter: %#v %v", body, err)
			}
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, `{"taskUid":2,"indexUid":"content","type":"documentDeletion","enqueuedAt":%q}`, at)
		case "/tasks/2":
			fmt.Fprintf(w, `{"uid":2,"indexUid":"content","type":"documentDeletion","status":"succeeded","enqueuedAt":%q,"customMetadata":%q}`, at, deleteCorrelation)
		default:
			t.Errorf("unexpected request %s", req.URL.Path)
			w.WriteHeader(500)
		}
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	if err := svc.UpsertContentDocument(ctx, 7, contentDeliveryDocument()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return svc.deliveries.QueueProtectionPurges(t.Context(), tx, 7, []string{"item"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.reconcileContentDeliveries(t.Context()); !errors.Is(err, repository.ErrContentIndexIsolated) {
		t.Fatal(err)
	}
	if deletes.Load() != 0 {
		t.Fatal("purge overtook pending write")
	}
	if err := svc.ReadyToAcknowledge(t.Context(), 7, "cursor"); err != nil {
		t.Fatal("fenced outlet blocked other capabilities")
	}
	complete.Store(true)
	if err := svc.reconcileContentDeliveries(t.Context()); err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 1 || deletes.Load() != 1 {
		t.Fatalf("recovery writes/deletes %d/%d", writes.Load(), deletes.Load())
	}
	if err := svc.deliveries.RequireActive(t.Context(), svc.epoch); err != nil {
		t.Fatal(err)
	}
}

func TestContentDeliveryOptionalAndUnavailableIndexDoesNotFailManagerConstruction(t *testing.T) {
	svc, _ := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	disabled, err := NewHybridSearchService(&config.Config{MeilisearchManagerContentIndex: "content"}, nil, nil, nil, nil, svc.deliveries, svc.protectionStore)
	if err != nil || disabled.Enabled() {
		t.Fatalf("optional index: %v", err)
	}
	if err := disabled.ReadyToAcknowledge(t.Context(), 7, "cursor"); err != nil {
		t.Fatal(err)
	}
	// The endpoint is configured but temporarily unavailable: remain fenced,
	// rather than failing the Manager process or pretending deletion succeeded.
	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	t.Cleanup(unavailable.Close)
	configured, err := NewHybridSearchService(&config.Config{MeilisearchManagerContentIndex: "content", MeilisearchURL: unavailable.URL}, nil, nil, nil, nil, svc.deliveries, svc.protectionStore)
	if err != nil || configured.initialized.Load() {
		t.Fatalf("unavailable index construction: %v", err)
	}
	if err := configured.deliveries.RequireActive(t.Context(), configured.epoch); !errors.Is(err, repository.ErrContentIndexIsolated) {
		t.Fatal("unavailable index became active")
	}
}

func TestContentDeliveryLostResponseRecoversOriginalCorrelatedTask(t *testing.T) {
	var writes atomic.Int32
	var marker string
	var succeeded atomic.Bool
	at := time.Date(2026, 10, 5, 0, 0, 0, 123456789, time.UTC)
	originalTask := func() meilisearch.Task {
		status := meilisearch.TaskStatusProcessing
		if succeeded.Load() {
			status = meilisearch.TaskStatusSucceeded
		}
		return meilisearch.Task{UID: 0, IndexUID: "content", Type: meilisearch.TaskTypeDocumentAdditionOrUpdate, Status: status, EnqueuedAt: at, CustomMetadata: marker}
	}
	svc, db := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/indexes/content/documents":
			writes.Add(1)
			marker = req.URL.Query().Get("customMetadata")
			if marker == "" || req.URL.Query().Get("skipCreation") != "true" {
				t.Error("uncorrelated or implicitly recreating submission")
			}
			w.WriteHeader(503) // accepted externally, receipt lost
		case "/tasks":
			_ = json.NewEncoder(w).Encode(meilisearch.TaskResult{Total: 1, Results: []meilisearch.Task{originalTask()}})
		case "/tasks/0":
			_ = json.NewEncoder(w).Encode(originalTask())
		default:
			t.Errorf("unexpected request %s", req.URL.Path)
			w.WriteHeader(500)
		}
	}))
	if err := svc.UpsertContentDocument(t.Context(), 7, contentDeliveryDocument()); !errors.Is(err, repository.ErrContentIndexIsolated) {
		t.Fatal(err)
	}
	var op models.ContentIndexDelivery
	if err := db.First(&op).Error; err != nil {
		t.Fatal(err)
	}
	if op.TaskCorrelation != op.ID || op.TaskCorrelation != marker || op.Status != repository.IndexDeliveryUnknown {
		t.Fatalf("missing durable correlation: %#v", op)
	}
	// A new startup continues the original submission without its payload.
	epoch, err := svc.deliveries.Open(t.Context(), svc.endpointID, true)
	if err != nil {
		t.Fatal(err)
	}
	svc.epoch = epoch
	if err := svc.reconcileContentDeliveries(t.Context()); !errors.Is(err, repository.ErrContentIndexIsolated) {
		t.Fatal("processing task reopened search", err)
	}
	stored, err := svc.deliveries.Get(t.Context(), 7, op.ID)
	if err != nil || stored.Status != repository.IndexDeliverySubmitted || stored.TaskEnqueuedAt != at.Format(time.RFC3339Nano) {
		t.Fatalf("original receipt not recovered: %#v %v", stored, err)
	}
	succeeded.Store(true)
	if err := svc.reconcileContentDeliveries(t.Context()); err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 1 {
		t.Fatal("SDK or recovery resent original write")
	}
}

func TestContentDeliveryRecoveryRejectsInsufficientOrConflictingHistory(t *testing.T) {
	for _, scenario := range []string{"missing", "duplicate", "wrong-index", "wrong-type", "incomplete", "unexpected-next", "duplicate-uid", "budget", "changed-total", "marker-changed-on-poll", "legacy", "different-endpoint"} {
		t.Run(scenario, func(t *testing.T) {
			var marker string
			var pages int
			svc, db := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("recovery attempted write")
					w.WriteHeader(500)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				task := meilisearch.Task{UID: 2, IndexUID: "content", Type: meilisearch.TaskTypeDocumentAdditionOrUpdate, Status: meilisearch.TaskStatusSucceeded, EnqueuedAt: time.Now().UTC(), CustomMetadata: marker}
				if r.URL.Path == "/tasks/2" {
					task.CustomMetadata = "another-marker"
					_ = json.NewEncoder(w).Encode(task)
					return
				}
				page := meilisearch.TaskResult{Total: 1, Results: []meilisearch.Task{task}}
				switch scenario {
				case "missing":
					page.Total = 0
					page.Results = nil
				case "duplicate":
					task.UID = 1
					page.Total = 2
					page.Results = append(page.Results, task)
				case "wrong-index":
					page.Results[0].IndexUID = "other"
				case "wrong-type":
					page.Results[0].Type = meilisearch.TaskTypeDocumentDeletion
				case "incomplete":
					page.Total = 3
				case "unexpected-next":
					page.Next = 1
				case "duplicate-uid":
					page.Total = 2
					task.CustomMetadata = "unrelated"
					page.Results = append(page.Results, task)
				case "budget":
					page.Total = 10001
				case "changed-total":
					pages++
					page.Total = 2
					page.Next = 1
					page.Results[0].CustomMetadata = "unrelated"
					if pages > 1 {
						page.Total = 3
						page.Next = 0
						page.Results[0].UID = 1
					}
				}
				_ = json.NewEncoder(w).Encode(page)
			}))
			op := &models.ContentIndexDelivery{TenantID: 7, DocumentID: "item", Kind: repository.IndexDeliveryWrite}
			if err := db.Transaction(func(tx *gorm.DB) error { return svc.deliveries.Register(t.Context(), tx, svc.epoch, op) }); err != nil {
				t.Fatal(err)
			}
			marker = op.TaskCorrelation
			if scenario == "legacy" {
				op.TaskCorrelation = ""
				if err := db.Model(op).Update("task_correlation", "").Error; err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "different-endpoint" {
				svc.endpointID = "other"
			}
			if err := svc.processContentDelivery(t.Context(), op); !errors.Is(err, repository.ErrContentIndexIsolated) {
				t.Fatal("unsafe recovery accepted", err)
			}
			stored, err := svc.deliveries.Get(t.Context(), 7, op.ID)
			if err != nil || stored.Status == repository.IndexDeliverySucceeded {
				t.Fatal("unsafe history completed delivery", err)
			}
			if err := svc.deliveries.RequireActive(t.Context(), svc.epoch); !errors.Is(err, repository.ErrContentIndexIsolated) {
				t.Fatal("unsafe history reopened outlet", err)
			}
		})
	}
}

func TestContentDeliveryLostDeleteResponseRecoversWithoutResubmitting(t *testing.T) {
	var deletes atomic.Int64
	var marker string
	at := time.Date(2026, 10, 5, 0, 0, 0, 123456789, time.UTC)
	svc, _ := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/indexes/content/documents/delete" {
			deletes.Add(1)
			marker = r.URL.Query().Get("customMetadata")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected recovery operation: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		task := meilisearch.Task{UID: 31, IndexUID: "content", Type: meilisearch.TaskTypeDocumentDeletion, Status: meilisearch.TaskStatusSucceeded, EnqueuedAt: at, CustomMetadata: marker}
		switch r.URL.Path {
		case "/tasks":
			_ = json.NewEncoder(w).Encode(meilisearch.TaskResult{Total: 1, Results: []meilisearch.Task{task}})
		case "/tasks/31":
			_ = json.NewEncoder(w).Encode(task)
		default:
			t.Error("unexpected task lookup", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	op, err := svc.deliveries.QueueDelete(t.Context(), svc.epoch, 7, "tenant_id = 7")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.processContentDelivery(t.Context(), op); !errors.Is(err, repository.ErrContentIndexIsolated) {
		t.Fatal("lost delete response accepted", err)
	}
	if marker == "" || marker != op.ID {
		t.Fatal("delete did not send its persistent identity")
	}
	svc.epoch, err = svc.deliveries.Open(t.Context(), svc.endpointID, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.reconcileContentDeliveries(t.Context()); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.deliveries.Get(t.Context(), 7, op.ID)
	if err != nil || stored.Status != repository.IndexDeliverySucceeded || stored.TaskUID == nil || *stored.TaskUID != 31 || deletes.Load() != 1 {
		t.Fatalf("delete recovery lost identity or resubmitted: %#v %d %v", stored, deletes.Load(), err)
	}
}

func TestContentDeliveryRecoveryPaginatesThroughUIDZero(t *testing.T) {
	var marker string
	var pages, zeroReads int
	svc, db := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		task := meilisearch.Task{UID: 0, IndexUID: "content", Type: meilisearch.TaskTypeDocumentAdditionOrUpdate, Status: meilisearch.TaskStatusSucceeded, EnqueuedAt: time.Now().UTC(), CustomMetadata: marker}
		if r.URL.Path == "/tasks/0" {
			zeroReads++
			_ = json.NewEncoder(w).Encode(task)
			return
		}
		pages++
		if pages == 1 {
			task.UID = 2
			task.CustomMetadata = "other"
			_ = json.NewEncoder(w).Encode(meilisearch.TaskResult{Total: 3, Next: 1, Results: []meilisearch.Task{task}})
			return
		}
		if r.URL.Query().Get("from") != "1" {
			t.Error("pagination cursor omitted")
		}
		task.UID = 1
		task.CustomMetadata = "other"
		_ = json.NewEncoder(w).Encode(meilisearch.TaskResult{Total: 3, Next: 0, Results: []meilisearch.Task{task}})
	}))
	op := &models.ContentIndexDelivery{TenantID: 7, DocumentID: "item", Kind: repository.IndexDeliveryWrite}
	if err := db.Transaction(func(tx *gorm.DB) error { return svc.deliveries.Register(t.Context(), tx, svc.epoch, op) }); err != nil {
		t.Fatal(err)
	}
	marker = op.TaskCorrelation
	if err := svc.recoverContentReceipt(t.Context(), op); err != nil {
		t.Fatal(err)
	}
	if op.TaskUID == nil || *op.TaskUID != 0 || pages != 2 || zeroReads != 1 {
		t.Fatal("UID zero pagination lost receipt")
	}
}

func TestContentDeliveryOldServerIsolatedWithoutSubmitting(t *testing.T) {
	for _, version := range []string{"1.7.6", "1.25.9", "unknown", "2.0.0"} {
		t.Run(version, func(t *testing.T) {
			svc, _ := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/version" {
					t.Error("unsupported server received index operation")
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"pkgVersion":%q}`, version)
			}))
			if err := svc.initIndexes(t.Context()); err == nil {
				t.Fatal("unsupported version accepted")
			}
		})
	}
	for _, version := range []string{"1.26.0", "1.54.3"} {
		if !supportsContentTaskCorrelation(version) {
			t.Fatal("supported version rejected")
		}
	}
}
