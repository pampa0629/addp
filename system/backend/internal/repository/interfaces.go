package repository

import "github.com/addp/system/internal/models"

// EngineRepositoryInterface 引擎仓库接口
type EngineRepositoryInterface interface {
	Create(engine *models.Engine) error
	GetByID(id uint) (*models.Engine, error)
	List(offset, limit int, engineType string) ([]models.Engine, int64, error)
	ListByTenant(tenantID uint, offset, limit int, engineType string) ([]models.Engine, int64, error)
	Update(engine *models.Engine) error
	Delete(id uint) error
	CheckDuplicate(name string, engineType string, tenantID uint, excludeID uint) (bool, error)
}

// ApplicationRepositoryInterface 应用仓库接口
type ApplicationRepositoryInterface interface {
	Create(app *models.Application) error
	GetByID(id uint) (*models.Application, error)
	List(offset, limit int) ([]models.Application, int64, error)
	ListByTenant(tenantID uint, offset, limit int) ([]models.Application, int64, error)
	Update(app *models.Application) error
	Delete(id uint) error
}
