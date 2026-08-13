package service

import (
	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

type CodeSetService struct {
	repo *repository.CodeSetRepository
}

func NewCodeSetService(repo *repository.CodeSetRepository) *CodeSetService {
	return &CodeSetService{repo: repo}
}

// CreateCodeSet 创建码值集
func (s *CodeSetService) CreateCodeSet(tenantID int64, req *models.CreateCodeSetRequest) (*models.CodeSet, error) {
	// 校验 code 唯一性
	exists, err := s.repo.ExistsByCode(tenantID, req.Code, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}

	// 校验 type
	if req.Type == "" {
		req.Type = "custom"
	}
	if req.Type != "system" && req.Type != "custom" {
		return nil, ErrInvalidCodeSetType
	}

	codeSet := &models.CodeSet{
		TenantID:    tenantID,
		Code:        req.Code,
		Name:        req.Name,
		Type:        req.Type,
		Description: req.Description,
	}

	if err := s.repo.Create(codeSet); err != nil {
		return nil, err
	}

	return codeSet, nil
}

// GetCodeSet 获取码值集
func (s *CodeSetService) GetCodeSet(id, tenantID int64) (*models.CodeSet, error) {
	return s.repo.GetByID(id, tenantID)
}

// ListCodeSets 获取码值集列表
func (s *CodeSetService) ListCodeSets(tenantID int64, keyword, codeSetType string, page, pageSize int) ([]models.CodeSet, int64, error) {
	return s.repo.List(tenantID, keyword, codeSetType, page, pageSize)
}

// UpdateCodeSet 更新码值集
func (s *CodeSetService) UpdateCodeSet(id, tenantID int64, req *models.UpdateCodeSetRequest) (*models.CodeSet, error) {
	codeSet, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}

	// 校验 type
	if req.Type != "" && req.Type != "system" && req.Type != "custom" {
		return nil, ErrInvalidCodeSetType
	}

	codeSet.Name = req.Name
	if req.Type != "" {
		codeSet.Type = req.Type
	}
	codeSet.Description = req.Description

	if err := s.repo.Update(codeSet, req.Version); err != nil {
		return nil, err
	}
	return s.repo.GetByID(id, tenantID)
}

// DeleteCodeSet 删除码值集
func (s *CodeSetService) DeleteCodeSet(id, tenantID int64) error {
	codeSet, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return err
	}

	// 系统内置码值集禁止删除
	if codeSet.Type == "system" {
		return ErrSystemCodeSetImmutable
	}

	return mapDeleteConflict(s.repo.Delete(id, tenantID), ErrCodeSetReferenced)
}

// GetCodeItems 获取码值项列表
func (s *CodeSetService) GetCodeItems(codeSetID, tenantID int64) ([]models.CodeItem, error) {
	// 验证码值集是否属于当前租户
	_, err := s.repo.GetByID(codeSetID, tenantID)
	if err != nil {
		return nil, err
	}

	return s.repo.GetItems(codeSetID)
}

// CreateCodeItem 创建码值项
func (s *CodeSetService) CreateCodeItem(codeSetID, tenantID int64, req *models.CreateCodeItemRequest) (*models.CodeItemMutationResponse, error) {
	// 验证码值集是否属于当前租户
	_, err := s.repo.GetByID(codeSetID, tenantID)
	if err != nil {
		return nil, err
	}

	// 校验 code 唯一性
	exists, err := s.repo.ExistsItemByCode(codeSetID, req.Code, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}

	item := &models.CodeItem{
		CodeSetID:   codeSetID,
		Code:        req.Code,
		Value:       req.Value,
		Description: req.Description,
		SortOrder:   req.SortOrder,
		IsActive:    req.IsActive,
	}

	if err := s.repo.CreateItem(item, tenantID, req.Version); err != nil {
		return nil, err
	}
	return &models.CodeItemMutationResponse{Item: item, Version: req.Version + 1}, nil
}

// UpdateCodeItem 更新码值项
func (s *CodeSetService) UpdateCodeItem(codeSetID, itemID, tenantID int64, req *models.UpdateCodeItemRequest) (*models.CodeItemMutationResponse, error) {
	// 验证码值集是否属于当前租户
	_, err := s.repo.GetByID(codeSetID, tenantID)
	if err != nil {
		return nil, err
	}

	item, err := s.repo.GetItemByID(itemID, codeSetID)
	if err != nil {
		return nil, err
	}

	item.Value = req.Value
	item.Description = req.Description
	item.SortOrder = req.SortOrder
	item.IsActive = req.IsActive

	if err := s.repo.UpdateItem(item, tenantID, req.Version); err != nil {
		return nil, err
	}
	return &models.CodeItemMutationResponse{Item: item, Version: req.Version + 1}, nil
}

// DeleteCodeItem 删除码值项
func (s *CodeSetService) DeleteCodeItem(codeSetID, itemID, tenantID, version int64) error {
	// 验证码值集是否属于当前租户
	_, err := s.repo.GetByID(codeSetID, tenantID)
	if err != nil {
		return err
	}

	_, err = s.repo.GetItemByID(itemID, codeSetID)
	if err != nil {
		return err
	}

	return mapDeleteConflict(s.repo.DeleteItem(itemID, codeSetID, tenantID, version), ErrCodeItemReferenced)
}
