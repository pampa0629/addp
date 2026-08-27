package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/addp/asset/internal/models"
	"github.com/addp/asset/internal/search"
	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AssetService struct {
	db      *gorm.DB
	catalog *commonClient.CatalogClient
	indexer *search.Indexer
}

func NewAssetService(db *gorm.DB, catalog *commonClient.CatalogClient, indexer *search.Indexer) *AssetService {
	return &AssetService{db: db, catalog: catalog, indexer: indexer}
}

var (
	ErrCatalogUnavailable             = errors.New("enterprise Catalog is unavailable")
	ErrCatalogReferenceNotSelectable  = errors.New("CatalogEntry is not selectable")
	ErrCatalogReferenceNotPublishable = errors.New("CatalogEntry is not publishable")
	ErrInvalidAssetAggregate          = errors.New("invalid Asset aggregate")
	ErrAssetNotEditable               = errors.New("Asset is not editable in its current status")
	ErrAssetVersionConflict           = errors.New("Asset version conflict")
)

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
	TypeName    string                  `json:"type_name"`
	TypeCode    string                  `json:"type_code"`
	CatalogName string                  `json:"catalog_name,omitempty"`
	ExtFields   []models.AssetExtField  `json:"ext_fields"`
	Catalog     *models.Catalog         `json:"catalog,omitempty"`
	TypeDef     *models.TypeDefinition  `json:"type_def,omitempty"`
	Components  []models.AssetComponent `json:"components"`
}

type AssetComponentInput struct {
	CatalogEntryID string `json:"catalog_entry_id"`
	Role           string `json:"role"`
	SortOrder      int    `json:"sort_order"`
}

type CreateAssetReq struct {
	Name        string                `json:"name" binding:"required"`
	Description string                `json:"description"`
	TypeID      int64                 `json:"type_id" binding:"required"`
	CatalogID   *int64                `json:"catalog_id"`
	Tags        []string              `json:"tags"`
	Components  []AssetComponentInput `json:"components" binding:"required,min=1"`
}

// UpdateAssetReq 原子替换资产的完整可编辑聚合。
type UpdateAssetReq struct {
	Version     int64                 `json:"version" binding:"required,min=1"`
	Name        string                `json:"name" binding:"required"`
	Description string                `json:"description"`
	TypeID      int64                 `json:"type_id" binding:"required"`
	CatalogID   *int64                `json:"catalog_id"`
	Tags        []string              `json:"tags"`
	Components  []AssetComponentInput `json:"components" binding:"required,min=1"`
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

// List 查询资产列表（分页 + 过滤）。关键词搜索只走 Asset 自己的搜索投影。
func (s *AssetService) List(tenantID uint, params *AssetListParams) ([]AssetWithType, int64, error) {
	if params.Keyword != "" {
		if !s.indexer.Enabled() {
			return nil, 0, errors.New("Asset search projection is unavailable")
		}
		var typeCode string
		if params.TypeID > 0 {
			var td models.TypeDefinition
			if err := s.db.First(&td, params.TypeID).Error; err == nil {
				typeCode = td.Code
			}
		}
		offset := int64((params.Page - 1) * params.PageSize)
		msResult, err := s.indexer.Search(int64(tenantID), params.Keyword, typeCode, params.CatalogID, int64(params.PageSize), offset)
		if err != nil || msResult == nil {
			return nil, 0, fmt.Errorf("Asset search projection is unavailable: %w", err)
		}
		if len(msResult.IDs) == 0 {
			return []AssetWithType{}, msResult.Total, nil
		}
		var assets []AssetWithType
		if err := s.db.Table("asset.assets a").
			Select("a.*, t.name as type_name, t.code as type_code, c.name as catalog_name").
			Joins("LEFT JOIN asset.type_definitions t ON t.id = a.type_id").
			Joins("LEFT JOIN asset.catalogs c ON c.id = a.catalog_id").
			Where("a.tenant_id = ? AND a.id IN ?", tenantID, msResult.IDs).
			Scan(&assets).Error; err != nil {
			return nil, 0, err
		}
		order := make(map[int64]int, len(msResult.IDs))
		for i, id := range msResult.IDs {
			order[id] = i
		}
		sorted := make([]AssetWithType, 0, len(assets))
		for _, asset := range assets {
			if _, ok := order[asset.ID]; ok {
				sorted = append(sorted, asset)
			}
		}
		sort.Slice(sorted, func(i, j int) bool { return order[sorted[i].ID] < order[sorted[j].ID] })
		return sorted, msResult.Total, nil
	}

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
	if err := s.db.Where("tenant_id = ? AND asset_id = ?", tenantID, id).
		Order("sort_order ASC, id ASC").Find(&detail.Components).Error; err != nil {
		return nil, err
	}

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
			detail.CatalogName = cat.Name
		}
	}

	return detail, nil
}

// GetPublished 获取消费面可见的已上架资产详情。
func (s *AssetService) GetPublished(tenantID uint, id int64) (*AssetDetail, error) {
	var asset models.Asset
	if err := s.db.Where("id = ? AND tenant_id = ? AND status = 'published'", id, tenantID).First(&asset).Error; err != nil {
		return nil, err
	}

	detail := &AssetDetail{Asset: asset}
	var extFields []models.AssetExtField
	if err := s.db.Where("asset_id = ?", id).Find(&extFields).Error; err != nil {
		return nil, err
	}
	detail.ExtFields = extFields
	if err := s.db.Where("tenant_id = ? AND asset_id = ?", tenantID, id).
		Order("sort_order ASC, id ASC").Find(&detail.Components).Error; err != nil {
		return nil, err
	}

	var typeDef models.TypeDefinition
	if err := s.db.First(&typeDef, asset.TypeID).Error; err == nil {
		detail.TypeDef = &typeDef
		detail.TypeName = typeDef.Name
		detail.TypeCode = typeDef.Code
	}
	if asset.CatalogID != nil {
		var catalog models.Catalog
		if err := s.db.First(&catalog, *asset.CatalogID).Error; err == nil {
			detail.Catalog = &catalog
			detail.CatalogName = catalog.Name
		}
	}
	return detail, nil
}

func (s *AssetService) Create(ctx context.Context, tenantID uint, userID uint, req *CreateAssetReq) (*AssetDetail, error) {
	if err := s.validateOwnedReferences(tenantID, req.TypeID, req.CatalogID); err != nil {
		return nil, err
	}
	components, err := s.validateComponents(ctx, tenantID, req.TypeID, req.Components, false)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrInvalidAssetAggregate
	}
	asset := models.Asset{
		TenantID: int64(tenantID), Name: name, Description: strings.TrimSpace(req.Description),
		TypeID: req.TypeID, CatalogID: req.CatalogID, Tags: models.JSONBArray(req.Tags), Status: "draft",
		OwnerID: int64(userID), Version: 1, CreatedBy: int64(userID),
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&asset).Error; err != nil {
			return err
		}
		for index := range components {
			components[index].TenantID = int64(tenantID)
			components[index].AssetID = asset.ID
		}
		return tx.Create(&components).Error
	}); err != nil {
		return nil, err
	}
	return s.Get(tenantID, asset.ID)
}

// Update atomically replaces the complete editable Asset aggregate.
func (s *AssetService) Update(ctx context.Context, tenantID uint, id int64, userID uint, req *UpdateAssetReq) (*AssetDetail, error) {
	if err := s.validateOwnedReferences(tenantID, req.TypeID, req.CatalogID); err != nil {
		return nil, err
	}
	components, err := s.validateComponents(ctx, tenantID, req.TypeID, req.Components, false)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if req.Version <= 0 || name == "" {
		return nil, ErrInvalidAssetAggregate
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		updatedBy := int64(userID)
		result := tx.Model(&models.Asset{}).
			Where("id = ? AND tenant_id = ? AND version = ? AND status IN ('draft','offline')", id, tenantID, req.Version).
			Updates(map[string]any{
				"name": name, "description": strings.TrimSpace(req.Description), "type_id": req.TypeID,
				"catalog_id": req.CatalogID, "tags": models.JSONBArray(req.Tags), "updated_by": updatedBy,
				"version": gorm.Expr("version + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			var current models.Asset
			if err := tx.Select("id", "status", "version").Where("id = ? AND tenant_id = ?", id, tenantID).First(&current).Error; err != nil {
				return err
			}
			if current.Status != "draft" && current.Status != "offline" {
				return ErrAssetNotEditable
			}
			return ErrAssetVersionConflict
		}
		if err := tx.Where("tenant_id = ? AND asset_id = ?", tenantID, id).Delete(&models.AssetComponent{}).Error; err != nil {
			return err
		}
		for index := range components {
			components[index].TenantID = int64(tenantID)
			components[index].AssetID = id
		}
		return tx.Create(&components).Error
	})
	if err != nil {
		return nil, err
	}
	return s.Get(tenantID, id)
}

// Delete 删除草稿或已下架资产；已发布资产必须先下架。
func (s *AssetService) Delete(tenantID uint, id int64) error {
	var asset models.Asset
	if err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&asset).Error; err != nil {
		return err
	}
	if asset.Status != "draft" && asset.Status != "offline" {
		return fmt.Errorf("只有草稿或已下架状态的资产可以删除")
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("asset_id = ?", id).Delete(&models.AssetExtField{}).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ? AND asset_id = ?", tenantID, id).Delete(&models.AssetComponent{}).Error; err != nil {
			return err
		}
		return tx.Delete(&asset).Error
	}); err != nil {
		return err
	}
	go s.indexer.DeleteAsset(id)
	return nil
}

// Publish 上架（draft/offline → published）
func (s *AssetService) Publish(ctx context.Context, tenantID uint, id int64) error {
	var asset models.Asset
	if err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&asset).Error; err != nil {
		return err
	}
	if asset.Status != "draft" && asset.Status != "offline" {
		return fmt.Errorf("只有草稿或已下架的资产可以上架")
	}
	components, err := s.loadComponentInputs(tenantID, []int64{id})
	if err != nil {
		return err
	}
	if _, err := s.validateComponents(ctx, tenantID, asset.TypeID, components[id], true); err != nil {
		return err
	}
	now := time.Now()
	asset.Status = "published"
	asset.PublishedAt = &now
	if err := s.db.Save(&asset).Error; err != nil {
		return err
	}
	// 异步同步 Meilisearch 索引
	go s.indexer.UpsertAsset(s.toIndexDoc(&asset))
	return nil
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
	if err := s.db.Save(&asset).Error; err != nil {
		return err
	}
	// 异步更新索引状态（文档保留，仅改 status）
	go s.indexer.UpdateStatus(id, "offline")
	return nil
}

// BatchPublish 批量上架
func (s *AssetService) BatchPublish(ctx context.Context, tenantID uint, ids []int64) (int, error) {
	if !validUniqueAssetIDs(ids) {
		return 0, ErrInvalidAssetAggregate
	}
	var candidates []models.Asset
	if err := s.db.Where("id IN ? AND tenant_id = ? AND status IN ('draft','offline')", ids, tenantID).Find(&candidates).Error; err != nil {
		return 0, err
	}
	if len(candidates) != len(ids) {
		return 0, ErrInvalidAssetAggregate
	}
	components, err := s.loadComponentInputs(tenantID, ids)
	if err != nil {
		return 0, err
	}
	for _, asset := range candidates {
		assetComponents := components[asset.ID]
		if _, err := s.validateComponents(ctx, tenantID, asset.TypeID, assetComponents, true); err != nil {
			return 0, err
		}
	}
	now := time.Now()
	result := s.db.Model(&models.Asset{}).
		Where("id IN ? AND tenant_id = ? AND status IN ('draft','offline')", ids, tenantID).
		Updates(map[string]interface{}{
			"status":       "published",
			"published_at": now,
		})
	if result.Error != nil {
		return 0, result.Error
	}
	// 异步批量写入索引：查询实际更新的资产
	go func() {
		var assets []models.Asset
		if err := s.db.Where("id IN ? AND tenant_id = ?", ids, tenantID).Find(&assets).Error; err != nil {
			log.Printf("⚠️  批量上架索引同步：查询资产失败: %v", err)
			return
		}
		docs := make([]search.AssetIndexDoc, 0, len(assets))
		for i := range assets {
			if doc := s.toIndexDoc(&assets[i]); doc != nil {
				docs = append(docs, *doc)
			}
		}
		s.indexer.UpsertAssets(docs)
	}()
	return int(result.RowsAffected), nil
}

func validUniqueAssetIDs(ids []int64) bool {
	if len(ids) == 0 {
		return false
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return false
		}
		if _, exists := seen[id]; exists {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

// BatchOffline 批量下架
func (s *AssetService) BatchOffline(tenantID uint, ids []int64) (int, error) {
	result := s.db.Model(&models.Asset{}).
		Where("id IN ? AND tenant_id = ? AND status = 'published'", ids, tenantID).
		Update("status", "offline")
	if result.Error != nil {
		return 0, result.Error
	}
	// 异步批量更新索引状态
	go s.indexer.UpdateStatusBatch(ids, "offline")
	return int(result.RowsAffected), nil
}

// toIndexDoc 将 Asset 模型转换为 Meilisearch 索引文档
// 注意：会查询数据库获取类型名和目录名，适合在 goroutine 中调用
func (s *AssetService) toIndexDoc(asset *models.Asset) *search.AssetIndexDoc {
	doc := &search.AssetIndexDoc{
		ID:          asset.ID,
		TenantID:    asset.TenantID,
		Name:        asset.Name,
		Description: asset.Description,
		Tags:        []string(asset.Tags),
		Status:      asset.Status,
		PublishedAt: search.MeilisearchPublishedAt(asset.PublishedAt),
	}

	var td models.TypeDefinition
	if err := s.db.First(&td, asset.TypeID).Error; err == nil {
		doc.TypeCode = td.Code
		doc.TypeName = td.Name
	}

	if asset.CatalogID != nil {
		var cat models.Catalog
		if err := s.db.First(&cat, *asset.CatalogID).Error; err == nil {
			doc.CatalogID = asset.CatalogID
			doc.CatalogName = cat.Name
		}
	}

	return doc
}

// BatchCatalog 批量归目录（catalogID 为 nil 时清除目录）
func (s *AssetService) BatchCatalog(tenantID uint, ids []int64, catalogID *int64) (int, error) {
	result := s.db.Model(&models.Asset{}).
		Where("id IN ? AND tenant_id = ?", ids, tenantID).
		Update("catalog_id", catalogID)
	return int(result.RowsAffected), result.Error
}

func (s *AssetService) validateComponents(ctx context.Context, tenantID uint, typeID int64, inputs []AssetComponentInput, publish bool) ([]models.AssetComponent, error) {
	if err := validateComponentShape(inputs); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(inputs))
	components := make([]models.AssetComponent, 0, len(inputs))
	for _, input := range inputs {
		id, err := uuid.Parse(input.CatalogEntryID)
		if err != nil || id == uuid.Nil || id.String() != input.CatalogEntryID {
			return nil, ErrInvalidAssetAggregate
		}
		ids = append(ids, id)
		components = append(components, models.AssetComponent{CatalogEntryID: id, Role: input.Role, SortOrder: input.SortOrder})
	}
	resolutions, err := s.resolveCatalogReferences(ctx, tenantID, ids, publish)
	if err != nil {
		return nil, err
	}
	if err := s.validateTypeComposition(typeID, inputs, resolutions); err != nil {
		return nil, err
	}
	return components, nil
}

func (s *AssetService) resolveCatalogReferences(ctx context.Context, tenantID uint, ids []uuid.UUID, publish bool) ([]commonClient.CatalogReferenceResolution, error) {
	if s.catalog == nil {
		return nil, ErrCatalogUnavailable
	}
	resolutions, err := s.catalog.WithTenantID(tenantID).ResolveReferences(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCatalogUnavailable, err)
	}
	for _, resolution := range resolutions {
		if !resolution.Found || !resolution.Selectable {
			return nil, ErrCatalogReferenceNotSelectable
		}
		if publish && !resolution.Publishable {
			return nil, ErrCatalogReferenceNotPublishable
		}
	}
	return resolutions, nil
}

func (s *AssetService) validateTypeComposition(typeID int64, inputs []AssetComponentInput, resolutions []commonClient.CatalogReferenceResolution) error {
	var typeDefinition models.TypeDefinition
	if err := s.db.Select("code").First(&typeDefinition, typeID).Error; err != nil {
		return ErrInvalidAssetAggregate
	}
	if typeDefinition.Code != "application" {
		return nil
	}
	if len(inputs) != 1 || len(resolutions) != 1 || inputs[0].Role != models.AssetComponentRolePrimary {
		return ErrInvalidAssetAggregate
	}
	resolution := resolutions[0]
	resourceID, err := uuid.Parse(resolution.SourceIdentity)
	if err != nil || resourceID == uuid.Nil || resourceID.String() != resolution.SourceIdentity ||
		resolution.EntryType != "data_application" || resolution.SourceModule != "workbench" || resolution.SourceType != "data_application" {
		return ErrInvalidAssetAggregate
	}
	return nil
}

func validateComponentShape(inputs []AssetComponentInput) error {
	if len(inputs) == 0 || len(inputs) > 200 {
		return ErrInvalidAssetAggregate
	}
	seen := make(map[string]struct{}, len(inputs))
	primaryCount := 0
	for _, input := range inputs {
		if input.SortOrder < 0 || (input.Role != models.AssetComponentRolePrimary && input.Role != models.AssetComponentRoleSupporting) {
			return ErrInvalidAssetAggregate
		}
		if _, exists := seen[input.CatalogEntryID]; exists {
			return ErrInvalidAssetAggregate
		}
		seen[input.CatalogEntryID] = struct{}{}
		if input.Role == models.AssetComponentRolePrimary {
			primaryCount++
		}
	}
	if primaryCount != 1 {
		return ErrInvalidAssetAggregate
	}
	return nil
}

func (s *AssetService) validateOwnedReferences(tenantID uint, typeID int64, catalogID *int64) error {
	if typeID <= 0 {
		return ErrInvalidAssetAggregate
	}
	var typeCount int64
	if err := s.db.Model(&models.TypeDefinition{}).
		Where("id = ? AND enabled = ? AND (tenant_id = 0 OR tenant_id = ?)", typeID, true, tenantID).
		Count(&typeCount).Error; err != nil {
		return err
	}
	if typeCount != 1 {
		return ErrInvalidAssetAggregate
	}
	if catalogID != nil {
		var catalogCount int64
		if err := s.db.Model(&models.Catalog{}).Where("id = ? AND tenant_id = ?", *catalogID, tenantID).Count(&catalogCount).Error; err != nil {
			return err
		}
		if catalogCount != 1 {
			return ErrInvalidAssetAggregate
		}
	}
	return nil
}

func (s *AssetService) loadComponentInputs(tenantID uint, assetIDs []int64) (map[int64][]AssetComponentInput, error) {
	var components []models.AssetComponent
	if err := s.db.Where("tenant_id = ? AND asset_id IN ?", tenantID, assetIDs).
		Order("asset_id ASC, sort_order ASC, id ASC").Find(&components).Error; err != nil {
		return nil, err
	}
	result := make(map[int64][]AssetComponentInput, len(assetIDs))
	for _, component := range components {
		result[component.AssetID] = append(result[component.AssetID], AssetComponentInput{
			CatalogEntryID: component.CatalogEntryID.String(), Role: component.Role, SortOrder: component.SortOrder,
		})
	}
	return result, nil
}

func parseUniqueCatalogEntryIDs(inputs []AssetComponentInput) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if _, exists := seen[input.CatalogEntryID]; exists {
			continue
		}
		id, err := uuid.Parse(input.CatalogEntryID)
		if err != nil || id == uuid.Nil || id.String() != input.CatalogEntryID {
			return nil, ErrInvalidAssetAggregate
		}
		seen[input.CatalogEntryID] = struct{}{}
		result = append(result, id)
	}
	return result, nil
}

// GetTypeFieldSchemas 获取指定类型的扩展字段定义
func (s *AssetService) GetTypeFieldSchemas(typeID int64) ([]models.TypeFieldSchema, error) {
	var schemas []models.TypeFieldSchema
	err := s.db.Where("type_id = ?", typeID).Order("sort_order ASC").Find(&schemas).Error
	return schemas, err
}

// TypeStat 按资产类型统计的数量
type TypeStat struct {
	TypeID   int64  `json:"type_id"`
	TypeCode string `json:"type_code"`
	TypeName string `json:"type_name"`
	Count    int64  `json:"count"`
}

// StatsResult 资产统计结果
type StatsResult struct {
	TypeStats []TypeStat `json:"type_stats"`
	Total     int64      `json:"total"`
}

// GetStats 获取当前租户已上架资产的统计数据（各类型数量 + 总计）
func (s *AssetService) GetStats(tenantID uint) (*StatsResult, error) {
	type row struct {
		TypeID   int64  `gorm:"column:type_id"`
		TypeCode string `gorm:"column:type_code"`
		TypeName string `gorm:"column:type_name"`
		Count    int64  `gorm:"column:count"`
	}
	var rows []row
	if err := s.db.Table("asset.assets a").
		Select("t.id as type_id, t.code as type_code, t.name as type_name, COUNT(*) as count").
		Joins("JOIN asset.type_definitions t ON t.id = a.type_id").
		Where("a.tenant_id = ? AND a.status = 'published'", tenantID).
		Group("t.id, t.code, t.name").
		Order("count DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	result := &StatsResult{TypeStats: make([]TypeStat, 0, len(rows))}
	for _, r := range rows {
		result.TypeStats = append(result.TypeStats, TypeStat{
			TypeID:   r.TypeID,
			TypeCode: r.TypeCode,
			TypeName: r.TypeName,
			Count:    r.Count,
		})
		result.Total += r.Count
	}
	return result, nil
}

// DashboardStats 运营看板统计数据
type DashboardStats struct {
	// 资产状态汇总
	AssetTotal     int64 `json:"asset_total"`
	AssetDraft     int64 `json:"asset_draft"`
	AssetPublished int64 `json:"asset_published"`
	AssetOffline   int64 `json:"asset_offline"`
	// 申请汇总
	ApplicationTotal    int64 `json:"application_total"`
	ApplicationPending  int64 `json:"application_pending"`
	AuthorizationActive int64 `json:"authorization_active"`
	// 时间趋势（近 30 天，按天汇总）
	PublishTrend     []DailyCount `json:"publish_trend"`
	ApplicationTrend []DailyCount `json:"application_trend"`
	// 评价汇总
	RatingCount    int64   `json:"rating_count"`
	RatingAvgScore float64 `json:"rating_avg_score"`
}

// DailyCount 按天汇总的数量
type DailyCount struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// GetDashboardStats 获取运营看板统计数据
func (s *AssetService) GetDashboardStats(tenantID uint) (*DashboardStats, error) {
	stats := &DashboardStats{}

	// 1. 资产状态汇总
	type statusRow struct {
		Status string `gorm:"column:status"`
		Count  int64  `gorm:"column:count"`
	}
	var statusRows []statusRow
	s.db.Table("asset.assets").
		Select("status, COUNT(*) as count").
		Where("tenant_id = ?", tenantID).
		Group("status").
		Scan(&statusRows)
	for _, r := range statusRows {
		stats.AssetTotal += r.Count
		switch r.Status {
		case "draft":
			stats.AssetDraft = r.Count
		case "published":
			stats.AssetPublished = r.Count
		case "offline":
			stats.AssetOffline = r.Count
		}
	}

	// 2. 申请汇总
	s.db.Table("asset.applications").
		Where("tenant_id = ?", tenantID).
		Count(&stats.ApplicationTotal)
	s.db.Table("asset.applications").
		Where("tenant_id = ? AND status = 'pending'", tenantID).
		Count(&stats.ApplicationPending)
	s.db.Table("asset.authorizations").
		Where("tenant_id = ? AND status = ?", tenantID, models.AuthorizationStatusEffective).
		Count(&stats.AuthorizationActive)

	// 3. 近 30 天上架趋势
	publishTrend := s.dailyTrend(
		"asset.assets",
		"published_at",
		"tenant_id = ? AND status = 'published' AND published_at IS NOT NULL",
		tenantID,
	)
	stats.PublishTrend = publishTrend

	// 4. 近 30 天申请趋势
	appTrend := s.dailyTrend(
		"asset.applications",
		"created_at",
		"tenant_id = ?",
		tenantID,
	)
	stats.ApplicationTrend = appTrend

	// 5. 评价汇总
	type ratingRow struct {
		Count    int64   `gorm:"column:count"`
		AvgScore float64 `gorm:"column:avg_score"`
	}
	var rr ratingRow
	s.db.Table("asset.ratings").
		Select("COUNT(*) as count, COALESCE(AVG(score), 0) as avg_score").
		Where("tenant_id = ?", tenantID).
		Scan(&rr)
	stats.RatingCount = rr.Count
	stats.RatingAvgScore = rr.AvgScore

	return stats, nil
}

// dailyTrend 获取指定表近 30 天每天的数据量
func (s *AssetService) dailyTrend(table, dateCol, condition string, args ...interface{}) []DailyCount {
	type row struct {
		Date  string `gorm:"column:date"`
		Count int64  `gorm:"column:count"`
	}
	var rows []row
	s.db.Table(table).
		Select(fmt.Sprintf("TO_CHAR(DATE_TRUNC('day', %s), 'YYYY-MM-DD') AS date, COUNT(*) AS count", dateCol)).
		Where(condition, args...).
		Where(fmt.Sprintf("%s >= NOW() - INTERVAL '30 days'", dateCol)).
		Group("date").
		Order("date ASC").
		Scan(&rows)

	counts := make([]DailyCount, 0, len(rows))
	for _, r := range rows {
		counts = append(counts, DailyCount{Date: r.Date, Count: r.Count})
	}
	return counts
}
