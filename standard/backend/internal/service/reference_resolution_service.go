package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/addp/standard/internal/models"
)

const MaxReferenceResolutionBatchSize = 200
const MaxReferenceCandidatePageSize = 50

var ErrInvalidReferenceResolutionRequest = errors.New("invalid standard reference resolution request")

type ReferenceType string

const (
	ReferenceTypeDomain   ReferenceType = "domain"
	ReferenceTypeGlossary ReferenceType = "glossary"
	ReferenceTypeElement  ReferenceType = "element"
)

type ReferenceResolutionRequest struct {
	ObjectType ReferenceType `json:"object_type" binding:"required" enums:"domain,glossary,element"`
	ID         int64         `json:"id,omitempty" minimum:"1"`
	Code       string        `json:"code,omitempty"`
}

type ReferenceResolution struct {
	DomainPath     []string      `json:"domain_path,omitempty"` // 业务域完整名称路径 / Full domain name path.
	ObjectType     ReferenceType `json:"object_type"`
	ID             int64         `json:"id"`
	Found          bool          `json:"found"`
	Referenceable  bool          `json:"referenceable"`
	Name           string        `json:"name,omitempty"`
	Code           string        `json:"code,omitempty"`
	Status         string        `json:"status,omitempty"`
	LifecycleState string        `json:"lifecycle_state,omitempty"`
	Version        int64         `json:"version,omitempty"`
	RevisionID     int64         `json:"revision_id,omitempty"`
	RevisionNo     int64         `json:"revision_no,omitempty"`
}

type ReferenceCandidate struct {
	DomainPath []string      `json:"domain_path,omitempty"` // 业务域完整名称路径 / Full domain name path.
	ObjectType ReferenceType `json:"object_type"`
	ID         int64         `json:"id"`
	Name       string        `json:"name"`
	Code       string        `json:"code,omitempty"`
	Status     string        `json:"status"`
	RevisionID int64         `json:"revision_id,omitempty"`
	RevisionNo int64         `json:"revision_no,omitempty"`
}

type ReferenceCandidateList struct {
	Data       []ReferenceCandidate `json:"data"`
	Total      int64                `json:"total"`
	Page       int                  `json:"page"`
	PageSize   int                  `json:"page_size"`
	TotalPages int                  `json:"total_pages"`
}

type ReferenceResolutionRepository interface {
	ResolveDomains(ctx context.Context, tenantID int64, ids []int64) ([]models.Domain, error)
	ResolveGlossaries(ctx context.Context, tenantID int64, ids []int64) ([]models.PublishedGlossaryReference, error)
	ResolveElements(ctx context.Context, tenantID int64, ids []int64) ([]models.PublishedElementReference, error)
	ResolveDomainsByCodes(ctx context.Context, tenantID int64, codes []string) ([]models.Domain, error)
	ResolveGlossariesByCodes(ctx context.Context, tenantID int64, codes []string) ([]models.PublishedGlossaryReference, error)
	ResolveElementsByCodes(ctx context.Context, tenantID int64, codes []string) ([]models.PublishedElementReference, error)
	ListDomains(ctx context.Context, tenantID int64) ([]models.Domain, error)
	ListGlossaryCandidates(ctx context.Context, tenantID int64, search string, page, pageSize int) ([]models.PublishedGlossaryReference, int64, error)
	ListElementCandidates(ctx context.Context, tenantID int64, search string, page, pageSize int) ([]models.PublishedElementReference, int64, error)
}

type ReferenceResolutionService struct {
	repository ReferenceResolutionRepository
}

func NewReferenceResolutionService(repository ReferenceResolutionRepository) *ReferenceResolutionService {
	return &ReferenceResolutionService{repository: repository}
}

func (s *ReferenceResolutionService) Resolve(
	ctx context.Context,
	tenantID int64,
	references []ReferenceResolutionRequest,
) ([]ReferenceResolution, error) {
	if s == nil || s.repository == nil || tenantID <= 0 || len(references) == 0 || len(references) > MaxReferenceResolutionBatchSize {
		return nil, ErrInvalidReferenceResolutionRequest
	}

	idsByType := map[ReferenceType][]int64{
		ReferenceTypeDomain: {}, ReferenceTypeGlossary: {}, ReferenceTypeElement: {},
	}
	codesByType := map[ReferenceType][]string{
		ReferenceTypeDomain: {}, ReferenceTypeGlossary: {}, ReferenceTypeElement: {},
	}
	for _, reference := range references {
		if _, ok := idsByType[reference.ObjectType]; !ok {
			return nil, fmt.Errorf("%w: unsupported object_type %q", ErrInvalidReferenceResolutionRequest, reference.ObjectType)
		}
		if (reference.ID > 0) == (reference.Code != "") || (reference.ID == 0 && !validReferenceCode(reference.ObjectType, reference.Code)) {
			return nil, ErrInvalidReferenceResolutionRequest
		}
		if reference.ID > 0 {
			idsByType[reference.ObjectType] = append(idsByType[reference.ObjectType], reference.ID)
		} else {
			codesByType[reference.ObjectType] = append(codesByType[reference.ObjectType], reference.Code)
		}
	}

	domains, err := s.repository.ResolveDomains(ctx, tenantID, uniqueReferenceIDs(idsByType[ReferenceTypeDomain]))
	if err != nil {
		return nil, err
	}
	glossaries, err := s.repository.ResolveGlossaries(ctx, tenantID, uniqueReferenceIDs(idsByType[ReferenceTypeGlossary]))
	if err != nil {
		return nil, err
	}
	elements, err := s.repository.ResolveElements(ctx, tenantID, uniqueReferenceIDs(idsByType[ReferenceTypeElement]))
	if err != nil {
		return nil, err
	}
	domainsByCode, err := s.repository.ResolveDomainsByCodes(ctx, tenantID, uniqueReferenceCodes(codesByType[ReferenceTypeDomain]))
	if err != nil {
		return nil, err
	}
	glossariesByCode, err := s.repository.ResolveGlossariesByCodes(ctx, tenantID, uniqueReferenceCodes(codesByType[ReferenceTypeGlossary]))
	if err != nil {
		return nil, err
	}
	elementsByCode, err := s.repository.ResolveElementsByCodes(ctx, tenantID, uniqueReferenceCodes(codesByType[ReferenceTypeElement]))
	if err != nil {
		return nil, err
	}

	domainPaths := map[int64][]string{}
	if len(domains)+len(domainsByCode) > 0 {
		allDomains, err := s.repository.ListDomains(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		_, domainPaths, err = buildDomainHierarchy(allDomains)
		if err != nil {
			return nil, err
		}
	}

	resolved := make(map[string]ReferenceResolution, len(domains)+len(glossaries)+len(elements))
	for _, domain := range domains {
		resolved[referenceResolutionKey(ReferenceTypeDomain, domain.ID)] = ReferenceResolution{
			ObjectType: ReferenceTypeDomain, ID: domain.ID, Found: true,
			Referenceable: domain.LifecycleState == "active", Name: domain.Name, Code: domain.Code,
			Status: domain.LifecycleState, LifecycleState: domain.LifecycleState, Version: domain.Version, DomainPath: domainPaths[domain.ID],
		}
	}
	for _, glossary := range glossaries {
		resolved[referenceResolutionKey(ReferenceTypeGlossary, glossary.ID)] = ReferenceResolution{
			ObjectType: ReferenceTypeGlossary, ID: glossary.ID, Found: true,
			Referenceable: glossary.Status == models.RevisionStatusPublished && glossary.LifecycleState == "active",
			Name:          glossary.Name, Code: glossary.Code, Status: glossary.Status,
			LifecycleState: glossary.LifecycleState, Version: glossary.Version,
			RevisionID: glossary.RevisionID, RevisionNo: glossary.RevisionNo,
		}
	}
	for _, element := range elements {
		resolved[referenceResolutionKey(ReferenceTypeElement, element.ID)] = ReferenceResolution{
			ObjectType: ReferenceTypeElement, ID: element.ID, Found: true,
			Referenceable: element.Status == models.RevisionStatusPublished && element.LifecycleState == "active",
			Name:          element.Name, Code: element.Code, Status: element.Status,
			LifecycleState: element.LifecycleState, Version: element.Version, RevisionID: element.RevisionID, RevisionNo: element.RevisionNo,
		}
	}
	for _, domain := range domainsByCode {
		resolved[referenceCodeResolutionKey(ReferenceTypeDomain, domain.Code)] = ReferenceResolution{
			ObjectType: ReferenceTypeDomain, ID: domain.ID, Found: true,
			Referenceable: domain.LifecycleState == "active", Name: domain.Name, Code: domain.Code,
			Status: domain.LifecycleState, LifecycleState: domain.LifecycleState, Version: domain.Version, DomainPath: domainPaths[domain.ID],
		}
	}
	for _, glossary := range glossariesByCode {
		resolved[referenceCodeResolutionKey(ReferenceTypeGlossary, glossary.Code)] = ReferenceResolution{
			ObjectType: ReferenceTypeGlossary, ID: glossary.ID, Found: true,
			Referenceable: glossary.Status == models.RevisionStatusPublished && glossary.LifecycleState == "active",
			Name:          glossary.Name, Code: glossary.Code, Status: glossary.Status,
			LifecycleState: glossary.LifecycleState, Version: glossary.Version,
			RevisionID: glossary.RevisionID, RevisionNo: glossary.RevisionNo,
		}
	}
	for _, element := range elementsByCode {
		resolved[referenceCodeResolutionKey(ReferenceTypeElement, element.Code)] = ReferenceResolution{
			ObjectType: ReferenceTypeElement, ID: element.ID, Found: true,
			Referenceable: element.Status == models.RevisionStatusPublished && element.LifecycleState == "active",
			Name:          element.Name, Code: element.Code, Status: element.Status,
			LifecycleState: element.LifecycleState, Version: element.Version, RevisionID: element.RevisionID, RevisionNo: element.RevisionNo,
		}
	}

	results := make([]ReferenceResolution, 0, len(references))
	for _, reference := range references {
		key := referenceResolutionKey(reference.ObjectType, reference.ID)
		if reference.Code != "" {
			key = referenceCodeResolutionKey(reference.ObjectType, reference.Code)
		}
		if result, ok := resolved[key]; ok {
			results = append(results, result)
			continue
		}
		results = append(results, ReferenceResolution{ObjectType: reference.ObjectType, ID: reference.ID, Code: reference.Code})
	}
	return results, nil
}

func validReferenceCode(objectType ReferenceType, code string) bool {
	maxLength := maxStandardStableCodeLength
	if objectType == ReferenceTypeDomain {
		maxLength = maxStandardCategoryCodeLength
	}
	return validStandardStableCode(code, maxLength)
}

func (s *ReferenceResolutionService) ListCandidates(
	ctx context.Context,
	tenantID int64,
	objectType ReferenceType,
	search string,
	page, pageSize int,
) (*ReferenceCandidateList, error) {
	search = strings.TrimSpace(search)
	if s == nil || s.repository == nil || tenantID <= 0 || page < 1 || pageSize < 1 || pageSize > MaxReferenceCandidatePageSize || len([]rune(search)) > 100 {
		return nil, ErrInvalidReferenceResolutionRequest
	}
	result := &ReferenceCandidateList{Data: []ReferenceCandidate{}, Page: page, PageSize: pageSize}
	switch objectType {
	case ReferenceTypeDomain:
		domains, err := s.repository.ListDomains(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		tree, paths, err := buildDomainHierarchy(domains)
		if err != nil {
			return nil, err
		}
		keyword := strings.ToLower(search)
		candidates := []ReferenceCandidate{}
		var visit func([]*DomainTree)
		visit = func(nodes []*DomainTree) {
			for _, node := range nodes {
				path := paths[node.ID]
				if node.LifecycleState == "active" && (keyword == "" || strings.Contains(strings.ToLower(strings.Join(path, " / ")+" / "+node.Code), keyword)) {
					candidates = append(candidates, ReferenceCandidate{ObjectType: objectType, ID: node.ID, Name: node.Name, Code: node.Code, Status: node.LifecycleState, DomainPath: path})
				}
				visit(node.Children)
			}
		}
		visit(tree)
		result.Total = int64(len(candidates))
		if page <= (len(candidates)+pageSize-1)/pageSize {
			start := (page - 1) * pageSize
			result.Data = candidates[start:min(start+pageSize, len(candidates))]
		}
	case ReferenceTypeGlossary:
		items, total, err := s.repository.ListGlossaryCandidates(ctx, tenantID, search, page, pageSize)
		if err != nil {
			return nil, err
		}
		result.Total = total
		for _, item := range items {
			result.Data = append(result.Data, ReferenceCandidate{ObjectType: objectType, ID: item.ID, Name: item.Name, Code: item.Code, Status: item.Status, RevisionID: item.RevisionID, RevisionNo: item.RevisionNo})
		}
	case ReferenceTypeElement:
		items, total, err := s.repository.ListElementCandidates(ctx, tenantID, search, page, pageSize)
		if err != nil {
			return nil, err
		}
		result.Total = total
		for _, item := range items {
			result.Data = append(result.Data, ReferenceCandidate{ObjectType: objectType, ID: item.ID, Name: item.Name, Code: item.Code, Status: item.Status, RevisionID: item.RevisionID, RevisionNo: item.RevisionNo})
		}
	default:
		return nil, ErrInvalidReferenceResolutionRequest
	}
	result.TotalPages = int((result.Total + int64(pageSize) - 1) / int64(pageSize))
	return result, nil
}

func uniqueReferenceIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func uniqueReferenceCodes(codes []string) []string {
	if len(codes) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(codes))
	result := make([]string, 0, len(codes))
	for _, code := range codes {
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		result = append(result, code)
	}
	return result
}

func referenceResolutionKey(objectType ReferenceType, id int64) string {
	return fmt.Sprintf("%s:%d", objectType, id)
}

func referenceCodeResolutionKey(objectType ReferenceType, code string) string {
	return fmt.Sprintf("%s:code:%s", objectType, code)
}
