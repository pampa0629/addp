package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/dataquality"
)

// ErrTenantReferenceNotFound deliberately conflates a missing resource with a
// cross-tenant resource so callers cannot use validation APIs for tenant probing.
var ErrTenantReferenceNotFound = errors.New("tenant reference not found")

// ErrStandardReferenceDeleting means the Standard owner has frozen new bindings
// while coordinating a hard delete.
var ErrStandardReferenceDeleting = errors.New("standard reference deleting")

// StandardClient is the Bearer-only client for tenant-owned Standard APIs.
type StandardClient struct{ tenantHTTPClient }

func NewStandardClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *StandardClient {
	return &StandardClient{tenantHTTPClient: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
}

func (c *StandardClient) WithTenantID(tenantID uint) *StandardClient {
	if c == nil {
		return nil
	}
	return &StandardClient{tenantHTTPClient: c.tenantHTTPClient.withTenantID(tenantID)}
}

const StandardCatalogResourceChangesSchemaVersion = "standard.catalog_resource_changes/v1"

type StandardCatalogResourceChange struct {
	ChangeID       string         `json:"change_id"`
	SourceType     string         `json:"source_type"`
	SourceIdentity string         `json:"source_identity"`
	Operation      string         `json:"operation"`
	SourceVersion  string         `json:"source_version"`
	ObservedAt     time.Time      `json:"observed_at"`
	Snapshot       map[string]any `json:"snapshot"`
}

type StandardCatalogResourceChangesResponse struct {
	SchemaVersion string                          `json:"schema_version"`
	Changes       []StandardCatalogResourceChange `json:"changes"`
	NextCursor    string                          `json:"next_cursor"`
	HasMore       bool                            `json:"has_more"`
}

type StandardCatalogReference struct {
	SourceType     string `json:"source_type"`
	SourceIdentity string `json:"source_identity"`
}

type StandardCatalogReferenceResolution struct {
	SourceType     string         `json:"source_type"`
	SourceIdentity string         `json:"source_identity"`
	Found          bool           `json:"found"`
	Status         string         `json:"status,omitempty"`
	Version        int64          `json:"version,omitempty"`
	Summary        map[string]any `json:"summary,omitempty"`
	DetailPath     string         `json:"detail_path,omitempty"`
}

type ResolveStandardCatalogReferencesResponse struct {
	Results []StandardCatalogReferenceResolution `json:"results"`
}

func (c *StandardClient) ListCatalogResourceChanges(ctx context.Context, afterCursor string, limit int) (*StandardCatalogResourceChangesResponse, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || limit < 1 || limit > 500 {
		return nil, errors.New("Standard catalog resource changes require tenant context and limit between 1 and 500")
	}
	query := url.Values{"limit": []string{strconv.Itoa(limit)}}
	if strings.TrimSpace(afterCursor) != "" {
		query.Set("after_cursor", afterCursor)
	}
	var response StandardCatalogResourceChangesResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/standard/catalog-resources/changes?"+query.Encode(), nil, &response); err != nil {
		return nil, fmt.Errorf("Standard list catalog resource changes: %w", err)
	}
	if err := validateStandardCatalogChanges(&response); err != nil {
		return nil, fmt.Errorf("Standard list catalog resource changes: %w", err)
	}
	return &response, nil
}

func (c *StandardClient) ResolveCatalogReferences(ctx context.Context, references []StandardCatalogReference) (*ResolveStandardCatalogReferencesResponse, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || len(references) == 0 || len(references) > 200 {
		return nil, errors.New("Standard catalog reference resolution requires tenant context and 1 to 200 references")
	}
	for _, reference := range references {
		if !validStandardCatalogReference(reference) {
			return nil, errors.New("Standard catalog reference resolution contains an invalid reference")
		}
	}
	var response ResolveStandardCatalogReferencesResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/standard/runtime/catalog-references/resolve", map[string]any{"references": references}, &response); err != nil {
		return nil, fmt.Errorf("Standard resolve catalog references: %w", err)
	}
	if len(response.Results) != len(references) {
		return nil, errors.New("Standard catalog reference resolution returned a result count mismatch")
	}
	for index, result := range response.Results {
		requested := references[index]
		if result.SourceType != requested.SourceType || result.SourceIdentity != requested.SourceIdentity {
			return nil, errors.New("Standard catalog reference resolution returned results out of request order")
		}
		if result.Found && (result.Version <= 0 || (result.Status != "draft" && result.Status != "approved" && result.Status != "deprecated") || len(result.Summary) == 0 || strings.TrimSpace(result.DetailPath) == "") {
			return nil, errors.New("Standard catalog reference resolution returned an invalid found result")
		}
	}
	return &response, nil
}

func validateStandardCatalogChanges(response *StandardCatalogResourceChangesResponse) error {
	if response == nil || response.SchemaVersion != StandardCatalogResourceChangesSchemaVersion || strings.TrimSpace(response.NextCursor) == "" {
		return errors.New("Standard returned an invalid catalog resource change batch")
	}
	for _, change := range response.Changes {
		if strings.TrimSpace(change.ChangeID) == "" || !validStandardCatalogReference(StandardCatalogReference{SourceType: change.SourceType, SourceIdentity: change.SourceIdentity}) ||
			(change.Operation != "upsert" && change.Operation != "missing") || len(change.SourceVersion) != 20 || len(change.Snapshot) == 0 || change.ObservedAt.IsZero() {
			return errors.New("Standard returned an invalid catalog resource change")
		}
		if _, err := strconv.ParseUint(change.SourceVersion, 10, 64); err != nil {
			return errors.New("Standard returned an invalid catalog source version")
		}
	}
	return nil
}

func validStandardCatalogReference(reference StandardCatalogReference) bool {
	if reference.SourceType != "metric" {
		return false
	}
	id, err := strconv.ParseInt(reference.SourceIdentity, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == reference.SourceIdentity
}

type StandardReference struct {
	ObjectType string `json:"object_type"`
	ID         int64  `json:"id"`
}

type StandardReferenceResolution struct {
	ObjectType     string `json:"object_type"`
	ID             int64  `json:"id"`
	Found          bool   `json:"found"`
	Referenceable  bool   `json:"referenceable"`
	Name           string `json:"name,omitempty"`
	Code           string `json:"code,omitempty"`
	Status         string `json:"status,omitempty"`
	LifecycleState string `json:"lifecycle_state,omitempty"`
	Version        int64  `json:"version,omitempty"`
	RevisionID     int64  `json:"revision_id,omitempty"`
	RevisionNo     int64  `json:"revision_no,omitempty"`
}

type standardReferenceResolutionRequest struct {
	References []StandardReference `json:"references"`
}

type standardReferenceResolutionResponse struct {
	Results []StandardReferenceResolution `json:"results"`
}

type StandardReferenceCandidate struct {
	ObjectType string `json:"object_type"`
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Code       string `json:"code,omitempty"`
	Status     string `json:"status"`
	RevisionID int64  `json:"revision_id,omitempty"`
	RevisionNo int64  `json:"revision_no,omitempty"`
}

type StandardReferenceCandidateList struct {
	Data       []StandardReferenceCandidate `json:"data"`
	Total      int64                        `json:"total"`
	Page       int                          `json:"page"`
	PageSize   int                          `json:"page_size"`
	TotalPages int                          `json:"total_pages"`
}

func (c *StandardClient) ResolveReferences(
	ctx context.Context,
	references []StandardReference,
) ([]StandardReferenceResolution, error) {
	if len(references) == 0 || len(references) > 200 {
		return nil, errors.New("standard resolve references requires 1 to 200 references")
	}
	for _, reference := range references {
		if reference.ID <= 0 || (reference.ObjectType != "domain" && reference.ObjectType != "glossary" && reference.ObjectType != "element") {
			return nil, errors.New("standard resolve references contains an invalid reference")
		}
	}
	var response standardReferenceResolutionResponse
	if err := c.doJSON(
		ctx,
		http.MethodPost,
		"/api/v1/standard/references/resolve",
		standardReferenceResolutionRequest{References: references},
		&response,
	); err != nil {
		return nil, fmt.Errorf("standard resolve references: %w", err)
	}
	if len(response.Results) != len(references) {
		return nil, errors.New("standard resolve references returned a result count mismatch")
	}
	for index, result := range response.Results {
		if result.ObjectType != references[index].ObjectType || result.ID != references[index].ID {
			return nil, errors.New("standard resolve references returned results out of request order")
		}
	}
	return response.Results, nil
}

func (c *StandardClient) ListReferenceCandidates(
	ctx context.Context,
	objectType, search string,
	page, pageSize int,
) (*StandardReferenceCandidateList, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 ||
		(objectType != "domain" && objectType != "glossary" && objectType != "element") ||
		page < 1 || pageSize < 1 || pageSize > 50 || len([]rune(strings.TrimSpace(search))) > 100 {
		return nil, errors.New("standard list reference candidates contains invalid parameters")
	}
	query := url.Values{
		"object_type": []string{objectType},
		"page":        []string{strconv.Itoa(page)},
		"page_size":   []string{strconv.Itoa(pageSize)},
	}
	if search = strings.TrimSpace(search); search != "" {
		query.Set("search", search)
	}
	var response StandardReferenceCandidateList
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/standard/references/candidates?"+query.Encode(), nil, &response); err != nil {
		return nil, fmt.Errorf("standard list reference candidates: %w", err)
	}
	if response.Page != page || response.PageSize != pageSize || response.Total < 0 || response.TotalPages < 0 {
		return nil, errors.New("standard list reference candidates returned invalid pagination")
	}
	for _, item := range response.Data {
		if item.ObjectType != objectType || item.ID <= 0 || strings.TrimSpace(item.Name) == "" {
			return nil, errors.New("standard list reference candidates returned an invalid candidate")
		}
	}
	return &response, nil
}

type ElementResponse struct {
	ID              int64                    `json:"id"`
	TenantID        int64                    `json:"tenant_id"`
	ScopeType       string                   `json:"scope_type"`
	OwnerDomainID   *int64                   `json:"owner_domain_id,omitempty"`
	Code            string                   `json:"code"`
	LifecycleState  string                   `json:"lifecycle_state"`
	CurrentRevision *ElementRevisionResponse `json:"current_revision"`
	DraftRevision   *ElementRevisionResponse `json:"draft_revision"`
}

type ElementRevisionResponse struct {
	ID                   int64                `json:"id"`
	RevisionNo           int64                `json:"revision_no"`
	Status               string               `json:"status"`
	Name                 string               `json:"name"`
	DataType             string               `json:"data_type"`
	CodeSetRevisionID    *int64               `json:"code_set_revision_id"`
	CompiledQualityRules dataquality.Document `json:"compiled_quality_rules"`
}

type ElementSummary struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

type ElementRevisionBinding struct {
	ElementID       int64                            `json:"element_id,string"`
	RevisionID      int64                            `json:"element_revision_id,string"`
	RevisionNo      int64                            `json:"revision_no"`
	ScopeType       string                           `json:"scope_type"`
	OwnerDomainID   *int64                           `json:"owner_domain_id,omitempty,string"`
	Code            string                           `json:"code"`
	Name            string                           `json:"name"`
	Definition      string                           `json:"definition"`
	DataType        string                           `json:"data_type"`
	Length          *int                             `json:"length,omitempty"`
	PrecisionNum    *int                             `json:"precision_num,omitempty"`
	Scale           *int                             `json:"scale,omitempty"`
	Nullable        bool                             `json:"nullable"`
	DefaultValue    string                           `json:"default_value"`
	Format          string                           `json:"format"`
	ValueDomainKind string                           `json:"value_domain_kind"`
	RangeConstraint *StandardElementRangeConstraint  `json:"range_constraint,omitempty"`
	CodeSetRevision *StandardCodeSetRevisionSnapshot `json:"code_set_revision,omitempty"`
	UnitID          *int64                           `json:"unit_id,omitempty,string"`
	ExampleValues   []string                         `json:"example_values"`
	EffectiveFrom   time.Time                        `json:"effective_from"`
	EffectiveTo     *time.Time                       `json:"effective_to,omitempty"`
}

type StandardElementRangeConstraint struct {
	Min          *json.Number `json:"min,omitempty"`
	Max          *json.Number `json:"max,omitempty"`
	MinInclusive *bool        `json:"min_inclusive,omitempty"`
	MaxInclusive *bool        `json:"max_inclusive,omitempty"`
}

type StandardCodeItemSnapshot struct {
	Code              string `json:"code"`
	Label             string `json:"label"`
	Definition        string `json:"definition,omitempty"`
	SortOrder         int    `json:"sort_order"`
	Status            string `json:"status"`
	ReplacementItemID *int64 `json:"replacement_item_id,omitempty,string"`
}

type StandardCodeSetRevisionSnapshot struct {
	CodeSetID     int64                      `json:"code_set_id,string"`
	RevisionID    int64                      `json:"revision_id,string"`
	RevisionNo    int64                      `json:"revision_no"`
	ScopeType     string                     `json:"scope_type"`
	OwnerDomainID *int64                     `json:"owner_domain_id,omitempty,string"`
	Origin        string                     `json:"origin"`
	Code          string                     `json:"code"`
	Name          string                     `json:"name"`
	Description   string                     `json:"description"`
	ValueType     string                     `json:"value_type"`
	Status        string                     `json:"status"`
	EffectiveFrom *time.Time                 `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time                 `json:"effective_to,omitempty"`
	Items         []StandardCodeItemSnapshot `json:"items"`
}

type elementRevisionResolutionRequest struct {
	ElementIDs []string  `json:"element_ids"`
	AsOf       time.Time `json:"as_of"`
}

type ElementRevisionResolution struct {
	ElementID int64                   `json:"element_id,string"`
	Found     bool                    `json:"found"`
	Snapshot  *ElementRevisionBinding `json:"snapshot,omitempty"`
}

type ElementRevisionResolutionResponse struct {
	Results []ElementRevisionResolution `json:"results"`
}

type ElementCandidate struct {
	ID           int64                `json:"id"`
	RevisionID   int64                `json:"revision_id"`
	RevisionNo   int64                `json:"revision_no"`
	Name         string               `json:"name"`
	Code         string               `json:"code"`
	QualityRules dataquality.Document `json:"quality_rules"`
}

type elementListResponse struct {
	Data []ElementResponse `json:"data"`
}

type elementCandidateListResponse struct {
	Data  []ElementResponse `json:"data"`
	Total int64             `json:"total"`
}

type ElementQualityRulesSnapshot struct {
	ElementID         int64                `json:"element_id"`
	ElementRevisionID int64                `json:"element_revision_id"`
	RevisionNo        int64                `json:"revision_no"`
	QualityRules      dataquality.Document `json:"quality_rules"`
}

type tenantReferenceResponse struct {
	ID             int64  `json:"id"`
	TenantID       int64  `json:"tenant_id"`
	LifecycleState string `json:"lifecycle_state"`
}

func (c *StandardClient) validateTenantReference(ctx context.Context, path, resourceName string) error {
	var resource tenantReferenceResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resource); err != nil {
		return fmt.Errorf("standard validate %s: %w", resourceName, err)
	}
	if c.tenantID == nil || resource.TenantID != int64(*c.tenantID) {
		return fmt.Errorf("standard validate %s: %w", resourceName, ErrTenantReferenceNotFound)
	}
	if resource.LifecycleState != "active" {
		return fmt.Errorf("standard validate %s: %w", resourceName, ErrStandardReferenceDeleting)
	}
	return nil
}

func (c *StandardClient) ValidateElement(ctx context.Context, elementID int64) error {
	var element ElementResponse
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/standard/elements/%d", elementID), nil, &element); err != nil {
		return fmt.Errorf("standard validate element: %w", err)
	}
	if c.tenantID == nil || element.TenantID != int64(*c.tenantID) {
		return fmt.Errorf("standard validate element: %w", ErrTenantReferenceNotFound)
	}
	if element.LifecycleState != "active" {
		return fmt.Errorf("standard validate element: %w", ErrStandardReferenceDeleting)
	}
	if element.CurrentRevision == nil || element.CurrentRevision.Status != "published" {
		return fmt.Errorf("standard validate element: %w", ErrTenantReferenceNotFound)
	}
	return nil
}

func (c *StandardClient) GetElement(ctx context.Context, elementID int64) (*ElementResponse, error) {
	var element ElementResponse
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/standard/elements/%d", elementID), nil, &element); err != nil {
		return nil, fmt.Errorf("standard get element: %w", err)
	}
	return &element, nil
}

// ResolveElementRevisions resolves every stable element ID at one shared point
// in time. Missing, inactive, cross-tenant, or non-effective elements fail the
// whole call so consumers can freeze an aggregate atomically.
func (c *StandardClient) ResolveElementRevisions(ctx context.Context, elementIDs []int64, asOf time.Time) (map[int64]ElementRevisionBinding, error) {
	snapshots, err := c.ResolveElementRevisionSnapshots(ctx, elementIDs, asOf)
	if err != nil {
		return nil, err
	}
	result := make(map[int64]ElementRevisionBinding, len(snapshots))
	for elementID, snapshot := range snapshots {
		if snapshot == nil {
			return nil, fmt.Errorf("standard resolve element revisions: %w", ErrTenantReferenceNotFound)
		}
		result[elementID] = *snapshot
	}
	return result, nil
}

// ResolveElementRevisionSnapshots resolves stable element IDs at one shared
// point in time while preserving an explicit nil snapshot for elements that do
// not have an effective revision. It is the federated read-model primitive;
// aggregate writers should call ResolveElementRevisions instead.
func (c *StandardClient) ResolveElementRevisionSnapshots(ctx context.Context, elementIDs []int64, asOf time.Time) (map[int64]*ElementRevisionBinding, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || asOf.IsZero() {
		return nil, errors.New("standard resolve element revisions contains invalid parameters")
	}
	unique := make([]int64, 0, len(elementIDs))
	seen := make(map[int64]struct{}, len(elementIDs))
	for _, elementID := range elementIDs {
		if elementID <= 0 {
			return nil, errors.New("standard resolve element revisions requires positive ids")
		}
		if _, exists := seen[elementID]; exists {
			continue
		}
		seen[elementID] = struct{}{}
		unique = append(unique, elementID)
	}
	result := make(map[int64]*ElementRevisionBinding, len(unique))
	for offset := 0; offset < len(unique); offset += 200 {
		end := offset + 200
		if end > len(unique) {
			end = len(unique)
		}
		batch := unique[offset:end]
		encodedBatch := make([]string, len(batch))
		for index, elementID := range batch {
			encodedBatch[index] = strconv.FormatInt(elementID, 10)
		}
		var response ElementRevisionResolutionResponse
		if err := c.doJSON(ctx, http.MethodPost, "/api/v1/standard/runtime/element-revisions/resolve", elementRevisionResolutionRequest{
			ElementIDs: encodedBatch,
			AsOf:       asOf.UTC(),
		}, &response); err != nil {
			return nil, fmt.Errorf("standard resolve element revisions: %w", err)
		}
		if len(response.Results) != len(batch) {
			return nil, errors.New("standard resolve element revisions returned a result count mismatch")
		}
		for index, resolution := range response.Results {
			if resolution.ElementID != batch[index] {
				return nil, errors.New("standard resolve element revisions returned results out of request order")
			}
			if !resolution.Found {
				if resolution.Snapshot != nil {
					return nil, errors.New("standard resolve element revisions returned a snapshot for a missing element")
				}
				result[resolution.ElementID] = nil
				continue
			}
			if resolution.Snapshot == nil || resolution.Snapshot.ElementID != resolution.ElementID || resolution.Snapshot.RevisionID <= 0 || resolution.Snapshot.RevisionNo <= 0 || strings.TrimSpace(resolution.Snapshot.DataType) == "" || resolution.Snapshot.EffectiveFrom.IsZero() || !validStandardOwnership(resolution.Snapshot.ScopeType, resolution.Snapshot.OwnerDomainID) {
				return nil, errors.New("standard resolve element revisions returned an invalid snapshot")
			}
			if codeSet := resolution.Snapshot.CodeSetRevision; codeSet != nil {
				if !validStandardOwnership(codeSet.ScopeType, codeSet.OwnerDomainID) || !validStandardCodeSetOrigin(codeSet.Origin, codeSet.ScopeType) {
					return nil, errors.New("standard resolve element revisions returned an invalid code set snapshot")
				}
			}
			result[resolution.ElementID] = resolution.Snapshot
		}
	}
	if len(result) != len(unique) {
		return nil, errors.New("standard resolve element revisions returned incomplete results")
	}
	return result, nil
}

func validStandardCodeSetOrigin(origin, scopeType string) bool {
	return (origin == "platform" && scopeType == "platform") ||
		(origin == "tenant" && (scopeType == "tenant_common" || scopeType == "domain"))
}

func validStandardOwnership(scopeType string, ownerDomainID *int64) bool {
	switch scopeType {
	case "platform", "tenant_common":
		return ownerDomainID == nil
	case "domain":
		return ownerDomainID != nil && *ownerDomainID > 0
	default:
		return false
	}
}

func (c *StandardClient) ListElementSummaries(ctx context.Context, elementIDs []int64) ([]ElementSummary, error) {
	if len(elementIDs) == 0 {
		return []ElementSummary{}, nil
	}
	values := make([]string, len(elementIDs))
	for index, elementID := range elementIDs {
		if elementID <= 0 {
			return nil, errors.New("standard list elements requires positive ids")
		}
		values[index] = strconv.FormatInt(elementID, 10)
	}
	query := url.Values{
		"ids":       []string{strings.Join(values, ",")},
		"page":      []string{"1"},
		"page_size": []string{"100"},
	}
	var response elementListResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/standard/elements?"+query.Encode(), nil, &response); err != nil {
		return nil, fmt.Errorf("standard list elements: %w", err)
	}
	result := make([]ElementSummary, 0, len(response.Data))
	for _, element := range response.Data {
		if element.CurrentRevision == nil || element.CurrentRevision.Status != "published" {
			continue
		}
		result = append(result, ElementSummary{ID: element.ID, Name: element.CurrentRevision.Name, Code: element.Code})
	}
	return result, nil
}

func (c *StandardClient) ListElementCandidates(ctx context.Context, keyword string, page, pageSize int) ([]ElementCandidate, int64, error) {
	query := url.Values{
		"keyword":   []string{keyword},
		"status":    []string{"published"},
		"page":      []string{strconv.Itoa(page)},
		"page_size": []string{strconv.Itoa(pageSize)},
	}
	var response elementCandidateListResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/standard/elements?"+query.Encode(), nil, &response); err != nil {
		return nil, 0, fmt.Errorf("standard list element candidates: %w", err)
	}
	result := make([]ElementCandidate, 0, len(response.Data))
	for index := range response.Data {
		revision := response.Data[index].CurrentRevision
		if revision == nil || revision.Status != "published" {
			return nil, 0, errors.New("standard returned element candidate without published revision")
		}
		if err := revision.CompiledQualityRules.Validate(); err != nil {
			return nil, 0, fmt.Errorf("standard returned invalid element candidate quality rules: %w", err)
		}
		result = append(result, ElementCandidate{ID: response.Data[index].ID, RevisionID: revision.ID, RevisionNo: revision.RevisionNo, Name: revision.Name, Code: response.Data[index].Code, QualityRules: revision.CompiledQualityRules})
	}
	return result, response.Total, nil
}

func (c *StandardClient) GetElementQualityRules(ctx context.Context, elementID int64) (*ElementQualityRulesSnapshot, error) {
	var result ElementQualityRulesSnapshot
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/standard/elements/%d/quality-rules", elementID), nil, &result); err != nil {
		return nil, fmt.Errorf("standard get element quality rules: %w", err)
	}
	if result.ElementID != elementID || result.ElementRevisionID <= 0 || result.RevisionNo <= 0 {
		return nil, errors.New("standard returned invalid element revision identity")
	}
	if err := result.QualityRules.Validate(); err != nil {
		return nil, fmt.Errorf("standard returned invalid element quality rules: %w", err)
	}
	return &result, nil
}

func (c *StandardClient) ValidateDomain(ctx context.Context, domainID int64) error {
	return c.validateTenantReference(ctx, fmt.Sprintf("/api/v1/standard/domains/%d", domainID), "domain")
}

func (c *StandardClient) ValidateMetric(ctx context.Context, metricID int64) error {
	return c.validateTenantReference(ctx, fmt.Sprintf("/api/v1/standard/metrics/%d", metricID), "metric")
}
