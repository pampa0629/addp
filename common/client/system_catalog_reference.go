package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type SystemCatalogReference struct {
	SubjectType string
	ID          int64
}

type SystemCatalogReferenceResolution struct {
	SubjectType      string
	ID               int64
	Found            bool
	Referenceable    bool
	Name             string
	Code             string
	Status           string
	PrincipalStatus  string
	MembershipStatus string
}

type systemCatalogReferenceWire struct {
	SubjectType string `json:"subject_type"`
	ID          string `json:"id"`
}

type systemCatalogReferenceResolutionWire struct {
	SubjectType      string `json:"subject_type"`
	ID               string `json:"id"`
	Found            bool   `json:"found"`
	Referenceable    bool   `json:"referenceable"`
	Name             string `json:"name,omitempty"`
	Code             string `json:"code,omitempty"`
	Status           string `json:"status,omitempty"`
	PrincipalStatus  string `json:"principal_status,omitempty"`
	MembershipStatus string `json:"membership_status,omitempty"`
}

type SystemCatalogReferenceCandidate struct {
	SubjectType string `json:"subject_type"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Code        string `json:"code,omitempty"`
	Status      string `json:"status"`
}

type SystemCatalogReferenceCandidateList struct {
	Data       []SystemCatalogReferenceCandidate `json:"data"`
	Total      int64                             `json:"total"`
	Page       int                               `json:"page"`
	PageSize   int                               `json:"page_size"`
	TotalPages int                               `json:"total_pages"`
}

func (c *SystemServiceClient) ResolveCatalogReferences(
	ctx context.Context,
	references []SystemCatalogReference,
) ([]SystemCatalogReferenceResolution, error) {
	if len(references) == 0 || len(references) > 200 {
		return nil, errors.New("System resolve catalog references requires 1 to 200 references")
	}
	wireReferences := make([]systemCatalogReferenceWire, 0, len(references))
	for _, reference := range references {
		if reference.ID <= 0 || (reference.SubjectType != "department" && reference.SubjectType != "user" && reference.SubjectType != "project_group") {
			return nil, errors.New("System resolve catalog references contains an invalid reference")
		}
		wireReferences = append(wireReferences, systemCatalogReferenceWire{
			SubjectType: reference.SubjectType,
			ID:          strconv.FormatInt(reference.ID, 10),
		})
	}
	var response struct {
		Results []systemCatalogReferenceResolutionWire `json:"results"`
	}
	if err := c.doTenantJSON(
		ctx,
		http.MethodPost,
		"/api/v1/system/runtime/catalog-references/resolve",
		map[string]any{"references": wireReferences},
		&response,
	); err != nil {
		return nil, fmt.Errorf("System resolve catalog references: %w", err)
	}
	if len(response.Results) != len(references) {
		return nil, errors.New("System resolve catalog references returned a result count mismatch")
	}
	results := make([]SystemCatalogReferenceResolution, 0, len(response.Results))
	for index, result := range response.Results {
		id, err := strconv.ParseInt(result.ID, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != result.ID ||
			result.SubjectType != references[index].SubjectType || id != references[index].ID {
			return nil, errors.New("System resolve catalog references returned results out of request order")
		}
		results = append(results, SystemCatalogReferenceResolution{
			SubjectType: result.SubjectType, ID: id, Found: result.Found,
			Referenceable: result.Referenceable, Name: result.Name, Code: result.Code,
			Status: result.Status, PrincipalStatus: result.PrincipalStatus,
			MembershipStatus: result.MembershipStatus,
		})
	}
	return results, nil
}

func (c *SystemServiceClient) ListCatalogReferenceCandidates(
	ctx context.Context,
	subjectType, search string,
	page, pageSize int,
) (*SystemCatalogReferenceCandidateList, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 ||
		(subjectType != "department" && subjectType != "user") ||
		page < 1 || pageSize < 1 || pageSize > 50 || len([]rune(strings.TrimSpace(search))) > 100 {
		return nil, errors.New("System list catalog reference candidates contains invalid parameters")
	}
	query := url.Values{
		"subject_type": []string{subjectType},
		"page":         []string{strconv.Itoa(page)},
		"page_size":    []string{strconv.Itoa(pageSize)},
	}
	if search = strings.TrimSpace(search); search != "" {
		query.Set("search", search)
	}
	var response SystemCatalogReferenceCandidateList
	if err := c.doTenantJSON(
		ctx, http.MethodGet, "/api/v1/system/runtime/catalog-references/candidates?"+query.Encode(), nil, &response,
	); err != nil {
		return nil, fmt.Errorf("System list catalog reference candidates: %w", err)
	}
	if response.Page != page || response.PageSize != pageSize || response.Total < 0 || response.TotalPages < 0 {
		return nil, errors.New("System list catalog reference candidates returned invalid pagination")
	}
	for _, item := range response.Data {
		id, err := strconv.ParseInt(item.ID, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != item.ID || item.SubjectType != subjectType || strings.TrimSpace(item.Name) == "" {
			return nil, errors.New("System list catalog reference candidates returned an invalid candidate")
		}
	}
	return &response, nil
}
