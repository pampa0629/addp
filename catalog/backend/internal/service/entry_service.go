package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EntryAccess struct {
	Inventory     bool
	DepartmentIDs []int64
}

type EntryListFilter struct {
	View              string
	Search            string
	EntryType         string
	SourceStatus      string
	SourceIdentity    string
	GovernanceStatus  string
	Visibility        string
	PrimaryDomainID   int64
	DepartmentID      int64
	SourceEngineID    int64
	CoverageDimension string
	CoverageState     string
	Page              int
	PageSize          int
}

const (
	EntryViewGovernance = "governance"
	EntryViewInventory  = "inventory"
)

type EntrySummary struct {
	models.Entry
	DisplayName    string `json:"display_name"`
	SourceStatus   string `json:"source_status"`
	SourceIdentity string `json:"source_identity"`
	SourceEngineID int64  `json:"source_engine_id,omitempty,string" swaggertype:"string"`
}

type EntryDetail struct {
	models.Entry
	DisplayName          string                               `json:"display_name"`
	RecommendedSuccessor *EntrySummary                        `json:"recommended_successor,omitempty"`
	Source               *models.SourceBinding                `json:"source,omitempty"`
	Components           []models.Component                   `json:"components"`
	SemanticLinks        []models.SemanticAssociation         `json:"semantic_links"`
	Responsibilities     []models.Responsibility              `json:"responsibilities"`
	ComponentElements    []models.ComponentElementAssociation `json:"component_elements"`
	SourceResolution     *SourceResolution                    `json:"source_resolution,omitempty"`
	QualitySummary       *QualitySummary                      `json:"quality_summary,omitempty"`
}

type QualitySummary struct {
	Status              string     `json:"status"`
	Configured          bool       `json:"configured"`
	CheckTaskID         int64      `json:"check_task_id,omitempty"`
	LastExecutionID     string     `json:"last_execution_id,omitempty"`
	LastExecutionStatus string     `json:"last_execution_status,omitempty"`
	QualityScore        *float64   `json:"quality_score,omitempty"`
	OpenIssueCount      int64      `json:"open_issue_count"`
	LastObservedAt      *time.Time `json:"last_observed_at,omitempty"`
	DetailPath          string     `json:"detail_path,omitempty"`
}

type QualitySummaryResolver interface {
	ResolveCatalogSummaries(context.Context, int64, []commonClient.QualityCatalogSummaryReference) ([]commonClient.QualityCatalogSummaryResolution, error)
}

type qualityClientSummaryResolver struct{ client *commonClient.QualityClient }

func NewQualityClientSummaryResolver(client *commonClient.QualityClient) QualitySummaryResolver {
	return &qualityClientSummaryResolver{client: client}
}

func (r *qualityClientSummaryResolver) ResolveCatalogSummaries(ctx context.Context, tenantID int64, references []commonClient.QualityCatalogSummaryReference) ([]commonClient.QualityCatalogSummaryResolution, error) {
	if r == nil || r.client == nil || tenantID <= 0 {
		return nil, fmt.Errorf("Quality summary resolver is unavailable")
	}
	result, err := r.client.WithTenantID(uint(tenantID)).ResolveCatalogSummaries(ctx, references)
	if err != nil {
		return nil, err
	}
	return result.Results, nil
}

type SourceResolution struct {
	Status         string         `json:"status"`
	OwnerStatus    string         `json:"owner_status,omitempty"`
	OwnerVersion   int64          `json:"owner_version,omitempty"`
	Summary        map[string]any `json:"summary,omitempty"`
	DetailPath     string         `json:"detail_path,omitempty"`
	ResolvedAt     *time.Time     `json:"resolved_at,omitempty"`
	LastObservedAt time.Time      `json:"last_observed_at"`
}

type ProfessionalSourceReference struct {
	SourceType     string
	SourceIdentity string
}

type ProfessionalSourceResult struct {
	Found      bool
	Status     string
	Version    int64
	Summary    map[string]any
	DetailPath string
}

type ProfessionalSourceResolver interface {
	SourceModule() string
	ResolveSources(context.Context, int64, []ProfessionalSourceReference) ([]ProfessionalSourceResult, error)
}

type modelClientSourceResolver struct{ client *commonClient.ModelClient }

func NewModelClientSourceResolver(client *commonClient.ModelClient) ProfessionalSourceResolver {
	return &modelClientSourceResolver{client: client}
}

func (*modelClientSourceResolver) SourceModule() string { return models.SourceModuleModel }

func (r *modelClientSourceResolver) ResolveSources(ctx context.Context, tenantID int64, references []ProfessionalSourceReference) ([]ProfessionalSourceResult, error) {
	if r == nil || r.client == nil || tenantID <= 0 {
		return nil, fmt.Errorf("Model source resolver is unavailable")
	}
	request := make([]commonClient.ModelCatalogReference, 0, len(references))
	for _, reference := range references {
		request = append(request, commonClient.ModelCatalogReference{SourceType: reference.SourceType, SourceIdentity: reference.SourceIdentity})
	}
	result, err := r.client.WithTenantID(uint(tenantID)).ResolveCatalogReferences(ctx, request)
	if err != nil {
		return nil, err
	}
	resolved := make([]ProfessionalSourceResult, 0, len(result.Results))
	for _, item := range result.Results {
		resolved = append(resolved, ProfessionalSourceResult{Found: item.Found, Status: item.Status, Version: item.Version, Summary: item.Summary, DetailPath: item.DetailPath})
	}
	return resolved, nil
}

type standardClientSourceResolver struct{ client *commonClient.StandardClient }

func NewStandardClientSourceResolver(client *commonClient.StandardClient) ProfessionalSourceResolver {
	return &standardClientSourceResolver{client: client}
}

type serviceClientSourceResolver struct{ client *commonClient.ServiceClient }

func NewServiceClientSourceResolver(client *commonClient.ServiceClient) ProfessionalSourceResolver {
	return &serviceClientSourceResolver{client: client}
}

type developClientSourceResolver struct{ client *commonClient.DevelopClient }

func NewDevelopClientSourceResolver(client *commonClient.DevelopClient) ProfessionalSourceResolver {
	return &developClientSourceResolver{client: client}
}

type workbenchClientSourceResolver struct{ client *commonClient.WorkbenchClient }

func NewWorkbenchClientSourceResolver(client *commonClient.WorkbenchClient) ProfessionalSourceResolver {
	return &workbenchClientSourceResolver{client: client}
}

func (*workbenchClientSourceResolver) SourceModule() string { return models.SourceModuleWorkbench }

func (r *workbenchClientSourceResolver) ResolveSources(ctx context.Context, tenantID int64, references []ProfessionalSourceReference) ([]ProfessionalSourceResult, error) {
	if r == nil || r.client == nil || tenantID <= 0 {
		return nil, fmt.Errorf("Workbench source resolver is unavailable")
	}
	request := make([]commonClient.WorkbenchCatalogReference, 0, len(references))
	for _, reference := range references {
		request = append(request, commonClient.WorkbenchCatalogReference{SourceType: reference.SourceType, SourceIdentity: reference.SourceIdentity})
	}
	result, err := r.client.WithTenantID(uint(tenantID)).ResolveCatalogReferences(ctx, request)
	if err != nil {
		return nil, err
	}
	resolved := make([]ProfessionalSourceResult, 0, len(result.Results))
	for _, item := range result.Results {
		resolved = append(resolved, ProfessionalSourceResult{Found: item.Found, Status: item.Status, Version: item.Version, Summary: item.Summary, DetailPath: item.DetailPath})
	}
	return resolved, nil
}

func (*developClientSourceResolver) SourceModule() string { return models.SourceModuleDevelop }

func (r *developClientSourceResolver) ResolveSources(ctx context.Context, tenantID int64, references []ProfessionalSourceReference) ([]ProfessionalSourceResult, error) {
	if r == nil || r.client == nil || tenantID <= 0 {
		return nil, fmt.Errorf("Develop source resolver is unavailable")
	}
	request := make([]commonClient.DevelopCatalogReference, 0, len(references))
	for _, reference := range references {
		request = append(request, commonClient.DevelopCatalogReference{SourceType: reference.SourceType, SourceIdentity: reference.SourceIdentity})
	}
	result, err := r.client.WithTenantID(uint(tenantID)).ResolveCatalogReferences(ctx, request)
	if err != nil {
		return nil, err
	}
	resolved := make([]ProfessionalSourceResult, 0, len(result.Results))
	for _, item := range result.Results {
		resolved = append(resolved, ProfessionalSourceResult{Found: item.Found, Status: item.Status, Version: item.Version, Summary: item.Summary, DetailPath: item.DetailPath})
	}
	return resolved, nil
}

func (*serviceClientSourceResolver) SourceModule() string { return models.SourceModuleService }

func (r *serviceClientSourceResolver) ResolveSources(ctx context.Context, tenantID int64, references []ProfessionalSourceReference) ([]ProfessionalSourceResult, error) {
	if r == nil || r.client == nil || tenantID <= 0 {
		return nil, fmt.Errorf("Service source resolver is unavailable")
	}
	request := make([]commonClient.ServiceCatalogReference, 0, len(references))
	for _, reference := range references {
		request = append(request, commonClient.ServiceCatalogReference{SourceType: reference.SourceType, SourceIdentity: reference.SourceIdentity})
	}
	result, err := r.client.WithTenantID(uint(tenantID)).ResolveCatalogReferences(ctx, request)
	if err != nil {
		return nil, err
	}
	resolved := make([]ProfessionalSourceResult, 0, len(result.Results))
	for _, item := range result.Results {
		resolved = append(resolved, ProfessionalSourceResult{Found: item.Found, Status: item.Status, Version: item.Version, Summary: item.Summary, DetailPath: item.DetailPath})
	}
	return resolved, nil
}

func (*standardClientSourceResolver) SourceModule() string { return models.SourceModuleStandard }

func (r *standardClientSourceResolver) ResolveSources(ctx context.Context, tenantID int64, references []ProfessionalSourceReference) ([]ProfessionalSourceResult, error) {
	if r == nil || r.client == nil || tenantID <= 0 {
		return nil, fmt.Errorf("Standard source resolver is unavailable")
	}
	request := make([]commonClient.StandardCatalogReference, 0, len(references))
	for _, reference := range references {
		request = append(request, commonClient.StandardCatalogReference{SourceType: reference.SourceType, SourceIdentity: reference.SourceIdentity})
	}
	result, err := r.client.WithTenantID(uint(tenantID)).ResolveCatalogReferences(ctx, request)
	if err != nil {
		return nil, err
	}
	resolved := make([]ProfessionalSourceResult, 0, len(result.Results))
	for _, item := range result.Results {
		resolved = append(resolved, ProfessionalSourceResult{Found: item.Found, Status: item.Status, Version: item.Version, Summary: item.Summary, DetailPath: item.DetailPath})
	}
	return resolved, nil
}

type EntryListResult struct {
	Data       []EntrySummary `json:"data"`
	Total      int64          `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
}

type EntryService struct {
	db                 *gorm.DB
	standard           StandardReferenceResolver
	system             SystemReferenceResolver
	standardCandidates ReferenceCandidateResolver
	systemCandidates   ReferenceCandidateResolver
	engine             EngineReferenceResolver
	search             CatalogSearchResolver
	sources            map[string]ProfessionalSourceResolver
	quality            QualitySummaryResolver
	metaFields         MetaFieldResolver
	elementRevisions   StandardElementRevisionResolver
}

func (s *EntryService) WithQualitySummaryResolver(resolver QualitySummaryResolver) *EntryService {
	s.quality = resolver
	return s
}

func NewEntryService(db *gorm.DB, standard StandardReferenceResolver, system SystemReferenceResolver) *EntryService {
	return &EntryService{db: db, standard: standard, system: system, sources: make(map[string]ProfessionalSourceResolver)}
}

func (s *EntryService) WithSearch(search CatalogSearchResolver) *EntryService {
	s.search = search
	return s
}

func (s *EntryService) WithProfessionalSourceResolvers(resolvers ...ProfessionalSourceResolver) *EntryService {
	for _, resolver := range resolvers {
		if resolver != nil && strings.TrimSpace(resolver.SourceModule()) != "" {
			s.sources[resolver.SourceModule()] = resolver
		}
	}
	return s
}

func (s *EntryService) List(ctx context.Context, tenantID int64, access EntryAccess, filter EntryListFilter) (*EntryListResult, error) {
	filter.View = normalizeEntryView(filter.View)
	filter.Search = strings.TrimSpace(filter.Search)
	filter.SourceIdentity = strings.TrimSpace(filter.SourceIdentity)
	filter.CoverageDimension = strings.TrimSpace(filter.CoverageDimension)
	filter.CoverageState = strings.TrimSpace(filter.CoverageState)
	if tenantID <= 0 || filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 200 || !validEntryListFilter(filter) {
		return nil, ErrInvalidPage
	}
	query, err := s.entriesForViewQuery(ctx, tenantID, access, filter.View)
	if err != nil {
		return nil, err
	}
	query = query.
		Joins("JOIN catalog.source_bindings AS source ON source.catalog_entry_id = entries.id AND source.tenant_id = entries.tenant_id AND source.is_current = ?", true)
	var searchOrder map[uuid.UUID]int
	var searchTotal *int64
	if filter.Search != "" {
		if s.search == nil {
			return nil, ErrSearchUnavailable
		}
		ids, total, err := s.search.SearchCatalogEntries(ctx, tenantID, access, filter)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSearchUnavailable, err)
		}
		searchTotal = &total
		if len(ids) == 0 {
			return &EntryListResult{Data: []EntrySummary{}, Total: total, Page: filter.Page, PageSize: filter.PageSize, TotalPages: totalPages(total, filter.PageSize)}, nil
		}
		searchOrder = make(map[uuid.UUID]int, len(ids))
		for index, id := range ids {
			searchOrder[id] = index
		}
		query = query.Where("entries.id IN ?", ids)
	}
	if filter.EntryType != "" {
		query = query.Where("entries.entry_type = ?", filter.EntryType)
	}
	if filter.SourceStatus != "" {
		query = query.Where("source.source_status = ?", filter.SourceStatus)
	}
	if filter.SourceIdentity != "" {
		query = query.Where("source.source_identity = ?", filter.SourceIdentity)
	}
	if filter.GovernanceStatus != "" {
		query = query.Where("entries.governance_status = ?", filter.GovernanceStatus)
	}
	if filter.Visibility != "" {
		query = query.Where("entries.visibility = ?", filter.Visibility)
	}
	if filter.PrimaryDomainID > 0 {
		query = applyPrimaryDomainFilter(query, filter.PrimaryDomainID)
	}
	if filter.DepartmentID > 0 {
		query = applyAccountableDepartmentFilter(query, filter.DepartmentID)
	}
	if filter.SourceEngineID > 0 {
		query = query.Where("CAST(source.observed_snapshot ->> 'engine_id' AS BIGINT) = ?", filter.SourceEngineID)
	}
	if filter.CoverageDimension != "" {
		query = applyMissingCoverageFilter(query, filter.CoverageDimension)
	}
	var total int64
	if searchTotal != nil {
		total = *searchTotal
	} else if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count Catalog entries: %w", err)
	}
	type entryRow struct {
		models.Entry
		SourceStatus     string
		SourceIdentity   string
		ObservedSnapshot commonModels.JSONMap
	}
	var rows []entryRow
	rowsQuery := query.Select("entries.*, source.source_status, source.source_identity, source.observed_snapshot")
	if searchOrder == nil {
		rowsQuery = rowsQuery.Order("entries.updated_at DESC, entries.id ASC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize)
	}
	if err := rowsQuery.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list Catalog entries: %w", err)
	}
	if searchOrder != nil {
		sort.Slice(rows, func(i, j int) bool { return searchOrder[rows[i].ID] < searchOrder[rows[j].ID] })
	}
	data := make([]EntrySummary, 0, len(rows))
	for _, row := range rows {
		name, _ := row.ObservedSnapshot["name"].(string)
		engineID, _ := numericInt64(row.ObservedSnapshot["engine_id"])
		if row.BusinessName != nil && strings.TrimSpace(*row.BusinessName) != "" {
			name = *row.BusinessName
		}
		data = append(data, EntrySummary{
			Entry: row.Entry, DisplayName: name, SourceStatus: row.SourceStatus,
			SourceIdentity: row.SourceIdentity, SourceEngineID: engineID,
		})
	}
	return &EntryListResult{Data: data, Total: total, Page: filter.Page, PageSize: filter.PageSize, TotalPages: totalPages(total, filter.PageSize)}, nil
}

func validEntryListFilter(filter EntryListFilter) bool {
	_, validCoverageDimension := coveragePredicateFor(filter.CoverageDimension)
	validCoverageFilter := (filter.CoverageDimension == "" && filter.CoverageState == "") ||
		(filter.View == EntryViewInventory && validCoverageDimension && filter.CoverageState == CoverageStateMissing && filter.Search == "")
	return oneOf(filter.View, EntryViewGovernance, EntryViewInventory) &&
		validEntryType(filter.EntryType) &&
		(filter.SourceStatus == "" || oneOf(filter.SourceStatus, models.SourceStatusActive, models.SourceStatusMissing)) &&
		(filter.GovernanceStatus == "" || oneOf(filter.GovernanceStatus, models.GovernanceStatusDiscovered, models.GovernanceStatusCurated, models.GovernanceStatusCertified, models.GovernanceStatusDeprecated)) &&
		(filter.Visibility == "" || oneOf(filter.Visibility, models.VisibilityInventory, models.VisibilityDepartment, models.VisibilityTenant)) &&
		len(filter.SourceIdentity) <= 255 && filter.PrimaryDomainID >= 0 && filter.DepartmentID >= 0 && filter.SourceEngineID >= 0 && validCoverageFilter
}

func validEntryType(entryType string) bool {
	return entryType == "" || oneOf(entryType,
		models.EntryTypeDataItem,
		models.EntryTypeBusinessEntity,
		models.EntryTypeLogicalModel,
		models.EntryTypeMetric,
		models.EntryTypeDataService,
		models.EntryTypeDevelopmentArtifact,
		models.EntryTypeDataApplication,
	)
}

func applyPrimaryDomainFilter(query *gorm.DB, domainID int64) *gorm.DB {
	if domainID <= 0 {
		return query
	}
	return query.Where(`EXISTS (
		SELECT 1 FROM catalog.semantic_associations semantic_filter
		WHERE semantic_filter.tenant_id = entries.tenant_id
		  AND semantic_filter.catalog_entry_id = entries.id
		  AND semantic_filter.semantic_type = 'domain'
		  AND semantic_filter.relation_role = 'primary'
		  AND semantic_filter.semantic_id = ?
	) OR EXISTS (
		SELECT 1 FROM catalog.source_bindings domain_source_filter
		WHERE domain_source_filter.tenant_id = entries.tenant_id
		  AND domain_source_filter.catalog_entry_id = entries.id
		  AND domain_source_filter.is_current = TRUE
		  AND domain_source_filter.source_module IN ('model', 'standard')
		  AND domain_source_filter.observed_snapshot ->> 'domain_id' = ?
	)`, domainID, fmt.Sprintf("%d", domainID))
}

func applyAccountableDepartmentFilter(query *gorm.DB, departmentID int64) *gorm.DB {
	if departmentID <= 0 {
		return query
	}
	return query.Where(`EXISTS (
		SELECT 1 FROM catalog.responsibilities responsibility_filter
		WHERE responsibility_filter.tenant_id = entries.tenant_id
		  AND responsibility_filter.catalog_entry_id = entries.id
		  AND responsibility_filter.role = 'accountable_department'
		  AND responsibility_filter.subject_type = 'department'
		  AND responsibility_filter.status = 'active'
		  AND responsibility_filter.subject_id = ?
	)`, departmentID)
}

func normalizeEntryView(view string) string {
	view = strings.TrimSpace(view)
	if view == "" {
		return EntryViewGovernance
	}
	return view
}

func (s *EntryService) entriesForViewQuery(ctx context.Context, tenantID int64, access EntryAccess, view string) (*gorm.DB, error) {
	view = normalizeEntryView(view)
	if view != EntryViewGovernance && view != EntryViewInventory {
		return nil, ErrInvalidPage
	}
	if view == EntryViewInventory && !access.Inventory {
		return nil, ErrInventoryPermissionRequired
	}
	query := s.visibleEntriesQuery(ctx, tenantID, access)
	query = query.Where("entries.entry_status = ?", models.EntryStatusActive)
	if view == EntryViewGovernance {
		query = query.Where("entries.governance_status IN ?", []string{
			models.GovernanceStatusCurated,
			models.GovernanceStatusCertified,
			models.GovernanceStatusDeprecated,
		})
	}
	return query, nil
}

func totalPages(total int64, pageSize int) int {
	if total == 0 {
		return 0
	}
	return int((total + int64(pageSize) - 1) / int64(pageSize))
}

func (s *EntryService) Get(ctx context.Context, tenantID int64, access EntryAccess, id uuid.UUID) (*EntryDetail, error) {
	var entry models.Entry
	if err := s.visibleEntriesQuery(ctx, tenantID, access).Where("entries.id = ?", id).First(&entry).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrEntryNotFound
		}
		return nil, fmt.Errorf("get Catalog entry: %w", err)
	}
	if entry.EntryStatus == models.EntryStatusMerged {
		return &EntryDetail{Entry: entry, DisplayName: ""}, nil
	}
	var source models.SourceBinding
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND catalog_entry_id = ? AND is_current = ?", tenantID, id, true).First(&source).Error; err != nil {
		return nil, fmt.Errorf("get Catalog source: %w", err)
	}
	var components []models.Component
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, id).
		Order("ordinal ASC, component_key ASC").Find(&components).Error; err != nil {
		return nil, fmt.Errorf("get Catalog components: %w", err)
	}
	var semanticLinks []models.SemanticAssociation
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, id).
		Order("semantic_type ASC, relation_role ASC, semantic_id ASC").Find(&semanticLinks).Error; err != nil {
		return nil, fmt.Errorf("get Catalog semantic links: %w", err)
	}
	var responsibilities []models.Responsibility
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, id).
		Order("role ASC, subject_id ASC").Find(&responsibilities).Error; err != nil {
		return nil, fmt.Errorf("get Catalog responsibilities: %w", err)
	}
	var componentElements []models.ComponentElementAssociation
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, id).
		Order("component_id ASC").Find(&componentElements).Error; err != nil {
		return nil, fmt.Errorf("get Catalog component elements: %w", err)
	}
	displayName, _ := source.ObservedSnapshot["name"].(string)
	if entry.BusinessName != nil && strings.TrimSpace(*entry.BusinessName) != "" {
		displayName = *entry.BusinessName
	}
	detail := &EntryDetail{
		Entry: entry, DisplayName: displayName, Source: &source, Components: components,
		SemanticLinks: semanticLinks, Responsibilities: responsibilities, ComponentElements: componentElements,
	}
	if entry.RecommendedSuccessorEntryID != nil {
		successor, err := s.getVisibleEntrySummary(ctx, tenantID, access, *entry.RecommendedSuccessorEntryID)
		if err != nil && !errors.Is(err, ErrEntryNotFound) {
			return nil, err
		}
		detail.RecommendedSuccessor = successor
	}
	if source.SourceModule != models.SourceModuleMeta {
		detail.SourceResolution = s.resolveProfessionalSource(ctx, tenantID, source)
	} else {
		detail.QualitySummary = s.resolveQualitySummary(ctx, tenantID, source)
	}
	return detail, nil
}

func (s *EntryService) getVisibleEntrySummary(ctx context.Context, tenantID int64, access EntryAccess, id uuid.UUID) (*EntrySummary, error) {
	type entryRow struct {
		models.Entry
		SourceStatus     string
		SourceIdentity   string
		ObservedSnapshot commonModels.JSONMap
	}
	var row entryRow
	query := s.visibleEntriesQuery(ctx, tenantID, access).
		Select("entries.*, source.source_status, source.source_identity, source.observed_snapshot").
		Joins("JOIN catalog.source_bindings AS source ON source.catalog_entry_id = entries.id AND source.tenant_id = entries.tenant_id AND source.is_current = ?", true).
		Where("entries.id = ?", id)
	if err := query.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEntryNotFound
		}
		return nil, fmt.Errorf("get recommended Catalog successor: %w", err)
	}
	displayName, _ := row.ObservedSnapshot["name"].(string)
	if row.BusinessName != nil && strings.TrimSpace(*row.BusinessName) != "" {
		displayName = *row.BusinessName
	}
	engineID, _ := numericInt64(row.ObservedSnapshot["engine_id"])
	return &EntrySummary{Entry: row.Entry, DisplayName: displayName, SourceStatus: row.SourceStatus,
		SourceIdentity: row.SourceIdentity, SourceEngineID: engineID}, nil
}

func (s *EntryService) resolveQualitySummary(ctx context.Context, tenantID int64, source models.SourceBinding) *QualitySummary {
	if source.SourceStatus != models.SourceStatusActive || source.SourceType != models.SourceTypeDataItem {
		return nil
	}
	itemType, _ := source.ObservedSnapshot["item_type"].(string)
	schemaName, _ := source.ObservedSnapshot["schema_name"].(string)
	tableName, _ := source.ObservedSnapshot["table_name"].(string)
	engineID, ok := numericInt64(source.ObservedSnapshot["engine_id"])
	if itemType != "table" || !ok || engineID <= 0 || strings.TrimSpace(schemaName) == "" || strings.TrimSpace(tableName) == "" {
		return nil
	}
	summary := &QualitySummary{Status: "unavailable"}
	if s == nil || s.quality == nil {
		return summary
	}
	results, err := s.quality.ResolveCatalogSummaries(ctx, tenantID, []commonClient.QualityCatalogSummaryReference{{EngineID: engineID, SchemaName: schemaName, TableName: tableName}})
	if err != nil || len(results) != 1 {
		return summary
	}
	result := results[0]
	if !result.Configured {
		summary.Status = "not_configured"
		return summary
	}
	summary.Status = "current"
	summary.Configured = true
	summary.CheckTaskID = result.CheckTaskID
	summary.LastExecutionID = result.LastExecutionID
	summary.LastExecutionStatus = result.LastExecutionStatus
	summary.QualityScore = result.QualityScore
	summary.OpenIssueCount = result.OpenIssueCount
	summary.LastObservedAt = result.LastObservedAt
	summary.DetailPath = result.DetailPath
	return summary
}

func (s *EntryService) resolveProfessionalSource(ctx context.Context, tenantID int64, source models.SourceBinding) *SourceResolution {
	resolution := &SourceResolution{
		Status:         "unavailable",
		Summary:        map[string]any(source.ObservedSnapshot),
		LastObservedAt: source.ObservedAt,
	}
	if source.SourceStatus == models.SourceStatusMissing {
		resolution.Status = "missing"
		resolution.Summary = map[string]any(source.ObservedSnapshot)
		return resolution
	}
	if s == nil || s.sources == nil || s.sources[source.SourceModule] == nil {
		return resolution
	}
	results, err := s.sources[source.SourceModule].ResolveSources(ctx, tenantID, []ProfessionalSourceReference{{SourceType: source.SourceType, SourceIdentity: source.SourceIdentity}})
	if err != nil || len(results) != 1 {
		return resolution
	}
	result := results[0]
	if !result.Found {
		resolution.Status = "missing"
		resolution.Summary = map[string]any(source.ObservedSnapshot)
		return resolution
	}
	now := time.Now().UTC()
	resolution.Status = "current"
	resolution.OwnerStatus = result.Status
	resolution.OwnerVersion = result.Version
	resolution.Summary = result.Summary
	resolution.DetailPath = result.DetailPath
	resolution.ResolvedAt = &now
	return resolution
}

func (s *EntryService) visibleEntriesQuery(ctx context.Context, tenantID int64, access EntryAccess) *gorm.DB {
	query := s.db.WithContext(ctx).Table("catalog.entries AS entries").Where("entries.tenant_id = ?", tenantID)
	if access.Inventory {
		return query
	}
	query = query.Where("entries.visibility IN ?", []string{models.VisibilityTenant, models.VisibilityDepartment})
	if len(access.DepartmentIDs) == 0 {
		return query.Where("entries.visibility = ?", models.VisibilityTenant)
	}
	return query.Where(`entries.visibility = ? OR (
		entries.visibility = ? AND EXISTS (
			SELECT 1 FROM catalog.responsibilities responsibility
			WHERE responsibility.tenant_id = entries.tenant_id
			  AND responsibility.catalog_entry_id = entries.id
			  AND responsibility.role = 'accountable_department'
			  AND responsibility.subject_type = 'department'
			  AND responsibility.status = 'active'
			  AND responsibility.subject_id IN ?
		)
	)`, models.VisibilityTenant, models.VisibilityDepartment, access.DepartmentIDs)
}

func numericInt64(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}
