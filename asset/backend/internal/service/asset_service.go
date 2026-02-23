package service

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/asset/internal/models"
	"github.com/addp/asset/internal/search"
	"gorm.io/gorm"
)

type AssetService struct {
	db             *gorm.DB
	moduleURLs     map[string]string // sourceModule -> base URL
	httpClient     *http.Client
	internalAPIKey string
	indexer        *search.Indexer
}

func NewAssetService(db *gorm.DB, moduleURLs map[string]string, internalAPIKey string, indexer *search.Indexer) *AssetService {
	return &AssetService{
		db:             db,
		moduleURLs:     moduleURLs,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
		internalAPIKey: internalAPIKey,
		indexer:        indexer,
	}
}

// AssetListParams 资产列表查询参数
type AssetListParams struct {
	Page      int
	PageSize  int
	Status    string
	TypeID    int64
	CatalogID *int64 // nil=不过滤；-1=只看未归目录
	Keyword   string
}

// AssetWithType 带类型信息的资产（列表展示用）
type AssetWithType struct {
	models.Asset
	TypeName    string `json:"type_name"`
	TypeCode    string `json:"type_code"`
	CatalogName string `json:"catalog_name,omitempty"`
}

// AssetDetail 资产详情（含扩展字段和目录信息）
type AssetDetail struct {
	models.Asset
	TypeName  string                 `json:"type_name"`
	TypeCode  string                 `json:"type_code"`
	ExtFields []models.AssetExtField `json:"ext_fields"`
	Catalog   *models.Catalog        `json:"catalog,omitempty"`
	TypeDef   *models.TypeDefinition `json:"type_def,omitempty"`
}

// UpdateAssetReq 更新资产请求（编目用：名称/描述/目录/标签）
type UpdateAssetReq struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	CatalogID   *int64   `json:"catalog_id"`
	Tags        []string `json:"tags"`
}

// BatchIDsReq 批量操作请求
type BatchIDsReq struct {
	IDs []int64 `json:"ids" binding:"required,min=1"`
}

// BatchCatalogReq 批量归目录请求
type BatchCatalogReq struct {
	IDs       []int64 `json:"ids" binding:"required,min=1"`
	CatalogID *int64  `json:"catalog_id"` // null 表示清除目录
}

// SyncResult 同步结果
type SyncResult struct {
	Created int `json:"created"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

// List 查询资产列表（分页 + 过滤）
func (s *AssetService) List(tenantID uint, params *AssetListParams) ([]AssetWithType, int64, error) {
	query := s.db.Table("asset.assets a").
		Select("a.*, t.name as type_name, t.code as type_code, c.name as catalog_name").
		Joins("LEFT JOIN asset.type_definitions t ON t.id = a.type_id").
		Joins("LEFT JOIN asset.catalogs c ON c.id = a.catalog_id").
		Where("a.tenant_id = ?", tenantID)

	if params.Status != "" {
		query = query.Where("a.status = ?", params.Status)
	}
	if params.TypeID > 0 {
		query = query.Where("a.type_id = ?", params.TypeID)
	}
	if params.CatalogID != nil {
		if *params.CatalogID == -1 {
			query = query.Where("a.catalog_id IS NULL")
		} else {
			query = query.Where("a.catalog_id = ?", *params.CatalogID)
		}
	}
	if params.Keyword != "" {
		query = query.Where("a.name ILIKE ? OR a.description ILIKE ?",
			"%"+params.Keyword+"%", "%"+params.Keyword+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (params.Page - 1) * params.PageSize
	var assets []AssetWithType
	if err := query.Order("a.created_at DESC").
		Offset(offset).Limit(params.PageSize).
		Scan(&assets).Error; err != nil {
		return nil, 0, err
	}

	return assets, total, nil
}

// Get 获取资产详情（含扩展字段）
func (s *AssetService) Get(tenantID uint, id int64) (*AssetDetail, error) {
	var asset models.Asset
	if err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&asset).Error; err != nil {
		return nil, err
	}

	detail := &AssetDetail{Asset: asset}

	var extFields []models.AssetExtField
	s.db.Where("asset_id = ?", id).Find(&extFields)
	detail.ExtFields = extFields

	var typeDef models.TypeDefinition
	if err := s.db.First(&typeDef, asset.TypeID).Error; err == nil {
		detail.TypeDef = &typeDef
		detail.TypeName = typeDef.Name
		detail.TypeCode = typeDef.Code
	}

	if asset.CatalogID != nil {
		var cat models.Catalog
		if err := s.db.First(&cat, *asset.CatalogID).Error; err == nil {
			detail.Catalog = &cat
		}
	}

	return detail, nil
}

// Update 更新资产基本信息（名称/描述/分类/标签）
func (s *AssetService) Update(tenantID uint, id int64, userID uint, req *UpdateAssetReq) (*AssetDetail, error) {
	var asset models.Asset
	if err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&asset).Error; err != nil {
		return nil, err
	}

	updatedBy := int64(userID)
	asset.UpdatedBy = &updatedBy

	if req.Name != nil {
		asset.Name = *req.Name
	}
	if req.Description != nil {
		asset.Description = *req.Description
	}
	if req.CatalogID != nil {
		asset.CatalogID = req.CatalogID
	}
	if req.Tags != nil {
		asset.Tags = models.JSONBArray(req.Tags)
	}

	if err := s.db.Save(&asset).Error; err != nil {
		return nil, err
	}

	return s.Get(tenantID, id)
}

// Delete 删除资产（仅限草稿状态）
func (s *AssetService) Delete(tenantID uint, id int64) error {
	var asset models.Asset
	if err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&asset).Error; err != nil {
		return err
	}
	if asset.Status != "draft" {
		return fmt.Errorf("只有草稿状态的资产可以删除")
	}
	s.db.Where("asset_id = ?", id).Delete(&models.AssetExtField{})
	return s.db.Delete(&asset).Error
}

// Publish 上架（draft/offline → published）
func (s *AssetService) Publish(tenantID uint, id int64) error {
	var asset models.Asset
	if err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&asset).Error; err != nil {
		return err
	}
	if asset.Status != "draft" && asset.Status != "offline" {
		return fmt.Errorf("只有草稿或已下架的资产可以上架")
	}
	now := time.Now()
	asset.Status = "published"
	asset.PublishedAt = &now
	return s.db.Save(&asset).Error
}

// Offline 下架（published → offline）
func (s *AssetService) Offline(tenantID uint, id int64) error {
	var asset models.Asset
	if err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&asset).Error; err != nil {
		return err
	}
	if asset.Status != "published" {
		return fmt.Errorf("只有已上架的资产可以下架")
	}
	asset.Status = "offline"
	return s.db.Save(&asset).Error
}

// BatchPublish 批量上架
func (s *AssetService) BatchPublish(tenantID uint, ids []int64) (int, error) {
	now := time.Now()
	result := s.db.Model(&models.Asset{}).
		Where("id IN ? AND tenant_id = ? AND status IN ('draft','offline')", ids, tenantID).
		Updates(map[string]interface{}{
			"status":       "published",
			"published_at": now,
		})
	return int(result.RowsAffected), result.Error
}

// BatchOffline 批量下架
func (s *AssetService) BatchOffline(tenantID uint, ids []int64) (int, error) {
	result := s.db.Model(&models.Asset{}).
		Where("id IN ? AND tenant_id = ? AND status = 'published'", ids, tenantID).
		Update("status", "offline")
	return int(result.RowsAffected), result.Error
}

// BatchCatalog 批量归目录（catalogID 为 nil 时清除目录）
func (s *AssetService) BatchCatalog(tenantID uint, ids []int64, catalogID *int64) (int, error) {
	result := s.db.Model(&models.Asset{}).
		Where("id IN ? AND tenant_id = ?", ids, tenantID).
		Update("catalog_id", catalogID)
	return int(result.RowsAffected), result.Error
}

// Sync 从各源模块发现新资产，自动创建草稿
// 各模块独立调用，单个模块失败不影响其他模块
func (s *AssetService) Sync(tenantID uint) (*SyncResult, error) {
	var typeDefs []models.TypeDefinition
	if err := s.db.Where("(tenant_id = 0 OR tenant_id = ?) AND enabled = true AND discovery_path != ''", tenantID).
		Find(&typeDefs).Error; err != nil {
		return nil, err
	}

	result := &SyncResult{}

	for _, td := range typeDefs {
		baseURL, ok := s.moduleURLs[td.SourceModule]
		if !ok || baseURL == "" {
			continue
		}

		items, err := s.fetchDiscoverableAssets(baseURL, td.DiscoveryPath, tenantID)
		if err != nil {
			// 模块不可达或出错：记录日志，跳过，不影响其他模块
			log.Printf("⚠️  资产发现：%s 模块不可用，跳过 (%s%s): %v",
				td.SourceModule, baseURL, td.DiscoveryPath, err)
			result.Errors++
			continue
		}

		for _, item := range items {
			fingerprint := commonModels.GenerateAssetFingerprint(
				int64(tenantID), td.SourceModule, item.SourceReference,
			)

			var count int64
			s.db.Model(&models.Asset{}).
				Where("fingerprint = ? AND tenant_id = ?", fingerprint, tenantID).
				Count(&count)

			if count > 0 {
				// 已存在：标记来源仍可用
				s.db.Model(&models.Asset{}).
					Where("fingerprint = ? AND tenant_id = ?", fingerprint, tenantID).
					Update("source_available", true)
				result.Skipped++
				continue
			}

			// 新资产：自动创建草稿
			asset := &models.Asset{
				TenantID:        int64(tenantID),
				Name:            item.Name,
				Description:     item.Description,
				TypeID:          td.ID,
				Status:          "draft",
				OwnerID:         int64(tenantID),
				SourceModule:    td.SourceModule,
				SourceReference: item.SourceReference,
				Fingerprint:     fingerprint,
				SourceAvailable: true,
				CreatedBy:       0, // 系统自动创建
			}
			if err := s.db.Create(asset).Error; err != nil {
				log.Printf("⚠️  创建草稿资产失败 (%s/%s): %v", td.SourceModule, item.SourceReference, err)
				result.Errors++
			} else {
				result.Created++
			}
		}
	}

	return result, nil
}

// fetchDiscoverableAssets 调用源模块的 discoverable 接口
// 响应格式兼容：{"data": [...]} 或直接 [...]
func (s *AssetService) fetchDiscoverableAssets(baseURL, discoveryPath string, tenantID uint) ([]commonClient.DiscoverableAsset, error) {
	url := fmt.Sprintf("%s%s", baseURL, discoveryPath)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	// 携带内部认证信息（跳过各模块的 JWT 认证）
	if s.internalAPIKey != "" {
		req.Header.Set("X-Internal-API-Key", s.internalAPIKey)
	}
	req.Header.Set("X-Tenant-ID", strconv.Itoa(int(tenantID)))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var items []commonClient.DiscoverableAsset
	var wrapper struct {
		Data []commonClient.DiscoverableAsset `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Data != nil {
		return wrapper.Data, nil
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return items, nil
}

// GetTypeFieldSchemas 获取指定类型的扩展字段定义
func (s *AssetService) GetTypeFieldSchemas(typeID int64) ([]models.TypeFieldSchema, error) {
	var schemas []models.TypeFieldSchema
	err := s.db.Where("type_id = ?", typeID).Order("sort_order ASC").Find(&schemas).Error
	return schemas, err
}
