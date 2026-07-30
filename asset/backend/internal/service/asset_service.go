package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/addp/asset/internal/models"
	"github.com/addp/asset/internal/search"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AssetService struct {
	db            *gorm.DB
	moduleURLs    map[string]string // sourceModule -> base URL
	httpClient    *http.Client
	serviceTokens commonClient.ServiceTokenProvider
	indexer       *search.Indexer
}

func NewAssetService(db *gorm.DB, moduleURLs map[string]string, serviceTokens commonClient.ServiceTokenProvider, indexer *search.Indexer) *AssetService {
	return &AssetService{
		db:            db,
		moduleURLs:    moduleURLs,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		serviceTokens: serviceTokens,
		indexer:       indexer,
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
	TypeName    string                 `json:"type_name"`
	TypeCode    string                 `json:"type_code"`
	CatalogName string                 `json:"catalog_name,omitempty"`
	ExtFields   []models.AssetExtField `json:"ext_fields"`
	Catalog     *models.Catalog        `json:"catalog,omitempty"`
	TypeDef     *models.TypeDefinition `json:"type_def,omitempty"`
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
// 当 Keyword 非空且 Meilisearch 可用时，优先走全文搜索
func (s *AssetService) List(tenantID uint, params *AssetListParams) ([]AssetWithType, int64, error) {
	// 当有关键词时，先走 Meilisearch 获取匹配 ID 列表
	if params.Keyword != "" && s.indexer.Enabled() {
		var typeCode string
		if params.TypeID > 0 {
			var td models.TypeDefinition
			if err := s.db.First(&td, params.TypeID).Error; err == nil {
				typeCode = td.Code
			}
		}
		offset := int64((params.Page - 1) * params.PageSize)
		msResult, err := s.indexer.Search(int64(tenantID), params.Keyword, typeCode, params.CatalogID, int64(params.PageSize), offset)
		if err == nil && msResult != nil {
			if len(msResult.IDs) == 0 {
				return []AssetWithType{}, msResult.Total, nil
			}
			var assets []AssetWithType
			if err := s.db.Table("asset.assets a").
				Select("a.*, t.name as type_name, t.code as type_code, c.name as catalog_name").
				Joins("LEFT JOIN asset.type_definitions t ON t.id = a.type_id").
				Joins("LEFT JOIN asset.catalogs c ON c.id = a.catalog_id").
				Where("a.id IN ?", msResult.IDs).
				Scan(&assets).Error; err != nil {
				return nil, 0, err
			}
			// 按 Meilisearch 返回的相关度顺序重排
			order := make(map[int64]int, len(msResult.IDs))
			for i, id := range msResult.IDs {
				order[id] = i
			}
			sorted := make([]AssetWithType, len(assets))
			for _, a := range assets {
				if i, ok := order[a.ID]; ok {
					sorted[i] = a
				}
			}
			return sorted, msResult.Total, nil
		}
		// Meilisearch 不可用时 fallback 到 ILIKE（不中断请求）
		log.Printf("⚠️  Meilisearch 搜索失败，fallback 到数据库搜索: %v", err)
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
func (s *AssetService) BatchPublish(tenantID uint, ids []int64) (int, error) {
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

// Sync 从各源模块发现新资产，自动创建草稿
// 各模块独立调用，单个模块失败不影响其他模块
func (s *AssetService) Sync(ctx context.Context, tenantID uint) (*SyncResult, error) {
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

		items, err := s.fetchDiscoverableAssets(ctx, baseURL, td.DiscoveryPath, tenantID)
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
			insert := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(asset)
			if insert.Error != nil {
				log.Printf("⚠️  创建草稿资产失败 (%s/%s): %v", td.SourceModule, item.SourceReference, insert.Error)
				result.Errors++
				continue
			}
			if insert.RowsAffected == 0 {
				if err := s.db.Model(&models.Asset{}).
					Where("fingerprint = ? AND tenant_id = ?", fingerprint, tenantID).
					Update("source_available", true).Error; err != nil {
					log.Printf("⚠️  更新资产来源状态失败 (%s/%s): %v", td.SourceModule, item.SourceReference, err)
					result.Errors++
					continue
				}
				result.Skipped++
				continue
			}
			result.Created++
		}
	}

	return result, nil
}

// fetchDiscoverableAssets 调用源模块的 discoverable 接口
func (s *AssetService) fetchDiscoverableAssets(ctx context.Context, baseURL, discoveryPath string, tenantID uint) ([]commonClient.DiscoverableAsset, error) {
	url := fmt.Sprintf("%s%s", baseURL, discoveryPath)

	token, err := s.serviceTokens.Token(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("获取 Asset Service Access Token 失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

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
		Where("tenant_id = ? AND is_active = true", tenantID).
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
