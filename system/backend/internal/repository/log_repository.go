package repository

import (
	"strings"
	"time"

	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

type LogRepository struct {
	db *gorm.DB
}

func NewLogRepository(db *gorm.DB) *LogRepository {
	return &LogRepository{db: db}
}

func (r *LogRepository) Create(log *models.AuditLog) error {
	return r.db.Create(log).Error
}

// List 查询日志列表（支持高级过滤）
func (r *LogRepository) List(offset, limit int, filters *models.AuditLogFilters) ([]models.AuditLog, int64, error) {
	var logs []models.AuditLog
	var total int64

	query := r.db.Model(&models.AuditLog{})

	// 应用过滤条件
	query = r.applyFilters(query, filters)

	// 统计总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 查询数据
	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&logs).Error
	return logs, total, err
}

// ListByTenant 查询指定租户的日志列表（支持高级过滤）
func (r *LogRepository) ListByTenant(tenantID uint, offset, limit int, filters *models.AuditLogFilters) ([]models.AuditLog, int64, error) {
	var logs []models.AuditLog
	var total int64

	query := r.db.Model(&models.AuditLog{}).Where("tenant_id = ?", tenantID)

	// 应用过滤条件
	query = r.applyFilters(query, filters)

	// 统计总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 查询数据
	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&logs).Error
	return logs, total, err
}

// applyFilters 应用过滤条件
func (r *LogRepository) applyFilters(query *gorm.DB, filters *models.AuditLogFilters) *gorm.DB {
	if filters == nil {
		return query
	}

	// 用户ID过滤
	if filters.UserID != nil {
		query = query.Where("user_id = ?", *filters.UserID)
	}

	// 时间范围过滤
	if filters.StartTime != "" {
		query = query.Where("created_at >= ?", filters.StartTime)
	}
	if filters.EndTime != "" {
		query = query.Where("created_at <= ?", filters.EndTime)
	}

	// 操作类型过滤（支持模糊匹配）
	if filters.Action != "" {
		query = query.Where("action LIKE ?", filters.Action+"%")
	}

	// 资源类型过滤
	if filters.EntityType != "" {
		query = query.Where("entity_type = ?", filters.EntityType)
	}

	// 用户名过滤（模糊匹配）
	if filters.Username != "" {
		query = query.Where("username LIKE ?", "%"+filters.Username+"%")
	}

	// IP地址过滤（模糊匹配）
	if filters.IPAddress != "" {
		// 去除空格
		ipAddr := strings.TrimSpace(filters.IPAddress)
		query = query.Where("ip_address LIKE ?", ipAddr+"%")
	}

	// 模块名过滤
	if filters.ModuleName != "" {
		query = query.Where("module_name = ?", filters.ModuleName)
	}

	return query
}

func (r *LogRepository) GetByID(id uint) (*models.AuditLog, error) {
	var log models.AuditLog
	err := r.db.First(&log, id).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}

// DeleteOlderThan 删除指定日期之前的日志
func (r *LogRepository) DeleteOlderThan(date time.Time) error {
	return r.db.Where("created_at < ?", date).Delete(&models.AuditLog{}).Error
}