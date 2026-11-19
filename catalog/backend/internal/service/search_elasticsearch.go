package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"sync"

	"github.com/addp/common/infra/search/elasticsearch"
	"github.com/google/uuid"
)

var catalogElasticIndexName = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,254}$`)

type ElasticsearchCatalogIndex struct {
	client *elasticsearch.Client
	path   string
	mu     sync.Mutex
	ready  bool
}

var _ CatalogSearchProjection = (*ElasticsearchCatalogIndex)(nil)
var _ CatalogSearchResolver = (*ElasticsearchCatalogIndex)(nil)

func NewElasticsearchCatalogIndex(endpoint, username, password, indexName string) (*ElasticsearchCatalogIndex, error) {
	if !catalogElasticIndexName.MatchString(indexName) {
		return nil, fmt.Errorf("invalid Catalog Elasticsearch index name")
	}
	client, err := elasticsearch.New(endpoint, username, password)
	if err != nil {
		return nil, err
	}
	return &ElasticsearchCatalogIndex{client: client, path: "/" + indexName}, nil
}

func (i *ElasticsearchCatalogIndex) Upsert(ctx context.Context, document CatalogSearchDocument) error {
	id, err := uuid.Parse(document.ID)
	if err != nil {
		return fmt.Errorf("invalid Catalog document ID: %w", err)
	}
	if err := i.ensureIndex(ctx); err != nil {
		return err
	}
	return i.client.Request(ctx, http.MethodPut, i.path+"/_doc/"+id.String()+"?refresh=wait_for", document, nil)
}

func (i *ElasticsearchCatalogIndex) Delete(ctx context.Context, documentID string) error {
	id, err := uuid.Parse(documentID)
	if err != nil {
		return fmt.Errorf("invalid Catalog document ID: %w", err)
	}
	if err := i.ensureIndex(ctx); err != nil {
		return err
	}
	err = i.client.Request(ctx, http.MethodDelete, i.path+"/_doc/"+id.String()+"?refresh=wait_for", nil, nil)
	var responseErr *elasticsearch.ResponseError
	if errors.As(err, &responseErr) && responseErr.Status == http.StatusNotFound && responseErr.Type == "" {
		return nil
	}
	return err
}

func (i *ElasticsearchCatalogIndex) SearchCatalogEntries(ctx context.Context, tenantID int64, access EntryAccess, filter EntryListFilter) ([]uuid.UUID, int64, error) {
	if err := i.ensureIndex(ctx); err != nil {
		return nil, 0, err
	}
	type query = map[string]any
	term := func(field string, value any) query { return query{"term": query{field: value}} }
	filters := []query{term("tenant_id", tenantID), term("entry_status", "active")}
	if filter.View == EntryViewGovernance {
		filters = append(filters, query{"terms": query{"governance_status": catalogGovernanceSearchStatuses}})
	}
	if !access.Inventory {
		departments := make([]string, 0, len(access.DepartmentIDs))
		for _, id := range access.DepartmentIDs {
			if id > 0 {
				departments = append(departments, strconv.FormatInt(id, 10))
			}
		}
		visibility := term("visibility", "tenant")
		if len(departments) > 0 {
			departmentVisibility := query{"bool": query{"filter": []query{
				term("visibility", "department"), {"terms": query{"accountable_department_id": departments}},
			}}}
			visibility = query{"bool": query{"should": []query{visibility, departmentVisibility}, "minimum_should_match": 1}}
		}
		filters = append(filters, visibility)
	}
	for _, facet := range catalogSearchFacets(filter) {
		filters = append(filters, term(facet.field, facet.value))
	}
	search := query{
		"from": (filter.Page - 1) * filter.PageSize, "size": filter.PageSize,
		"track_total_hits": true, "_source": false,
		"sort": []any{"_score", query{"updated_at": "desc"}, "id"},
		"query": query{"bool": query{
			"filter": filters,
			"must":   query{"multi_match": query{"query": filter.Search, "fields": catalogSearchTextFields}},
		}},
	}
	var result struct {
		Hits struct {
			Total struct{ Value int64 }
			Hits  []struct {
				ID uuid.UUID `json:"_id"`
			}
		}
	}
	if err := i.client.Request(ctx, http.MethodPost, i.path+"/_search?allow_partial_search_results=false", search, &result); err != nil {
		return nil, 0, err
	}
	ids := make([]uuid.UUID, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		ids = append(ids, hit.ID)
	}
	return ids, result.Hits.Total.Value, nil
}

func (i *ElasticsearchCatalogIndex) ensureIndex(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.ready {
		return nil
	}
	type field struct {
		Type string `json:"type"`
	}
	properties := map[string]field{
		"tenant_id": {"long"}, "source_engine_id": {"long"}, "updated_at": {"date"},
	}
	for _, name := range []string{"id", "entry_status", "entry_type", "source_status", "governance_status", "visibility", "primary_domain_id", "accountable_department_id"} {
		properties[name] = field{"keyword"}
	}
	for _, name := range catalogSearchTextFields {
		properties[name] = field{"text"}
	}
	settings := map[string]any{"mappings": map[string]any{"dynamic": "strict", "properties": properties}}
	var result struct{ Acknowledged bool }
	err := i.client.Request(ctx, http.MethodPut, i.path, settings, &result)
	var responseErr *elasticsearch.ResponseError
	if errors.As(err, &responseErr) && responseErr.Type == "resource_already_exists_exception" {
		// Another Catalog process can have already created the same index.
		i.ready = true
		return nil
	}
	if err != nil {
		return err
	}
	if !result.Acknowledged {
		return fmt.Errorf("Catalog Elasticsearch index creation was not acknowledged")
	}
	i.ready = true
	return nil
}
