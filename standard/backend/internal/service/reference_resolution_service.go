package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/addp/standard/internal/models"
)

const MaxReferenceResolutionBatchSize = 200

var ErrInvalidReferenceResolutionRequest = errors.New("invalid standard reference resolution request")

type ReferenceType string

const (
	ReferenceTypeDomain   ReferenceType = "domain"
	ReferenceTypeGlossary ReferenceType = "glossary"
	ReferenceTypeElement  ReferenceType = "element"
)

type ReferenceResolutionRequest struct {
	ObjectType ReferenceType `json:"object_type" binding:"required" enums:"domain,glossary,element"`
	ID         int64         `json:"id" binding:"required,gt=0" minimum:"1"`
}

type ReferenceResolution struct {
	ObjectType     ReferenceType `json:"object_type"`
	ID             int64         `json:"id"`
	Found          bool          `json:"found"`
	Referenceable  bool          `json:"referenceable"`
	Name           string        `json:"name,omitempty"`
	Code           string        `json:"code,omitempty"`
	Status         string        `json:"status,omitempty"`
	LifecycleState string        `json:"lifecycle_state,omitempty"`
	Version        int64         `json:"version,omitempty"`
}

type ReferenceResolutionRepository interface {
	ResolveDomains(ctx context.Context, tenantID int64, ids []int64) ([]models.Domain, error)
	ResolveGlossaries(ctx context.Context, tenantID int64, ids []int64) ([]models.Glossary, error)
	ResolveElements(ctx context.Context, tenantID int64, ids []int64) ([]models.Element, error)
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
	for _, reference := range references {
		if reference.ID <= 0 {
			return nil, ErrInvalidReferenceResolutionRequest
		}
		if _, ok := idsByType[reference.ObjectType]; !ok {
			return nil, fmt.Errorf("%w: unsupported object_type %q", ErrInvalidReferenceResolutionRequest, reference.ObjectType)
		}
		idsByType[reference.ObjectType] = append(idsByType[reference.ObjectType], reference.ID)
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

	resolved := make(map[string]ReferenceResolution, len(domains)+len(glossaries)+len(elements))
	for _, domain := range domains {
		resolved[referenceResolutionKey(ReferenceTypeDomain, domain.ID)] = ReferenceResolution{
			ObjectType: ReferenceTypeDomain, ID: domain.ID, Found: true,
			Referenceable: domain.LifecycleState == "active", Name: domain.Name, Code: domain.Code,
			Status: domain.LifecycleState, LifecycleState: domain.LifecycleState, Version: domain.Version,
		}
	}
	for _, glossary := range glossaries {
		resolved[referenceResolutionKey(ReferenceTypeGlossary, glossary.ID)] = ReferenceResolution{
			ObjectType: ReferenceTypeGlossary, ID: glossary.ID, Found: true,
			Referenceable: glossary.Status == "approved", Name: glossary.Name,
			Status: glossary.Status, Version: glossary.Version,
		}
	}
	for _, element := range elements {
		resolved[referenceResolutionKey(ReferenceTypeElement, element.ID)] = ReferenceResolution{
			ObjectType: ReferenceTypeElement, ID: element.ID, Found: true,
			Referenceable: element.Status == "approved" && element.LifecycleState == "active",
			Name:          element.Name, Code: element.Code, Status: element.Status,
			LifecycleState: element.LifecycleState, Version: element.Version,
		}
	}

	results := make([]ReferenceResolution, 0, len(references))
	for _, reference := range references {
		if result, ok := resolved[referenceResolutionKey(reference.ObjectType, reference.ID)]; ok {
			results = append(results, result)
			continue
		}
		results = append(results, ReferenceResolution{ObjectType: reference.ObjectType, ID: reference.ID})
	}
	return results, nil
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

func referenceResolutionKey(objectType ReferenceType, id int64) string {
	return fmt.Sprintf("%s:%d", objectType, id)
}
