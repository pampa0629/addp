package repository

import (
	"context"
	"errors"
	"time"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

var ErrEngineVersionConflict = errors.New("engine version conflict")

type EngineRepository struct {
	db *gorm.DB
}

func NewEngineRepository(db *gorm.DB) *EngineRepository {
	return &EngineRepository{db: db}
}

func (r *EngineRepository) Create(engine *models.Engine) error {
	return r.db.Create(engine).Error
}

func (r *EngineRepository) GetByID(id uint) (*models.Engine, error) {
	var engine models.Engine
	err := r.db.First(&engine, id).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &engine, nil
}

func (r *EngineRepository) List(offset, limit int, engineType string) ([]models.Engine, int64, error) {
	var engines []models.Engine
	var total int64

	query := r.db.Where("lifecycle_state = ?", models.EngineLifecycleActive)

	if engineType != "" {
		query = query.Where("engine_type = ?", engineType)
	}

	// 先获取总数
	if err := query.Model(&models.Engine{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 再获取分页数据
	err := query.Order("id ASC").Offset(offset).Limit(limit).Find(&engines).Error
	return engines, total, err
}

func (r *EngineRepository) ListAll() ([]models.Engine, error) {
	var engines []models.Engine
	err := r.db.Where("lifecycle_state <> ?", models.EngineLifecycleDeleted).Find(&engines).Error
	return engines, err
}

// ListByTenant 查询指定租户的资源列表（包含内置资源）
func (r *EngineRepository) ListByTenant(tenantID uint, offset, limit int, engineType string) ([]models.Engine, int64, error) {
	var engines []models.Engine
	var total int64

	// 查询条件：
	// 1. 租户自己创建的资源（tenant_id = tenantID）
	// 2. 内置资源（is_builtin = true AND tenant_id IS NULL）
	query := r.db.Where(
		"(tenant_id = ? OR (is_builtin = ? AND tenant_id IS NULL)) AND lifecycle_state = ?",
		tenantID, true, models.EngineLifecycleActive,
	)

	if engineType != "" {
		query = query.Where("engine_type = ?", engineType)
	}

	// 先获取总数
	if err := query.Model(&models.Engine{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 再获取分页数据
	err := query.Order("id ASC").Offset(offset).Limit(limit).Find(&engines).Error
	return engines, total, err
}

// ListVisibleByTenant 返回租户可见引擎，由服务层在能力过滤后统一分页。
func (r *EngineRepository) ListVisibleByTenant(tenantID uint, engineType string, lifecycleStates []string) ([]models.Engine, error) {
	var engines []models.Engine
	query := r.db.Where(
		"(tenant_id = ? OR (is_builtin = ? AND tenant_id IS NULL))",
		tenantID, true,
	)
	if len(lifecycleStates) == 0 {
		lifecycleStates = []string{models.EngineLifecycleActive}
	}
	query = query.Where("lifecycle_state IN ?", lifecycleStates)
	if engineType != "" {
		query = query.Where("engine_type = ?", engineType)
	}
	err := query.Order("id ASC").Find(&engines).Error
	return engines, err
}

func (r *EngineRepository) ListDeleting() ([]models.Engine, error) {
	var engines []models.Engine
	err := r.db.Where("lifecycle_state = ?", models.EngineLifecycleDeleting).Order("id ASC").Find(&engines).Error
	return engines, err
}

func (r *EngineRepository) Update(engine *models.Engine) error {
	if engine == nil || engine.ID == 0 || engine.Version < 1 {
		return ErrEngineVersionConflict
	}
	expectedVersion := engine.Version
	engine.Version = expectedVersion + 1
	engine.UpdatedAt = time.Now()
	result := r.db.Model(&models.Engine{}).
		Where("id = ? AND version = ?", engine.ID, expectedVersion).
		Select("*").
		Omit("id", "created_at").
		Updates(engine)
	if result.Error != nil {
		engine.Version = expectedVersion
		return result.Error
	}
	if result.RowsAffected != 1 {
		engine.Version = expectedVersion
		return ErrEngineVersionConflict
	}
	return nil
}

// FindByIdentityKey 返回同一永久身份的 Engine Instance，包含 deleted 墓碑。
func (r *EngineRepository) FindByIdentityKey(engineType string, tenantID *uint, identityKey models.JSONString) (*models.Engine, error) {
	var engine models.Engine
	query := r.db.Where("lower(engine_type) = lower(?) AND identity_key = ?", engineType, identityKey)
	if tenantID == nil {
		query = query.Where("tenant_id IS NULL")
	} else {
		query = query.Where("tenant_id = ?", *tenantID)
	}
	if err := query.First(&engine).Error; err != nil {
		return nil, err
	}
	return &engine, nil
}

// FindByFilters 根据过滤条件查询引擎列表
func (r *EngineRepository) FindByFilters(ctx context.Context, filters map[string]interface{}) ([]*models.Engine, error) {
	var engines []*models.Engine
	query := r.db.WithContext(ctx)

	for key, value := range filters {
		query = query.Where(key+" = ?", value)
	}

	err := query.Find(&engines).Error
	if err != nil {
		return nil, err
	}

	return engines, nil
}

// CreateWithContext 创建资源（带 context）
func (r *EngineRepository) CreateWithContext(ctx context.Context, engine *models.Engine) error {
	return r.db.WithContext(ctx).Create(engine).Error
}

// FindByNameAndTenant 根据名称和租户查找资源
func (r *EngineRepository) FindByNameAndTenant(name string, tenantID uint) (*models.Engine, error) {
	var engine models.Engine
	err := r.db.Where("name = ? AND tenant_id = ? AND lifecycle_state <> ?", name, tenantID, models.EngineLifecycleDeleted).First(&engine).Error
	if err != nil {
		return nil, err
	}
	return &engine, nil
}
