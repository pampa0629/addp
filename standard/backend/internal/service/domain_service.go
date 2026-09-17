package service

import (
	"context"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/gorm"
)

type DomainService struct {
	repo     *repository.DomainRepository
	refs     *repository.TenantReferenceRepository
	deletion *StandardReferenceDeletionService
}

func NewDomainService(repo *repository.DomainRepository, refs *repository.TenantReferenceRepository, deletion *StandardReferenceDeletionService) *DomainService {
	return &DomainService{repo: repo, refs: refs, deletion: deletion}
}

func (s *DomainService) CreateDomain(req *models.CreateDomainRequest, tenantID, userID int64) (*models.Domain, error) {
	if err := s.refs.RequireDomain(tenantID, req.ParentID); err != nil {
		return nil, err
	}
	code, err := normalizeStandardStableCode(req.Code, maxStandardCategoryCodeLength)
	if err != nil {
		return nil, err
	}
	// 检查 code 唯一性
	exists, err := s.repo.ExistsByCode(code, tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}

	domain := &models.Domain{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Code:        code,
		Description: req.Description,
		ParentID:    req.ParentID,
		Icon:        req.Icon,
		SortOrder:   req.SortOrder,
		CreatedBy:   userID,
	}

	if err := s.repo.Create(domain); err != nil {
		return nil, err
	}
	return domain, nil
}

func (s *DomainService) GetDomain(id, tenantID int64) (*models.Domain, error) {
	return s.repo.GetByID(id, tenantID)
}

// DomainTree 带子节点的业务域
type DomainTree struct {
	models.Domain
	Children []*DomainTree `json:"children,omitempty"`
}

func (s *DomainService) ListDomainsAsTree(tenantID int64) ([]*DomainTree, error) {
	domains, err := s.repo.List(tenantID)
	if err != nil {
		return nil, err
	}

	tree, _, err := buildDomainHierarchy(domains)
	return tree, err
}

func (s *DomainService) UpdateDomain(id, tenantID, userID int64, req *models.UpdateDomainRequest) (*models.Domain, error) {
	domain, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.validateParent(id, tenantID, req.ParentID); err != nil {
		return nil, err
	}

	if req.Name != "" {
		domain.Name = req.Name
	}
	domain.Description = req.Description
	domain.ParentID = req.ParentID
	domain.Icon = req.Icon
	domain.SortOrder = req.SortOrder
	domain.UpdatedBy = &userID

	if err := s.repo.Update(domain, req.Version); err != nil {
		return nil, err
	}
	return s.repo.GetByID(id, tenantID)
}

func (s *DomainService) validateParent(id, tenantID int64, parentID *int64) error {
	if err := s.refs.RequireDomain(tenantID, parentID); err != nil {
		return err
	}
	for current := parentID; current != nil; {
		if *current == id {
			return ErrDomainParentCycle
		}
		parent, err := s.repo.GetByID(*current, tenantID)
		if err != nil {
			return err
		}
		current = parent.ParentID
	}
	return nil
}

func (s *DomainService) DeleteDomain(ctx context.Context, id, tenantID, expectedVersion int64) error {
	return s.deletion.Delete(ctx, tenantID, "domain", id, func(tx *gorm.DB, resourceID, resourceTenantID int64) error {
		return mapDeleteConflict(s.repo.DeleteVersionedTx(tx, resourceID, resourceTenantID, expectedVersion), ErrDomainReferenced)
	})
}
