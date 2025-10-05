package service

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanner"
	"gorm.io/gorm"
)

// SyncService Level 1 轻量级同步服务
type SyncService struct {
	db           *gorm.DB
	systemClient *client.SystemClient
}

func NewSyncService(db *gorm.DB, systemClient *client.SystemClient) *SyncService {
	return &SyncService{
		db:           db,
		systemClient: systemClient,
	}
}

// GetDB 获取数据库连接
func (s *SyncService) GetDB() *gorm.DB {
	return s.db
}

// AutoSyncAll 自动同步所有数据源 (Level 1)
func (s *SyncService) AutoSyncAll(tenantID uint) error {
	log.Printf("AutoSyncAll called for tenant %d", tenantID)

	// 获取所有数据库类型的资源
	resources, err := s.systemClient.ListResources("")
	if err != nil {
		log.Printf("Failed to list resources from System: %v", err)
		return fmt.Errorf("failed to list resources: %w", err)
	}

	log.Printf("Found %d resources from System", len(resources))

	for _, resource := range resources {
		// 过滤租户资源
		if resource.TenantID != tenantID && tenantID != 0 {
			log.Printf("Skipping resource %d (tenant %d != %d)", resource.ID, resource.TenantID, tenantID)
			continue
		}

		// 只处理数据库类型 (不区分大小写)
		resourceType := strings.ToLower(resource.ResourceType)
		if resourceType != "postgresql" && resourceType != "mysql" {
			log.Printf("Skipping resource %d (type %s not postgresql/mysql)", resource.ID, resource.ResourceType)
			continue
		}

		log.Printf("Starting async sync for resource %d (type: %s, tenant: %d)", resource.ID, resource.ResourceType, resource.TenantID)

		// 异步同步 - 直接传递resource对象,不依赖systemClient
		go func(r commonModels.Resource) {
			log.Printf("Goroutine started for resource %d", r.ID)
			if err := s.syncResourceInternal(&r, r.TenantID); err != nil {
				log.Printf("Failed to sync resource %d: %v", r.ID, err)
			} else {
				log.Printf("Successfully synced resource %d", r.ID)
			}
		}(resource)
	}

	return nil
}

// syncResourceInternal 内部同步方法,接收已经获取的resource对象
func (s *SyncService) syncResourceInternal(resource *commonModels.Resource, tenantID uint) error {
	log.Printf("syncResourceInternal called for resource %d (%s)", resource.ID, resource.ResourceName)

	// 创建或更新数据源记录
	datasource, err := s.getOrCreateDatasource(resource.ID, tenantID)
	if err != nil {
		log.Printf("Failed to create/get datasource for resource %d: %v", resource.ID, err)
		return err
	}

	log.Printf("Datasource created/found: id=%d, name=%s", datasource.ID, datasource.DatasourceName)

	// 创建同步日志
	syncLog := &models.MetadataSyncLog{
		DatasourceID: datasource.ID,
		TenantID:     tenantID,
		SyncType:     "auto",
		SyncLevel:    "database",
		Status:       "running",
		StartedAt:    ptrTime(time.Now()),
	}
	if err := s.db.Create(syncLog).Error; err != nil {
		log.Printf("Failed to create sync log: %v", err)
		return fmt.Errorf("failed to create sync log: %w", err)
	}

	log.Printf("Sync log created: id=%d", syncLog.ID)

	// 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		s.updateSyncLogFailed(syncLog, err.Error())
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	// 创建扫描器
	scan, err := scanner.NewScanner(resource.ResourceType, connStr)
	if err != nil {
		s.updateSyncLogFailed(syncLog, err.Error())
		return fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	// 扫描数据库列表
	databases, err := scan.ScanDatabases()
	if err != nil {
		s.updateSyncLogFailed(syncLog, err.Error())
		return fmt.Errorf("failed to scan databases: %w", err)
	}

	// 保存数据库元数据
	for _, db := range databases {
		metaDB := &models.MetadataDatabase{
			DatasourceID:   datasource.ID,
			TenantID:       tenantID,
			DatabaseName:   db.Name,
			Charset:        db.Charset,
			Collation:      db.Collation,
			TableCount:     db.TableCount,
			TotalSizeBytes: db.TotalSizeBytes,
			IsScanned:      false,
		}

		// 检查是否已存在
		var existing models.MetadataDatabase
		result := s.db.Where("datasource_id = ? AND database_name = ?", datasource.ID, db.Name).First(&existing)

		if result.Error == gorm.ErrRecordNotFound {
			// 新建
			if err := s.db.Create(metaDB).Error; err != nil {
				log.Printf("Failed to create database metadata: %v", err)
			}
		} else {
			// 更新
			if err := s.db.Model(&existing).Updates(metaDB).Error; err != nil {
				log.Printf("Failed to update database metadata: %v", err)
			}
		}
	}

	// 更新数据源状态
	datasource.SyncStatus = "success"
	datasource.LastSyncAt = ptrTime(time.Now())
	datasource.SyncLevel = "database"
	s.db.Save(datasource)

	// 更新同步日志
	now := time.Now()
	syncLog.Status = "success"
	syncLog.CompletedAt = &now
	syncLog.DurationSeconds = int(now.Sub(*syncLog.StartedAt).Seconds())
	syncLog.DatabasesScanned = len(databases)
	s.db.Save(syncLog)

	log.Printf("Successfully synced resource %d, found %d databases", resource.ID, len(databases))
	return nil
}

// SyncResource 同步单个资源的数据库列表 (Level 1) - 用于API调用
func (s *SyncService) SyncResource(resourceID, tenantID uint) error {
	// 获取资源信息
	resource, err := s.systemClient.GetResource(resourceID)
	if err != nil {
		return fmt.Errorf("failed to get resource: %w", err)
	}

	return s.syncResourceInternal(resource, tenantID)
}

// getOrCreateDatasource 获取或创建数据源记录
func (s *SyncService) getOrCreateDatasource(resourceID, tenantID uint) (*models.MetadataDatasource, error) {
	// 获取资源信息
	resource, err := s.systemClient.GetResource(resourceID)
	if err != nil {
		return nil, err
	}

	// 查找已存在的数据源
	var datasource models.MetadataDatasource
	result := s.db.Where("resource_id = ? AND tenant_id = ?", resourceID, tenantID).First(&datasource)

	if result.Error == gorm.ErrRecordNotFound {
		// 创建新数据源
		datasource = models.MetadataDatasource{
			ResourceID:     resourceID,
			TenantID:       tenantID,
			DatasourceName: resource.ResourceName,
			DatasourceType: resource.ResourceType,
			SyncStatus:     "pending",
		}
		if err := s.db.Create(&datasource).Error; err != nil {
			return nil, fmt.Errorf("failed to create datasource: %w", err)
		}
	}

	return &datasource, nil
}

// updateSyncLogFailed 更新同步日志为失败状态
func (s *SyncService) updateSyncLogFailed(syncLog *models.MetadataSyncLog, errorMsg string) {
	now := time.Now()
	syncLog.Status = "failed"
	syncLog.CompletedAt = &now
	syncLog.DurationSeconds = int(now.Sub(*syncLog.StartedAt).Seconds())
	syncLog.ErrorMessage = errorMsg
	s.db.Save(syncLog)
}

// ptrTime 返回时间指针
func ptrTime(t time.Time) *time.Time {
	return &t
}
