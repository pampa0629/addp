package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/meilisearch/meilisearch-go"
)

type MeilisearchCatalogIndex struct {
	client    *meilisearch.Client
	indexName string
	mu        sync.Mutex
	ready     bool
}

func (i *MeilisearchCatalogIndex) SearchCatalogEntries(ctx context.Context, tenantID int64, access EntryAccess, filter EntryListFilter) ([]uuid.UUID, int64, error) {
	if err := i.ensureIndex(ctx); err != nil {
		return nil, 0, err
	}
	filters := []string{fmt.Sprintf("tenant_id = %d", tenantID), `entry_status = "active"`}
	if filter.View == EntryViewGovernance {
		filters = append(filters, `governance_status IN ["curated", "certified", "deprecated"]`)
	}
	if !access.Inventory {
		visibility := `visibility = "tenant"`
		if len(access.DepartmentIDs) > 0 {
			ids := make([]string, 0, len(access.DepartmentIDs))
			for _, id := range access.DepartmentIDs {
				if id > 0 {
					ids = append(ids, fmt.Sprintf(`"%d"`, id))
				}
			}
			if len(ids) > 0 {
				visibility = fmt.Sprintf(`(%s OR (visibility = "department" AND accountable_department_id IN [%s]))`, visibility, strings.Join(ids, ","))
			}
		}
		filters = append(filters, visibility)
	}
	appendStringFilter := func(field, value string) {
		if value != "" {
			filters = append(filters, fmt.Sprintf(`%s = "%s"`, field, value))
		}
	}
	appendStringFilter("entry_type", filter.EntryType)
	appendStringFilter("source_status", filter.SourceStatus)
	appendStringFilter("governance_status", filter.GovernanceStatus)
	appendStringFilter("visibility", filter.Visibility)
	if filter.PrimaryDomainID > 0 {
		appendStringFilter("primary_domain_id", fmt.Sprintf("%d", filter.PrimaryDomainID))
	}
	if filter.DepartmentID > 0 {
		appendStringFilter("accountable_department_id", fmt.Sprintf("%d", filter.DepartmentID))
	}
	if filter.SourceEngineID > 0 {
		filters = append(filters, fmt.Sprintf("source_engine_id = %d", filter.SourceEngineID))
	}
	response, err := i.client.Index(i.indexName).Search(filter.Search, &meilisearch.SearchRequest{
		Filter: strings.Join(filters, " AND "), Limit: int64(filter.PageSize), Offset: int64((filter.Page - 1) * filter.PageSize),
	})
	if err != nil {
		return nil, 0, err
	}
	ids := make([]uuid.UUID, 0, len(response.Hits))
	for _, hit := range response.Hits {
		payload, ok := hit.(map[string]interface{})
		if !ok {
			continue
		}
		value, _ := payload["id"].(string)
		id, err := uuid.Parse(value)
		if err == nil && id != uuid.Nil {
			ids = append(ids, id)
		}
	}
	return ids, response.EstimatedTotalHits, nil
}

func NewMeilisearchCatalogIndex(url, apiKey, indexName string) (*MeilisearchCatalogIndex, error) {
	url = strings.TrimSpace(url)
	indexName = strings.TrimSpace(indexName)
	if url == "" || indexName == "" {
		return nil, fmt.Errorf("Catalog Meilisearch URL and index are required")
	}
	return &MeilisearchCatalogIndex{
		client: meilisearch.NewClient(meilisearch.ClientConfig{
			Host: url, APIKey: apiKey, Timeout: 10 * time.Second,
		}),
		indexName: indexName,
	}, nil
}

func (i *MeilisearchCatalogIndex) Health() error {
	if i == nil || i.client == nil {
		return fmt.Errorf("Catalog Meilisearch client is unavailable")
	}
	health, err := i.client.Health()
	if err != nil {
		return err
	}
	if health.Status != "available" {
		return fmt.Errorf("Catalog Meilisearch status is %s", health.Status)
	}
	return nil
}

func (i *MeilisearchCatalogIndex) Upsert(ctx context.Context, document CatalogSearchDocument) error {
	if err := i.ensureIndex(ctx); err != nil {
		return err
	}
	task, err := i.client.Index(i.indexName).AddDocuments([]CatalogSearchDocument{document}, "id")
	if err != nil {
		return err
	}
	return i.waitTask(ctx, task.TaskUID)
}

func (i *MeilisearchCatalogIndex) Delete(ctx context.Context, id string) error {
	if err := i.ensureIndex(ctx); err != nil {
		return err
	}
	task, err := i.client.Index(i.indexName).DeleteDocument(id)
	if err != nil {
		return err
	}
	return i.waitTask(ctx, task.TaskUID)
}

func (i *MeilisearchCatalogIndex) ensureIndex(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.ready {
		return nil
	}
	if err := i.Health(); err != nil {
		return err
	}
	existing, err := i.client.GetIndex(i.indexName)
	if err != nil {
		if !strings.Contains(err.Error(), "index_not_found") {
			return err
		}
		task, createErr := i.client.CreateIndex(&meilisearch.IndexConfig{Uid: i.indexName, PrimaryKey: "id"})
		if createErr != nil {
			return createErr
		}
		if err := i.waitTask(ctx, task.TaskUID); err != nil {
			return err
		}
	} else if existing.PrimaryKey != "id" {
		return fmt.Errorf("Catalog Meilisearch index %s primary key is %q, expected id", i.indexName, existing.PrimaryKey)
	}
	index := i.client.Index(i.indexName)
	settings := []func() (*meilisearch.TaskInfo, error){
		func() (*meilisearch.TaskInfo, error) {
			return index.UpdateSearchableAttributes(&[]string{
				"business_name", "business_description", "source_name", "source_identity", "domain_names",
				"glossary_names", "responsibility_names", "component_names",
			})
		},
		func() (*meilisearch.TaskInfo, error) {
			return index.UpdateFilterableAttributes(&[]string{
				"tenant_id", "entry_status", "entry_type", "source_status", "source_engine_id", "governance_status",
				"visibility", "primary_domain_id", "accountable_department_id",
			})
		},
		func() (*meilisearch.TaskInfo, error) { return index.UpdateSortableAttributes(&[]string{"updated_at"}) },
	}
	for _, update := range settings {
		task, err := update()
		if err != nil {
			return err
		}
		if err := i.waitTask(ctx, task.TaskUID); err != nil {
			return err
		}
	}
	i.ready = true
	return nil
}

func (i *MeilisearchCatalogIndex) waitTask(ctx context.Context, taskID int64) error {
	task, err := i.client.WaitForTask(taskID, meilisearch.WaitParams{Context: ctx, Interval: 50 * time.Millisecond})
	if err != nil {
		return err
	}
	if task == nil || task.Status != meilisearch.TaskStatusSucceeded {
		status := meilisearch.TaskStatusUnknown
		if task != nil {
			status = task.Status
		}
		return fmt.Errorf("Catalog Meilisearch task %d finished with status %s", taskID, status)
	}
	return nil
}
