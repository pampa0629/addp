package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/addp/manager/internal/repository"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
)

// The protocol fixture implements replacement (POST) and partial update (PUT).
// The same snapshot assertions also run against Meilisearch in the owner T2 gate.
func TestContentIndexSnapshotsCoexistAndReplaceOnlyTheirOwnFields(t *testing.T) {
	for _, contentFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("content_first_%v", contentFirst), func(t *testing.T) {
			var mu sync.Mutex
			stored := map[string]interface{}{}
			tasks := map[int]string{}
			at := "2026-10-09T00:00:00Z"
			svc, _ := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/indexes/content/documents" {
					var batch []map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&batch); err != nil || len(batch) != 1 {
						t.Errorf("invalid document batch: %v", err)
						w.WriteHeader(500)
						return
					}
					if r.Method == http.MethodPost {
						stored = map[string]interface{}{}
					} else if r.Method != http.MethodPut {
						t.Errorf("unexpected write method: %s", r.Method)
					}
					for key, value := range batch[0] {
						stored[key] = value
					}
					uid := len(tasks)
					tasks[uid] = r.URL.Query().Get("customMetadata")
					w.WriteHeader(http.StatusAccepted)
					fmt.Fprintf(w, `{"taskUid":%d,"indexUid":"content","type":"documentAdditionOrUpdate","enqueuedAt":%q}`, uid, at)
					return
				}
				uid, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/tasks/"))
				if err != nil {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
					return
				}
				fmt.Fprintf(w, `{"uid":%d,"indexUid":"content","type":"documentAdditionOrUpdate","status":"succeeded","enqueuedAt":%q,"customMetadata":%q}`, uid, at, tasks[uid])
			}))
			exerciseContentSnapshots(t, svc, "item", contentFirst, func() map[string]interface{} {
				mu.Lock()
				defer mu.Unlock()
				copy := map[string]interface{}{}
				for key, value := range stored {
					copy[key] = value
				}
				return copy
			})
		})
	}
}

func exerciseContentSnapshots(t *testing.T, svc *HybridSearchService, id string, contentFirst bool, read func() map[string]interface{}) {
	t.Helper()
	rows := int64(10)
	technical := commonClient.ManagerContentDocument{
		DocumentID: id, PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata, EngineID: 9,
		DataItemType: "object", Name: "current.txt", FullName: "documents/current.txt", Bucket: "documents",
		Fields: []commonClient.ManagerContentField{{Name: "id", DataType: "string"}}, RowCount: &rows,
		Description: "registered structure", ProjectionTime: time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC),
	}
	content := commonClient.ManagerContentDocument{
		DocumentID: id, PayloadKind: commonClient.ManagerContentPayloadExtractedContent, EngineID: 9,
		DataItemType: "object", Name: "old-request-name.txt", Content: "protected text", ContentPreview: "protected preview",
		Title: "old title", Author: "old author", Keywords: []string{"old keyword"}, Tags: []string{"old tag"},
		Metadata: map[string]interface{}{"summary": "old summary"}, WordCount: 2,
		ProjectionTime: time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC),
	}
	submit := func(doc commonClient.ManagerContentDocument) {
		t.Helper()
		if err := svc.UpsertContentDocument(t.Context(), 7, doc); err != nil {
			t.Fatal(err)
		}
	}
	if contentFirst {
		submit(content)
		submit(technical)
	} else {
		submit(technical)
		submit(content)
	}
	assertBoth := func(doc map[string]interface{}) {
		t.Helper()
		if doc["name"] != technical.Name || doc["full_name"] != technical.FullName || doc["content"] != content.Content || doc["author"] != content.Author || doc["description"] != technical.Description {
			t.Fatalf("snapshots overwrote each other: %#v", doc)
		}
	}
	assertBoth(read())
	results := make(chan error, 2)
	go func() { results <- svc.UpsertContentDocument(t.Context(), 7, technical) }()
	go func() { results <- svc.UpsertContentDocument(t.Context(), 7, content) }()
	accepted := 0
	var unexpected error
	for range 2 {
		if err := <-results; err == nil {
			accepted++
		} else if !errors.Is(err, repository.ErrContentIndexIsolated) {
			unexpected = err
		}
	}
	if unexpected != nil || accepted == 0 {
		t.Fatalf("concurrent admission: accepted=%d error=%v", accepted, unexpected)
	}
	// Durable admission serializes uncertain/same-item submissions. A refused
	// caller retries after the accepted write settles; the protection fence stays.
	submit(technical)
	submit(content)
	assertBoth(read())
	before := read()
	for range 2 {
		submit(technical)
		submit(content)
	}
	if after := read(); !reflect.DeepEqual(before, after) {
		t.Fatalf("repeated snapshots changed stored document: before=%#v after=%#v", before, after)
	}
	content.Content, content.ContentPreview = "new text", ""
	content.Title, content.Author, content.Keywords, content.Tags, content.Metadata, content.WordCount = "", "", nil, nil, nil, 0
	submit(content)
	after := read()
	if after["name"] != technical.Name || after["description"] != technical.Description || after["content"] != "new text" || after["author"] != "" || after["title"] != "" || after["keywords"] != nil || after["metadata"] != nil || after["word_count"] != float64(0) {
		t.Fatalf("content snapshot failed to clear its old fields: %#v", after)
	}
	technical.Fields, technical.RowCount, technical.Description = nil, nil, ""
	submit(technical)
	after = read()
	if after["content"] != "new text" || after["fields"] != nil || after["row_count"] != nil || after["description"] != "" {
		t.Fatalf("technical snapshot failed to clear its old fields independently: %#v", after)
	}
}
