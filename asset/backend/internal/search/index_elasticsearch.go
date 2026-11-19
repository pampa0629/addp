package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/addp/common/infra/search/elasticsearch"
)

type elasticsearchAssetIndex struct {
	client *elasticsearch.Client
	path   string
}

var elasticAssetIndexName = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,254}$`)
var assetTextFields = []string{"name", "description", "tags", "type_name", "category_name"}

func NewElasticsearchIndexer(endpoint, username, password, indexName string) (*Indexer, error) {
	if indexName == "" {
		indexName = "asset_published"
	}
	if !elasticAssetIndexName.MatchString(indexName) {
		return nil, fmt.Errorf("invalid Asset Elasticsearch index name")
	}
	client, err := elasticsearch.New(endpoint, username, password)
	if err != nil {
		return nil, err
	}
	index := &elasticsearchAssetIndex{client: client, path: "/" + indexName}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := index.ensureIndex(ctx); err != nil {
		return nil, err
	}
	return &Indexer{elastic: index, index: indexName, enabled: true}, nil
}

type object = map[string]any

func (i *elasticsearchAssetIndex) ensureIndex(ctx context.Context) error {
	properties := object{
		"id": object{"type": "long"}, "tenant_id": object{"type": "long"}, "category_id": object{"type": "long"},
		"type_code": object{"type": "keyword"}, "status": object{"type": "keyword"},
		"published_at": object{"type": "keyword"},
	}
	for _, name := range assetTextFields {
		properties[name] = object{"type": "text"}
	}
	body := object{"mappings": object{"dynamic": "strict", "properties": properties}}
	var created struct{ Acknowledged bool }
	err := i.client.Request(ctx, http.MethodPut, i.path, body, &created)
	var responseErr *elasticsearch.ResponseError
	if errors.As(err, &responseErr) && responseErr.Type == "resource_already_exists_exception" {
		return nil
	}
	if err != nil {
		return err
	}
	if !created.Acknowledged {
		return fmt.Errorf("Asset Elasticsearch index creation was not acknowledged")
	}
	return nil
}

func (i *elasticsearchAssetIndex) replace(ctx context.Context, docs []AssetIndexDoc) error {
	var result struct {
		TimedOut  bool `json:"timed_out"`
		Failures  []json.RawMessage
		Conflicts int `json:"version_conflicts"`
	}
	if err := i.client.Request(ctx, http.MethodPost, i.path+"/_delete_by_query?refresh=true",
		object{"query": object{"match_all": object{}}}, &result); err != nil {
		return err
	}
	if result.TimedOut || len(result.Failures) != 0 || result.Conflicts != 0 {
		return fmt.Errorf("Asset Elasticsearch index could not be cleared for rebuild")
	}
	return i.write(ctx, "index", docs, nil, "")
}

// write submits only owner-approved documents or changes to their projected status.
func (i *elasticsearchAssetIndex) write(ctx context.Context, action string, docs []AssetIndexDoc, ids []int64, status string) error {
	var body bytes.Buffer
	encode := json.NewEncoder(&body)
	appendAction := func(id int64, payload any) error {
		if err := encode.Encode(object{action: object{"_id": strconv.FormatInt(id, 10)}}); err != nil {
			return err
		}
		if payload != nil {
			return encode.Encode(payload)
		}
		return nil
	}
	for _, doc := range docs {
		if doc.Tags == nil {
			doc.Tags = []string{}
		}
		if err := appendAction(doc.ID, doc); err != nil {
			return err
		}
	}
	for _, id := range ids {
		var payload any
		if action == "update" {
			payload = object{"doc": object{"status": status}}
		}
		if err := appendAction(id, payload); err != nil {
			return err
		}
	}
	if body.Len() == 0 {
		return nil
	}
	var result struct {
		Items []map[string]struct {
			Status int
			Error  struct{ Type string }
		}
	}
	if err := i.client.RequestNDJSON(ctx, http.MethodPost, i.path+"/_bulk?refresh=wait_for", &body, &result); err != nil {
		return err
	}
	if len(result.Items) != len(docs)+len(ids) {
		return fmt.Errorf("Asset Elasticsearch returned an incomplete bulk receipt")
	}
	for _, item := range result.Items {
		receipt := item[action]
		if receipt.Status == http.StatusNotFound &&
			((action == "delete" && receipt.Error.Type == "") ||
				(action == "update" && receipt.Error.Type == "document_missing_exception")) {
			continue
		}
		if receipt.Status < 200 || receipt.Status >= 300 {
			return fmt.Errorf("Asset Elasticsearch bulk %s returned HTTP %d (%s)", action, receipt.Status, receipt.Error.Type)
		}
	}
	return nil
}

func (i *elasticsearchAssetIndex) search(ctx context.Context, tenantID int64, keyword, typeCode string, categories []int64, limit, offset int64) (*SearchResult, error) {
	filters := []object{{"term": object{"tenant_id": tenantID}}, {"term": object{"status": "published"}}}
	if typeCode != "" {
		filters = append(filters, object{"term": object{"type_code": typeCode}})
	}
	if len(categories) == 1 && categories[0] == -1 {
		filters = append(filters, object{"bool": object{"must_not": object{"exists": object{"field": "category_id"}}}})
	} else if categories != nil {
		filters = append(filters, object{"terms": object{"category_id": categories}})
	}
	query := object{
		"from": offset, "size": limit, "track_total_hits": true, "_source": false,
		"sort": []string{"_score", "id"},
		"query": object{"bool": object{
			"filter": filters, "must": object{"multi_match": object{"query": keyword, "fields": assetTextFields}},
		}},
	}
	var result struct {
		Hits struct {
			Total struct{ Value int64 }
			Hits  []struct {
				ID string `json:"_id"`
			}
		}
	}
	if err := i.client.Request(ctx, http.MethodPost, i.path+"/_search?allow_partial_search_results=false", query, &result); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		id, err := strconv.ParseInt(hit.ID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid Asset Elasticsearch document ID: %w", err)
		}
		ids = append(ids, id)
	}
	return &SearchResult{IDs: ids, Total: result.Hits.Total.Value}, nil
}
