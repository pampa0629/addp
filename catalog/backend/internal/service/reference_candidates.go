package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
)

const (
	ReferenceCandidateDomain      = "domain"
	ReferenceCandidateGlossary    = "glossary"
	ReferenceCandidateElement     = "element"
	ReferenceCandidateDepartment  = "department"
	ReferenceCandidateUser        = "user"
	maxReferenceCandidatePageSize = 50
)

type ReferenceCandidate struct {
	ReferenceType string `json:"reference_type" enums:"domain,glossary,element,department,user"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Code          string `json:"code,omitempty"`
	Status        string `json:"status"`
}

type ReferenceCandidateList struct {
	Data       []ReferenceCandidate `json:"data"`
	Total      int64                `json:"total"`
	Page       int                  `json:"page"`
	PageSize   int                  `json:"page_size"`
	TotalPages int                  `json:"total_pages"`
}

type ReferenceCandidateResolver interface {
	ListReferenceCandidates(context.Context, int64, string, string, int, int) (*ReferenceCandidateList, error)
}

type standardClientCandidateResolver struct{ client *commonClient.StandardClient }

func NewStandardClientCandidateResolver(client *commonClient.StandardClient) ReferenceCandidateResolver {
	return &standardClientCandidateResolver{client: client}
}

func (r *standardClientCandidateResolver) ListReferenceCandidates(
	ctx context.Context, tenantID int64, referenceType, search string, page, pageSize int,
) (*ReferenceCandidateList, error) {
	if r == nil || r.client == nil || tenantID <= 0 {
		return nil, errors.New("Standard reference candidate resolver is unavailable")
	}
	response, err := r.client.WithTenantID(uint(tenantID)).ListReferenceCandidates(ctx, referenceType, search, page, pageSize)
	if err != nil {
		return nil, err
	}
	result := &ReferenceCandidateList{Data: make([]ReferenceCandidate, 0, len(response.Data)), Total: response.Total, Page: response.Page, PageSize: response.PageSize, TotalPages: response.TotalPages}
	for _, item := range response.Data {
		result.Data = append(result.Data, ReferenceCandidate{
			ReferenceType: item.ObjectType, ID: strconv.FormatInt(item.ID, 10),
			Name: item.Name, Code: item.Code, Status: item.Status,
		})
	}
	return result, nil
}

type systemClientCandidateResolver struct {
	client *commonClient.SystemServiceClient
}

func NewSystemClientCandidateResolver(client *commonClient.SystemServiceClient) ReferenceCandidateResolver {
	return &systemClientCandidateResolver{client: client}
}

func (r *systemClientCandidateResolver) ListReferenceCandidates(
	ctx context.Context, tenantID int64, referenceType, search string, page, pageSize int,
) (*ReferenceCandidateList, error) {
	if r == nil || r.client == nil || tenantID <= 0 {
		return nil, errors.New("System reference candidate resolver is unavailable")
	}
	response, err := r.client.WithTenantID(uint(tenantID)).ListCatalogReferenceCandidates(ctx, referenceType, search, page, pageSize)
	if err != nil {
		return nil, err
	}
	result := &ReferenceCandidateList{Data: make([]ReferenceCandidate, 0, len(response.Data)), Total: response.Total, Page: response.Page, PageSize: response.PageSize, TotalPages: response.TotalPages}
	for _, item := range response.Data {
		result.Data = append(result.Data, ReferenceCandidate{
			ReferenceType: item.SubjectType, ID: item.ID,
			Name: item.Name, Code: item.Code, Status: item.Status,
		})
	}
	return result, nil
}

func (s *EntryService) WithReferenceCandidateResolvers(standard, system ReferenceCandidateResolver) *EntryService {
	s.standardCandidates = standard
	s.systemCandidates = system
	return s
}

func (s *EntryService) ListReferenceCandidates(
	ctx context.Context,
	tenantID int64,
	referenceType, search string,
	page, pageSize int,
) (*ReferenceCandidateList, error) {
	search = strings.TrimSpace(search)
	if s == nil || tenantID <= 0 || page < 1 || pageSize < 1 || pageSize > maxReferenceCandidatePageSize || len([]rune(search)) > 100 {
		return nil, ErrInvalidPage
	}
	var resolver ReferenceCandidateResolver
	switch referenceType {
	case ReferenceCandidateDomain, ReferenceCandidateGlossary, ReferenceCandidateElement:
		resolver = s.standardCandidates
	case ReferenceCandidateDepartment, ReferenceCandidateUser:
		resolver = s.systemCandidates
	default:
		return nil, ErrInvalidPage
	}
	if resolver == nil {
		return nil, ErrReferenceValidationUnavailable
	}
	result, err := resolver.ListReferenceCandidates(ctx, tenantID, referenceType, search, page, pageSize)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrReferenceValidationUnavailable, err)
	}
	if result == nil || result.Page != page || result.PageSize != pageSize || result.Total < 0 || result.TotalPages < 0 {
		return nil, ErrReferenceValidationUnavailable
	}
	for _, item := range result.Data {
		if item.ReferenceType != referenceType || !canonicalPositiveID(item.ID) || strings.TrimSpace(item.Name) == "" {
			return nil, ErrReferenceValidationUnavailable
		}
	}
	return result, nil
}

func canonicalPositiveID(value string) bool {
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == value
}
