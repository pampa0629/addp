package service

import (
	"errors"

	"github.com/addp/asset/internal/models"
	"gorm.io/gorm"
)

type TypeService struct {
	db *gorm.DB
}

func NewTypeService(db *gorm.DB) *TypeService {
	return &TypeService{db: db}
}

// ListTypes 返回指定租户可见的资产类型（全局类型 + 租户自定义类型，含禁用）
func (s *TypeService) ListTypes(tenantID uint) ([]models.TypeDefinition, error) {
	var types []models.TypeDefinition
	err := s.db.Where("tenant_id = ? OR tenant_id = 0", tenantID).
		Order("sort_order ASC, id ASC").
		Find(&types).Error
	return types, err
}

// GetType 根据 ID 获取类型定义
func (s *TypeService) GetType(id int64) (*models.TypeDefinition, error) {
	var t models.TypeDefinition
	if err := s.db.First(&t, id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// SeedBuiltinTypes 初始化内置资产类型（幂等，重复执行安全）
// 若记录已存在则更新 name/discovery_path/enabled 字段
func (s *TypeService) SeedBuiltinTypes() error {
	// 迁移：将旧 code=model 重命名为 algo_model
	s.db.Model(&models.TypeDefinition{}).
		Where("code = ? AND tenant_id = 0", "model").
		Updates(map[string]interface{}{"code": "algo_model", "name": "算法模型"})
	builtinTypes := []models.TypeDefinition{
		{
			TenantID:      0,
			Name:          "数据集",
			Code:          "dataset",
			SourceModule:  "meta",
			AuthHandler:   "soft",
			EntryType:     "preview",
			DiscoveryPath: "/api/v1/meta/assets/discoverable",
			Description:   "数据库表、文件、空间数据等",
			Enabled:       true,
			SortOrder:     1,
		},
		{
			TenantID:      0,
			Name:          "数据服务",
			Code:          "service",
			SourceModule:  "service",
			AuthHandler:   "token",
			EntryType:     "token",
			DiscoveryPath: "/api/v1/service/assets/discoverable",
			Description:   "REST API 服务、OGC 服务",
			Enabled:       true,
			SortOrder:     2,
		},
		{
			TenantID:      0,
			Name:          "指标",
			Code:          "metric",
			SourceModule:  "standard",
			AuthHandler:   "soft",
			EntryType:     "preview",
			DiscoveryPath: "/api/v1/standard/assets/discoverable",
			Description:   "业务 KPI、统计指标定义",
			Enabled:       true,
			SortOrder:     3,
		},
		{
			TenantID:     0,
			Name:         "报表/看板",
			Code:         "report",
			SourceModule: "manual",
			AuthHandler:  "soft",
			EntryType:    "link",
			Description:  "BI 报表、分析看板（手动录入，暂不支持）",
			Enabled:      false,
			SortOrder:    4,
		},
		{
			TenantID:      0,
			Name:          "算法模型",
			Code:          "algo_model",
			SourceModule:  "develop",
			AuthHandler:   "soft",
			EntryType:     "link",
			DiscoveryPath: "/api/v1/develop/assets/discoverable",
			Description:   "工作流、Notebook、训练模型",
			Enabled:       true,
			SortOrder:     5,
		},
		{
			TenantID:     0,
			Name:         "应用",
			Code:         "application",
			SourceModule: "manual",
			AuthHandler:  "soft",
			EntryType:    "link",
			Description:  "外部系统/工具，注册接入（手动录入，暂不支持）",
			Enabled:      false,
			SortOrder:    6,
		},
	}

	for _, t := range builtinTypes {
		var existing models.TypeDefinition
		err := s.db.Where("code = ? AND tenant_id = 0", t.Code).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := s.db.Create(&t).Error; err != nil {
				return err
			}
		} else if err == nil {
			// 更新 name/discovery_path/enabled，确保与代码定义一致
			s.db.Model(&existing).Updates(map[string]interface{}{
				"name":           t.Name,
				"discovery_path": t.DiscoveryPath,
				"enabled":        t.Enabled,
			})
		}
	}
	return nil
}
