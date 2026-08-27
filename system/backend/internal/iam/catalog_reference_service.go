package iam

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const MaxCatalogReferenceBatchSize = 200
const MaxCatalogReferenceCandidatePageSize = 50

var ErrInvalidCatalogReferenceRequest = errors.New("invalid catalog reference request")

type CatalogSubjectType string

const (
	CatalogSubjectTypeDepartment   CatalogSubjectType = "department"
	CatalogSubjectTypeUser         CatalogSubjectType = "user"
	CatalogSubjectTypeProjectGroup CatalogSubjectType = "project_group"
)

type CatalogReference struct {
	SubjectType CatalogSubjectType
	ID          int64
}

type CatalogReferenceResolution struct {
	SubjectType      CatalogSubjectType
	ID               int64
	Found            bool
	Referenceable    bool
	Name             string
	Code             string
	Status           string
	PrincipalStatus  string
	MembershipStatus string
}

type CatalogReferenceCandidate struct {
	SubjectType CatalogSubjectType
	ID          int64
	Name        string
	Code        string
	Status      string
}

type CatalogDepartmentProjection struct {
	ID     int64
	Name   string
	Code   string
	Status string
}

type CatalogUserProjection struct {
	ID               int64
	DisplayName      string
	PrincipalStatus  string
	MembershipStatus string
	Referenceable    bool
}

type CatalogProjectGroupProjection struct {
	ID     int64
	Name   string
	Code   string
	Status string
}

type catalogReferenceRepository interface {
	ResolveCatalogDepartments(context.Context, int64, []int64) ([]CatalogDepartmentProjection, error)
	ResolveCatalogUsers(context.Context, int64, []int64) ([]CatalogUserProjection, error)
	ResolveCatalogProjectGroups(context.Context, int64, []int64) ([]CatalogProjectGroupProjection, error)
	ListCatalogDepartmentCandidates(context.Context, int64, string, int, int) ([]CatalogReferenceCandidate, int64, error)
	ListCatalogUserCandidates(context.Context, int64, string, int, int) ([]CatalogReferenceCandidate, int64, error)
}

type CatalogReferenceService struct {
	repository catalogReferenceRepository
}

func NewCatalogReferenceService(repository catalogReferenceRepository) (*CatalogReferenceService, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", ErrInvalidCatalogReferenceRequest)
	}
	return &CatalogReferenceService{repository: repository}, nil
}

func (s *CatalogReferenceService) Resolve(
	ctx context.Context,
	tenantID int64,
	serviceClientID string,
	references []CatalogReference,
) ([]CatalogReferenceResolution, error) {
	if s == nil || s.repository == nil || tenantID <= 0 || serviceClientID != "addp-catalog" ||
		len(references) == 0 || len(references) > MaxCatalogReferenceBatchSize {
		return nil, ErrInvalidCatalogReferenceRequest
	}

	idsByType := map[CatalogSubjectType][]int64{
		CatalogSubjectTypeDepartment:   {},
		CatalogSubjectTypeUser:         {},
		CatalogSubjectTypeProjectGroup: {},
	}
	for _, reference := range references {
		if reference.ID <= 0 {
			return nil, ErrInvalidCatalogReferenceRequest
		}
		if _, ok := idsByType[reference.SubjectType]; !ok {
			return nil, fmt.Errorf("%w: unsupported subject_type %q", ErrInvalidCatalogReferenceRequest, reference.SubjectType)
		}
		idsByType[reference.SubjectType] = append(idsByType[reference.SubjectType], reference.ID)
	}

	departments, err := s.repository.ResolveCatalogDepartments(ctx, tenantID, uniqueCatalogReferenceIDs(idsByType[CatalogSubjectTypeDepartment]))
	if err != nil {
		return nil, err
	}
	users, err := s.repository.ResolveCatalogUsers(ctx, tenantID, uniqueCatalogReferenceIDs(idsByType[CatalogSubjectTypeUser]))
	if err != nil {
		return nil, err
	}
	projectGroups, err := s.repository.ResolveCatalogProjectGroups(ctx, tenantID, uniqueCatalogReferenceIDs(idsByType[CatalogSubjectTypeProjectGroup]))
	if err != nil {
		return nil, err
	}

	resolved := make(map[string]CatalogReferenceResolution, len(departments)+len(users)+len(projectGroups))
	for _, department := range departments {
		resolved[catalogReferenceKey(CatalogSubjectTypeDepartment, department.ID)] = CatalogReferenceResolution{
			SubjectType: CatalogSubjectTypeDepartment, ID: department.ID, Found: true,
			Referenceable: department.Status == "active", Name: department.Name,
			Code: department.Code, Status: department.Status,
		}
	}
	for _, user := range users {
		key := catalogReferenceKey(CatalogSubjectTypeUser, user.ID)
		current, exists := resolved[key]
		if exists && (current.Referenceable || !user.Referenceable) {
			continue
		}
		resolved[key] = CatalogReferenceResolution{
			SubjectType: CatalogSubjectTypeUser, ID: user.ID, Found: true,
			Referenceable: user.Referenceable, Name: user.DisplayName,
			Status: user.MembershipStatus, PrincipalStatus: user.PrincipalStatus,
			MembershipStatus: user.MembershipStatus,
		}
	}
	for _, projectGroup := range projectGroups {
		resolved[catalogReferenceKey(CatalogSubjectTypeProjectGroup, projectGroup.ID)] = CatalogReferenceResolution{
			SubjectType: CatalogSubjectTypeProjectGroup, ID: projectGroup.ID, Found: true,
			Referenceable: projectGroup.Status != string(ProjectGroupStatusClosed), Name: projectGroup.Name,
			Code: projectGroup.Code, Status: projectGroup.Status,
		}
	}

	results := make([]CatalogReferenceResolution, 0, len(references))
	for _, reference := range references {
		if result, ok := resolved[catalogReferenceKey(reference.SubjectType, reference.ID)]; ok {
			results = append(results, result)
			continue
		}
		results = append(results, CatalogReferenceResolution{SubjectType: reference.SubjectType, ID: reference.ID})
	}
	return results, nil
}

func (s *CatalogReferenceService) ListCandidates(
	ctx context.Context,
	tenantID int64,
	serviceClientID string,
	subjectType CatalogSubjectType,
	search string,
	page, pageSize int,
) ([]CatalogReferenceCandidate, int64, error) {
	search = strings.TrimSpace(search)
	if s == nil || s.repository == nil || tenantID <= 0 || serviceClientID != "addp-catalog" ||
		page < 1 || pageSize < 1 || pageSize > MaxCatalogReferenceCandidatePageSize || len([]rune(search)) > 100 {
		return nil, 0, ErrInvalidCatalogReferenceRequest
	}
	switch subjectType {
	case CatalogSubjectTypeDepartment:
		return s.repository.ListCatalogDepartmentCandidates(ctx, tenantID, search, page, pageSize)
	case CatalogSubjectTypeUser:
		return s.repository.ListCatalogUserCandidates(ctx, tenantID, search, page, pageSize)
	default:
		return nil, 0, ErrInvalidCatalogReferenceRequest
	}
}

func uniqueCatalogReferenceIDs(ids []int64) []int64 {
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

func catalogReferenceKey(subjectType CatalogSubjectType, id int64) string {
	return fmt.Sprintf("%s:%d", subjectType, id)
}
