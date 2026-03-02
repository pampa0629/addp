package service

import (
	"errors"
	"fmt"

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
		return nil, fmt.Errorf("码值集编码 '%s' 已存在", req.Code)
	}

	// 校验 type
	if req.Type == "" {
		req.Type = "custom"
	}
	if req.Type != "system" && req.Type != "custom" {
		return nil, errors.New("type 必须是 system 或 custom")
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
	codeSet, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	if codeSet.TenantID != tenantID {
		return nil, errors.New("无权访问该码值集")
	}

	return codeSet, nil
}

// ListCodeSets 获取码值集列表
func (s *CodeSetService) ListCodeSets(tenantID int64, keyword, codeSetType string, page, pageSize int) ([]models.CodeSet, int64, error) {
	return s.repo.List(tenantID, keyword, codeSetType, page, pageSize)
}

// UpdateCodeSet 更新码值集
func (s *CodeSetService) UpdateCodeSet(id, tenantID int64, req *models.UpdateCodeSetRequest) error {
	codeSet, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}

	if codeSet.TenantID != tenantID {
		return errors.New("无权访问该码值集")
	}

	// 校验 type
	if req.Type != "" && req.Type != "system" && req.Type != "custom" {
		return errors.New("type 必须是 system 或 custom")
	}

	codeSet.Name = req.Name
	if req.Type != "" {
		codeSet.Type = req.Type
	}
	codeSet.Description = req.Description

	return s.repo.Update(codeSet)
}

// DeleteCodeSet 删除码值集
func (s *CodeSetService) DeleteCodeSet(id, tenantID int64) error {
	codeSet, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}

	if codeSet.TenantID != tenantID {
		return errors.New("无权访问该码值集")
	}

	// 系统内置码值集禁止删除
	if codeSet.Type == "system" {
		return errors.New("系统内置码值集不允许删除")
	}

	return s.repo.Delete(id)
}

// GetCodeItems 获取码值项列表
func (s *CodeSetService) GetCodeItems(codeSetID, tenantID int64) ([]models.CodeItem, error) {
	// 验证码值集是否属于当前租户
	codeSet, err := s.repo.GetByID(codeSetID)
	if err != nil {
		return nil, err
	}
	if codeSet.TenantID != tenantID {
		return nil, errors.New("无权访问该码值集")
	}

	return s.repo.GetItems(codeSetID)
}

// CreateCodeItem 创建码值项
func (s *CodeSetService) CreateCodeItem(codeSetID, tenantID int64, req *models.CreateCodeItemRequest) (*models.CodeItem, error) {
	// 验证码值集是否属于当前租户
	codeSet, err := s.repo.GetByID(codeSetID)
	if err != nil {
		return nil, err
	}
	if codeSet.TenantID != tenantID {
		return nil, errors.New("无权访问该码值集")
	}

	// 校验 code 唯一性
	exists, err := s.repo.ExistsItemByCode(codeSetID, req.Code, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("码值项编码 '%s' 已存在", req.Code)
	}

	item := &models.CodeItem{
		CodeSetID:   codeSetID,
		Code:        req.Code,
		Value:       req.Value,
		Description: req.Description,
		SortOrder:   req.SortOrder,
		IsActive:    req.IsActive,
	}

	if err := s.repo.CreateItem(item); err != nil {
		return nil, err
	}

	return item, nil
}

// UpdateCodeItem 更新码值项
func (s *CodeSetService) UpdateCodeItem(codeSetID, itemID, tenantID int64, req *models.UpdateCodeItemRequest) error {
	// 验证码值集是否属于当前租户
	codeSet, err := s.repo.GetByID(codeSetID)
	if err != nil {
		return err
	}
	if codeSet.TenantID != tenantID {
		return errors.New("无权访问该码值集")
	}

	item, err := s.repo.GetItemByID(itemID)
	if err != nil {
		return err
	}

	if item.CodeSetID != codeSetID {
		return errors.New("码值项不属于该码值集")
	}

	item.Value = req.Value
	item.Description = req.Description
	item.SortOrder = req.SortOrder
	item.IsActive = req.IsActive

	return s.repo.UpdateItem(item)
}

// DeleteCodeItem 删除码值项
func (s *CodeSetService) DeleteCodeItem(codeSetID, itemID, tenantID int64) error {
	// 验证码值集是否属于当前租户
	codeSet, err := s.repo.GetByID(codeSetID)
	if err != nil {
		return err
	}
	if codeSet.TenantID != tenantID {
		return errors.New("无权访问该码值集")
	}

	item, err := s.repo.GetItemByID(itemID)
	if err != nil {
		return err
	}

	if item.CodeSetID != codeSetID {
		return errors.New("码值项不属于该码值集")
	}

	return s.repo.DeleteItem(itemID)
}
