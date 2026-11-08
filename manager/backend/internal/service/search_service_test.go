package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/meilisearch/meilisearch-go"
)

func TestMetadataOnlyKeywordSearchReturnsOnlyActualFieldNames(t *testing.T) {
	for _, test := range []struct {
		name      string
		formatted map[string]interface{}
		matched   bool
	}{
		{"name_highlight", map[string]interface{}{"name": "userInfo.<mark>phone</mark>", "data_type": "<mark>string</mark>", "comment": "<mark>private</mark>"}, true},
		{"comment_highlight_is_not_a_name_match", map[string]interface{}{"name": "userInfo.phone", "comment": "<mark>private</mark>"}, false},
		{"missing_highlights", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity := testSearchIdentity(t, 7, 11, 80, "Persons")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method + " " + r.URL.Path {
				case "GET /indexes/content/settings/pagination":
					_, _ = w.Write([]byte(`{"maxTotalHits":1000}`))
				case "POST /indexes/content/search":
					var request meilisearch.SearchRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					want := []string{"name", "full_name", "fields.name"}
					if !reflect.DeepEqual(request.AttributesToSearchOn, want) || !reflect.DeepEqual(request.AttributesToHighlight, want) {
						t.Errorf("metadata keyword/highlight scope: %+v / %+v", request.AttributesToSearchOn, request.AttributesToHighlight)
					}
					wantRetrieve := []string{"document_id", "locator", "engine_id", "engine_name", "engine_type", "data_item_type", "name", "full_name", "schema", "bucket", "path", "fields.name"}
					if !reflect.DeepEqual(request.AttributesToRetrieve, wantRetrieve) {
						t.Errorf("metadata retrieval scope: %+v", request.AttributesToRetrieve)
					}
					hit := map[string]interface{}{
						"document_id": identity.ID, "locator": identity.Locator, "engine_id": 11, "name": "Persons", "_rankingScore": 0.9,
						"fields": []interface{}{map[string]interface{}{"name": "userInfo.phone", "data_type": "string", "comment": "private", "sample_value": "private value"}},
						"title":  "private title", "content_preview": "private body", "metadata": map[string]interface{}{"sample": "private sample"},
						"_formatted": map[string]interface{}{"content_preview": "<mark>private</mark> body"},
					}
					if test.formatted != nil {
						hit["_formatted"].(map[string]interface{})["fields"] = []interface{}{test.formatted}
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"hits": []interface{}{hit}})
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			svc := &HybridSearchService{client: meilisearch.New(server.URL, meilisearch.DisableRetries()), contentIndex: "content"}
			id, _ := decodeSearchIdentity(identity.ID)
			session := &searchSession{service: svc, tenant: 7, query: "phone"}
			projected, err := session.keywordProjection(context.Background(), []searchIdentity{id}, searchKeywordMetadata)
			result := []SearchDocument{projected[id]}
			if err != nil {
				t.Fatal(err)
			}
			want := SearchDocument{DocumentID: identity.ID, Locator: identity.Locator, EngineID: 11, Name: "Persons", FileName: "Persons"}
			if test.matched {
				want.FieldMatches = []SearchFieldMatch{{Name: "userInfo.phone", Highlights: map[string]string{"name": "userInfo.<mark>phone</mark>"}}}
			}
			if !reflect.DeepEqual(result, []SearchDocument{want}) {
				t.Fatalf("metadata response exceeded field-name projection or lost the actual match: %+v", result)
			}
		})
	}
}

func TestMapMeilisearchHitPreservesActualFieldMatches(t *testing.T) {
	t.Parallel()
	var hit map[string]interface{}
	if err := json.Unmarshal([]byte(`{
  "document_id":"collection", "name":"Outdoors",
  "fields":[
    {"name":"aamember","data_type":"json","comment":"participants"},
    {"name":"leader.nickname","data_type":"string","comment":"Team participant"},
    {"name":"unmatched","data_type":"string"},
    {"name":"literal.<mark>source</mark>","data_type":"string"}
  ],
  "_formatted":{"fields":[
    {"name":"<mark>aamember</mark>","data_type":"json","comment":"participants"},
    {"name":"leader.nickname","data_type":"string","comment":"Team <mark>participant</mark>"},
    {"name":"unmatched","data_type":"string"},
    {"name":"literal.<mark>source</mark>","data_type":"string"}
  ]}
}`), &hit); err != nil {
		t.Fatal(err)
	}
	doc := mapMeilisearchHit(hit)
	want := []SearchFieldMatch{
		{Name: "aamember", DataType: "json", Comment: "participants", Highlights: map[string]string{"name": "<mark>aamember</mark>"}},
		{Name: "leader.nickname", DataType: "string", Comment: "Team participant", Highlights: map[string]string{"comment": "Team <mark>participant</mark>"}},
	}
	if !reflect.DeepEqual(doc.FieldMatches, want) {
		t.Fatalf("field matches=%+v, want %+v", doc.FieldMatches, want)
	}
	merged := doc
	mergeVectorSearchFacts(&merged, vectorDocumentToSearchDocument(VectorDocument{DocumentID: "collection", Distance: 0.1}))
	if !reflect.DeepEqual(merged.FieldMatches, want) || len(vectorDocumentToSearchDocument(VectorDocument{DocumentID: "semantic-only"}).FieldMatches) != 0 {
		t.Fatal("vector projection fabricated or removed keyword field evidence")
	}
	delete(hit, "_formatted")
	if len(mapMeilisearchHit(hit).FieldMatches) != 0 {
		t.Fatal("unformatted definitions reported as matches")
	}
}

func searchDocumentIDs(documents []SearchDocument) []string {
	ids := make([]string, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, document.DocumentID)
	}
	return ids
}

func TestBuildSearchFilterRestrictsCurrentEngine(t *testing.T) {
	t.Parallel()

	tenantID := uint(7)
	engineID := uint(3)
	if got := buildSearchFilter(&tenantID, &engineID); got != "tenant_id = 7 AND engine_id = 3" {
		t.Fatalf("buildSearchFilter() = %q", got)
	}
}

func TestBuildSearchFilterAllowsTenantWideSearch(t *testing.T) {
	t.Parallel()

	tenantID := uint(7)
	if got := buildSearchFilter(&tenantID, nil); got != "tenant_id = 7" {
		t.Fatalf("buildSearchFilter() = %q", got)
	}
}

func TestMapMeilisearchHitKeepsOnlyIndexedLocator(t *testing.T) {
	t.Parallel()

	doc := mapMeilisearchHit(map[string]interface{}{
		"document_id":    "item-fingerprint",
		"content_hash":   "content-sha256",
		"engine_id":      float64(9),
		"engine_type":    "minio",
		"data_item_type": "object",
		"full_name":      "addp/reports/report.docx",
		"name":           "report.docx",
	})

	if doc.DocumentID != "item-fingerprint" {
		t.Fatalf("DocumentID = %q, want item fingerprint", doc.DocumentID)
	}
	if doc.FileName != "report.docx" {
		t.Fatalf("FileName = %q, want report.docx", doc.FileName)
	}
	if doc.Locator != "" {
		t.Fatalf("Locator = %q, want empty locator when index does not provide one", doc.Locator)
	}
}

func TestMapMeilisearchHitPrefersIndexedLocator(t *testing.T) {
	t.Parallel()

	doc := mapMeilisearchHit(map[string]interface{}{
		"document_id":    "item-fingerprint",
		"locator":        "addp://engine/9/path/addp/reports/report.docx?type=object&item_id=7",
		"engine_id":      float64(9),
		"data_item_type": "object",
		"full_name":      "wrong/path.txt",
		"name":           "report.docx",
	})

	if doc.Locator != "addp://engine/9/path/addp/reports/report.docx?type=object&item_id=7" {
		t.Fatalf("Locator = %q, want indexed locator", doc.Locator)
	}
}

func TestMapMeilisearchHitRejectsLocatorWithoutItemID(t *testing.T) {
	t.Parallel()

	doc := mapMeilisearchHit(map[string]interface{}{
		"document_id":    "item-fingerprint",
		"locator":        "addp://engine/9/path/addp/reports/report.docx?type=object",
		"engine_id":      float64(9),
		"data_item_type": "object",
		"full_name":      "addp/reports/report.docx",
		"name":           "report.docx",
	})

	if doc.Locator != "" {
		t.Fatalf("Locator = %q, want empty locator without item_id", doc.Locator)
	}
}

func TestVectorDocumentToSearchDocumentKeepsMetadataLocator(t *testing.T) {
	t.Parallel()

	doc := vectorDocumentToSearchDocument(VectorDocument{
		DocumentID: "vector-doc",
		EngineID:   9,
		Metadata: map[string]interface{}{
			"locator": "addp://engine/9/path/addp/reports/report.docx?type=object&item_id=7",
			"storage": map[string]interface{}{
				"bucket": "addp",
				"path":   "reports/",
				"name":   "report.docx",
			},
		},
	})

	if doc.Locator != "addp://engine/9/path/addp/reports/report.docx?type=object&item_id=7" {
		t.Fatalf("Locator = %q, want indexed locator", doc.Locator)
	}
}

func TestVectorDocumentToSearchDocumentRejectsMetadataLocatorWithoutItemID(t *testing.T) {
	t.Parallel()

	doc := vectorDocumentToSearchDocument(VectorDocument{
		DocumentID: "vector-doc",
		EngineID:   9,
		Metadata: map[string]interface{}{
			"resource": map[string]interface{}{
				"locator": "addp://engine/9/path/addp/reports/report.docx?type=object",
			},
			"storage": map[string]interface{}{
				"bucket": "addp",
				"path":   "reports/",
				"name":   "report.docx",
			},
		},
	})

	if doc.Locator != "" {
		t.Fatalf("Locator = %q, want empty locator without item_id", doc.Locator)
	}
}
