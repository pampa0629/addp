package repository

import (
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"

	"gorm.io/gorm"
)

// ScanRepository 数据访问层，负责 meta_node 和 meta_item 的 CRUD 操作
type ScanRepository struct {
	db *gorm.DB
}

// NewScanRepository 创建 Repository 实例
func NewScanRepository(db *gorm.DB) *ScanRepository {
	return &ScanRepository{db: db}
}

func EnsureEngineCatalogRootNode(repo *ScanRepository, tenantID uint, resource *commonModels.Engine, p plugin.EnginePlugin) (*models.MetaNode, error) {
	return EnsureEngineCatalogRootNodeWithNativeName(repo, tenantID, resource, p, "")
}

func EnsureEngineCatalogRootNodeWithNativeName(repo *ScanRepository, tenantID uint, resource *commonModels.Engine, p plugin.EnginePlugin, nativeName string) (*models.MetaNode, error) {
	rootTerm := scanflow.EngineCatalogRootTermForPlugin(p)
	fullName := ""
	attrs := models.JSONMap{
		"schema_version": 1,
		"catalog": map[string]interface{}{
			"root_term":           rootTerm,
			"display_name_source": "engine.name",
		},
	}
	if nativeName != "" && nativeName != resource.Name {
		attrs["catalog"].(map[string]interface{})["native_name"] = nativeName
	}
	return repo.UpsertNode(tenantID, resource.ID, nil, rootTerm, resource.Name, &fullName, attrs)
}
