package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
)

const (
	FacetStatusCurrent     = "current"
	FacetStatusUnavailable = "unavailable"
	referenceBatchSize     = 200
)

type EntryFacetOption struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Code          string `json:"code,omitempty"`
	Status        string `json:"status,omitempty"`
	EngineType    string `json:"engine_type,omitempty"`
	Referenceable bool   `json:"referenceable"`
	Count         int64  `json:"count"`
}

type EntryReferenceFacet struct {
	Status  string             `json:"status" enums:"current,unavailable"`
	Options []EntryFacetOption `json:"options"`
}

type EntryFacets struct {
	View                   string              `json:"view" enums:"governance,inventory"`
	PrimaryDomains         EntryReferenceFacet `json:"primary_domains"`
	AccountableDepartments EntryReferenceFacet `json:"accountable_departments"`
	SourceEngines          EntryReferenceFacet `json:"source_engines"`
}

type EngineReferenceResolution struct {
	ID             int64
	Found          bool
	Referenceable  bool
	Name           string
	EngineType     string
	LifecycleState string
}

type EngineReferenceResolver interface {
	ResolveEngineReferences(context.Context, int64, []int64) ([]EngineReferenceResolution, error)
}

type systemClientEngineReferenceResolver struct {
	client *commonClient.SystemServiceClient
}

func NewSystemClientEngineReferenceResolver(client *commonClient.SystemServiceClient) EngineReferenceResolver {
	return &systemClientEngineReferenceResolver{client: client}
}

func (r *systemClientEngineReferenceResolver) ResolveEngineReferences(
	ctx context.Context,
	tenantID int64,
	ids []int64,
) ([]EngineReferenceResolution, error) {
	if r == nil || r.client == nil || tenantID <= 0 || len(ids) == 0 {
		return nil, errors.New("System engine reference resolver is unavailable")
	}
	descriptors, err := r.client.WithTenantID(uint(tenantID)).ListEngineRuntimeDescriptors(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]EngineReferenceResolution, len(descriptors))
	for _, descriptor := range descriptors {
		id := int64(descriptor.ID)
		if id <= 0 {
			continue
		}
		byID[id] = EngineReferenceResolution{
			ID: id, Found: true, Referenceable: descriptor.LifecycleState == "active",
			Name: descriptor.Name, EngineType: descriptor.EngineType, LifecycleState: descriptor.LifecycleState,
		}
	}
	results := make([]EngineReferenceResolution, 0, len(ids))
	for _, id := range ids {
		if result, ok := byID[id]; ok {
			results = append(results, result)
			continue
		}
		results = append(results, EngineReferenceResolution{ID: id})
	}
	return results, nil
}

func (s *EntryService) WithEngineReferenceResolver(resolver EngineReferenceResolver) *EntryService {
	s.engine = resolver
	return s
}

func (s *EntryService) ListFacets(ctx context.Context, tenantID int64, access EntryAccess, view string) (*EntryFacets, error) {
	view = normalizeEntryView(view)
	if tenantID <= 0 {
		return nil, ErrInvalidPage
	}
	if _, err := s.entriesForViewQuery(ctx, tenantID, access, view); err != nil {
		return nil, err
	}
	domainCounts, err := s.primaryDomainFacetCounts(ctx, tenantID, access, view)
	if err != nil {
		return nil, fmt.Errorf("list Catalog primary Domain facets: %w", err)
	}
	departmentCounts, err := s.accountableDepartmentFacetCounts(ctx, tenantID, access, view)
	if err != nil {
		return nil, fmt.Errorf("list Catalog accountable Department facets: %w", err)
	}
	engineCounts, err := s.sourceEngineFacetCounts(ctx, tenantID, access, view)
	if err != nil {
		return nil, fmt.Errorf("list Catalog source Engine facets: %w", err)
	}
	return &EntryFacets{
		View:                   view,
		PrimaryDomains:         s.resolveDomainFacet(ctx, tenantID, domainCounts),
		AccountableDepartments: s.resolveDepartmentFacet(ctx, tenantID, departmentCounts),
		SourceEngines:          s.resolveEngineFacet(ctx, tenantID, engineCounts),
	}, nil
}

type facetCountRow struct {
	ID    int64
	Count int64
}

func (s *EntryService) primaryDomainFacetCounts(ctx context.Context, tenantID int64, access EntryAccess, view string) (map[int64]int64, error) {
	counts := make(map[int64]int64)
	base, err := s.entriesForViewQuery(ctx, tenantID, access, view)
	if err != nil {
		return nil, err
	}
	var explicit []facetCountRow
	if err := base.
		Joins("JOIN catalog.semantic_associations AS semantic_facet ON semantic_facet.catalog_entry_id = entries.id AND semantic_facet.tenant_id = entries.tenant_id").
		Where("semantic_facet.semantic_type = ? AND semantic_facet.relation_role = ?", "domain", "primary").
		Select("semantic_facet.semantic_id AS id, COUNT(DISTINCT entries.id) AS count").
		Group("semantic_facet.semantic_id").Scan(&explicit).Error; err != nil {
		return nil, err
	}
	mergeFacetCounts(counts, explicit)

	base, err = s.entriesForViewQuery(ctx, tenantID, access, view)
	if err != nil {
		return nil, err
	}
	var ownerManaged []facetCountRow
	if err := base.
		Joins("JOIN catalog.source_bindings AS source_facet ON source_facet.catalog_entry_id = entries.id AND source_facet.tenant_id = entries.tenant_id AND source_facet.is_current = ?", true).
		Where("source_facet.source_module IN ? AND source_facet.observed_snapshot ->> 'domain_id' <> ''", []string{"model", "standard"}).
		Select("CAST(source_facet.observed_snapshot ->> 'domain_id' AS BIGINT) AS id, COUNT(DISTINCT entries.id) AS count").
		Group("CAST(source_facet.observed_snapshot ->> 'domain_id' AS BIGINT)").Scan(&ownerManaged).Error; err != nil {
		return nil, err
	}
	mergeFacetCounts(counts, ownerManaged)
	return counts, nil
}

func (s *EntryService) accountableDepartmentFacetCounts(ctx context.Context, tenantID int64, access EntryAccess, view string) (map[int64]int64, error) {
	base, err := s.entriesForViewQuery(ctx, tenantID, access, view)
	if err != nil {
		return nil, err
	}
	var rows []facetCountRow
	if err := base.
		Joins("JOIN catalog.responsibilities AS responsibility_facet ON responsibility_facet.catalog_entry_id = entries.id AND responsibility_facet.tenant_id = entries.tenant_id").
		Where("responsibility_facet.role = ? AND responsibility_facet.subject_type = ? AND responsibility_facet.status = ?", "accountable_department", "department", "active").
		Select("responsibility_facet.subject_id AS id, COUNT(DISTINCT entries.id) AS count").
		Group("responsibility_facet.subject_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[int64]int64, len(rows))
	mergeFacetCounts(counts, rows)
	return counts, nil
}

func (s *EntryService) sourceEngineFacetCounts(ctx context.Context, tenantID int64, access EntryAccess, view string) (map[int64]int64, error) {
	base, err := s.entriesForViewQuery(ctx, tenantID, access, view)
	if err != nil {
		return nil, err
	}
	var rows []facetCountRow
	if err := base.
		Joins("JOIN catalog.source_bindings AS source_facet ON source_facet.catalog_entry_id = entries.id AND source_facet.tenant_id = entries.tenant_id AND source_facet.is_current = ?", true).
		Where("source_facet.observed_snapshot ->> 'engine_id' <> ''").
		Select("CAST(source_facet.observed_snapshot ->> 'engine_id' AS BIGINT) AS id, COUNT(DISTINCT entries.id) AS count").
		Group("CAST(source_facet.observed_snapshot ->> 'engine_id' AS BIGINT)").Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[int64]int64, len(rows))
	mergeFacetCounts(counts, rows)
	return counts, nil
}

func mergeFacetCounts(target map[int64]int64, rows []facetCountRow) {
	for _, row := range rows {
		if row.ID > 0 && row.Count > 0 {
			target[row.ID] += row.Count
		}
	}
}

func (s *EntryService) resolveDomainFacet(ctx context.Context, tenantID int64, counts map[int64]int64) EntryReferenceFacet {
	ids := sortedFacetIDs(counts)
	if len(ids) == 0 {
		return currentEmptyFacet()
	}
	if s.standard == nil {
		return unavailableFacet()
	}
	results := make([]commonClient.StandardReferenceResolution, 0, len(ids))
	for start := 0; start < len(ids); start += referenceBatchSize {
		end := min(start+referenceBatchSize, len(ids))
		batch := make([]commonClient.StandardReference, 0, end-start)
		for _, id := range ids[start:end] {
			batch = append(batch, commonClient.StandardReference{ObjectType: "domain", ID: id})
		}
		resolved, err := s.standard.ResolveStandardReferences(ctx, tenantID, batch)
		if err != nil || len(resolved) != len(batch) {
			return unavailableFacet()
		}
		results = append(results, resolved...)
	}
	options := make([]EntryFacetOption, 0, len(results))
	for _, result := range results {
		if !result.Found {
			continue
		}
		options = append(options, EntryFacetOption{
			ID: strconv.FormatInt(result.ID, 10), Name: result.Name, Code: result.Code,
			Status: result.Status, Referenceable: result.Referenceable, Count: counts[result.ID],
		})
	}
	return resolvedFacet(options)
}

func (s *EntryService) resolveDepartmentFacet(ctx context.Context, tenantID int64, counts map[int64]int64) EntryReferenceFacet {
	ids := sortedFacetIDs(counts)
	if len(ids) == 0 {
		return currentEmptyFacet()
	}
	if s.system == nil {
		return unavailableFacet()
	}
	results := make([]commonClient.SystemCatalogReferenceResolution, 0, len(ids))
	for start := 0; start < len(ids); start += referenceBatchSize {
		end := min(start+referenceBatchSize, len(ids))
		batch := make([]commonClient.SystemCatalogReference, 0, end-start)
		for _, id := range ids[start:end] {
			batch = append(batch, commonClient.SystemCatalogReference{SubjectType: "department", ID: id})
		}
		resolved, err := s.system.ResolveSystemReferences(ctx, tenantID, batch)
		if err != nil || len(resolved) != len(batch) {
			return unavailableFacet()
		}
		results = append(results, resolved...)
	}
	options := make([]EntryFacetOption, 0, len(results))
	for _, result := range results {
		if !result.Found {
			continue
		}
		options = append(options, EntryFacetOption{
			ID: strconv.FormatInt(result.ID, 10), Name: result.Name, Code: result.Code,
			Status: result.Status, Referenceable: result.Referenceable, Count: counts[result.ID],
		})
	}
	return resolvedFacet(options)
}

func (s *EntryService) resolveEngineFacet(ctx context.Context, tenantID int64, counts map[int64]int64) EntryReferenceFacet {
	ids := sortedFacetIDs(counts)
	if len(ids) == 0 {
		return currentEmptyFacet()
	}
	if s.engine == nil {
		return unavailableFacet()
	}
	results, err := s.engine.ResolveEngineReferences(ctx, tenantID, ids)
	if err != nil || len(results) != len(ids) {
		return unavailableFacet()
	}
	options := make([]EntryFacetOption, 0, len(results))
	for _, result := range results {
		if !result.Found {
			continue
		}
		options = append(options, EntryFacetOption{
			ID: strconv.FormatInt(result.ID, 10), Name: result.Name, EngineType: result.EngineType,
			Status: result.LifecycleState, Referenceable: result.Referenceable, Count: counts[result.ID],
		})
	}
	return resolvedFacet(options)
}

func sortedFacetIDs(counts map[int64]int64) []int64 {
	ids := make([]int64, 0, len(counts))
	for id, count := range counts {
		if id > 0 && count > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func currentEmptyFacet() EntryReferenceFacet {
	return EntryReferenceFacet{Status: FacetStatusCurrent, Options: []EntryFacetOption{}}
}

func unavailableFacet() EntryReferenceFacet {
	return EntryReferenceFacet{Status: FacetStatusUnavailable, Options: []EntryFacetOption{}}
}

func resolvedFacet(options []EntryFacetOption) EntryReferenceFacet {
	sort.Slice(options, func(i, j int) bool {
		left, right := strings.ToLower(options[i].Name), strings.ToLower(options[j].Name)
		if left == right {
			return options[i].ID < options[j].ID
		}
		return left < right
	})
	return EntryReferenceFacet{Status: FacetStatusCurrent, Options: options}
}
