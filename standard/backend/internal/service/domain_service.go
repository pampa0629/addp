package service

import (
	"context"

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
	// 检查 code 唯一性
	exists, err := s.repo.ExistsByCode(req.Code, tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}

	domain := &models.Domain{
		TenantID:    tenantID,
		Name:        req.Name,
		Code:        req.Code,
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

	// 转为树形结构
	nodeMap := make(map[int64]*DomainTree)
	for i := range domains {
		nodeMap[domains[i].ID] = &DomainTree{Domain: domains[i]}
	}

	var roots []*DomainTree
	for _, node := range nodeMap {
		if node.ParentID == nil {
			roots = append(roots, node)
		} else {
			if parent, ok := nodeMap[*node.ParentID]; ok {
				parent.Children = append(parent.Children, node)
			} else {
				// 父节点不在当前租户，作为根节点处理
				roots = append(roots, node)
			}
		}
	}

	return roots, nil
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

func (s *DomainService) DeleteDomain(ctx context.Context, id, tenantID int64) error {
	return s.deletion.Delete(ctx, tenantID, "domain", id, func(tx *gorm.DB, resourceID, resourceTenantID int64) error {
		return mapDeleteConflict(s.repo.DeleteTx(tx, resourceID, resourceTenantID), ErrDomainReferenced)
	})
}
