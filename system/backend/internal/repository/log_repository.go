package repository

import (
	"strings"
	"time"

	commonrepo "github.com/addp/common/repository"
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

	// HTTP 方法过滤
	if filters.HTTPMethod != "" {
		query = query.Where("http_method = ?", filters.HTTPMethod)
	}

	// 资源路径过滤（前缀匹配）
	if filters.ResourcePath != "" {
		resourcePath := strings.TrimSpace(filters.ResourcePath)
		if resourcePath != "" {
			query = query.Where("resource_path LIKE ?", resourcePath+"%")
		}
	}

	// 资源类型过滤
	if filters.EntityType != "" {
		query = query.Where("entity_type = ?", filters.EntityType)
	}

	// 资源ID过滤
	if filters.EntityID != "" {
		query = query.Where("entity_id = ?", filters.EntityID)
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

	// HTTP状态码范围过滤
	if filters.StatusCode != "" {
		switch filters.StatusCode {
		case "2xx":
			query = query.Where("http_status >= ? AND http_status < ?", 200, 300)
		case "4xx":
			query = query.Where("http_status >= ? AND http_status < ?", 400, 500)
		case "5xx":
			query = query.Where("http_status >= ?", 500)
		}
	}

	return query
}

func (r *LogRepository) GetByID(id uint) (*models.AuditLog, error) {
	var log models.AuditLog
	err := r.db.First(&log, id).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &log, nil
}

// DeleteOlderThan 删除指定日期之前的日志
func (r *LogRepository) DeleteOlderThan(date time.Time) error {
	return r.db.Where("created_at < ?", date).Delete(&models.AuditLog{}).Error
}

// ============================================
// 统计查询方法（新增）
// ============================================

// TrendDataPoint 趋势数据点
type TrendDataPoint struct {
	Timestamp string `json:"timestamp"` // 时间戳（日期或小时）
	Count     int64  `json:"count"`     // 日志数量
}

// UserStats 用户统计
type UserStats struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	Count    int64  `json:"count"`
}

// ModuleStats 模块统计
type ModuleStats struct {
	ModuleName string `json:"module_name"`
	Count      int64  `json:"count"`
}

// ActionStats 操作类型统计
type ActionStats struct {
	ActionType string `json:"action_type"` // POST/PUT/DELETE/PATCH
	Count      int64  `json:"count"`
}

// GetDailyTrends 获取每日趋势（最近N天）
func (r *LogRepository) GetDailyTrends(tenantID *uint, days int) ([]TrendDataPoint, error) {
	startTime := time.Now().AddDate(0, 0, -days)

	query := r.db.Model(&models.AuditLog{}).
		Select("DATE(created_at) as timestamp, COUNT(*) as count").
		Where("created_at >= ?", startTime).
		Group("DATE(created_at)").
		Order("timestamp ASC")

	if tenantID != nil {
		query = query.Where("tenant_id = ?", *tenantID)
	}

	var trends []TrendDataPoint
	err := query.Scan(&trends).Error
	return trends, err
}

// GetHourlyTrends 获取每小时趋势（最近24小时）
func (r *LogRepository) GetHourlyTrends(tenantID *uint) ([]TrendDataPoint, error) {
	startTime := time.Now().Add(-24 * time.Hour)

	query := r.db.Model(&models.AuditLog{}).
		Select("TO_CHAR(created_at, 'YYYY-MM-DD HH24:00') as timestamp, COUNT(*) as count").
		Where("created_at >= ?", startTime).
		Group("TO_CHAR(created_at, 'YYYY-MM-DD HH24:00')").
		Order("timestamp ASC")

	if tenantID != nil {
		query = query.Where("tenant_id = ?", *tenantID)
	}

	var trends []TrendDataPoint
	err := query.Scan(&trends).Error
	return trends, err
}

// GetTodayCount 获取今日日志数量
func (r *LogRepository) GetTodayCount(tenantID *uint) (int64, error) {
	today := time.Now().Truncate(24 * time.Hour)

	query := r.db.Model(&models.AuditLog{}).
		Where("created_at >= ?", today)

	if tenantID != nil {
		query = query.Where("tenant_id = ?", *tenantID)
	}

	var count int64
	err := query.Count(&count).Error
	return count, err
}

// GetCountByTimeRange 获取指定时间范围内的日志数量
func (r *LogRepository) GetCountByTimeRange(tenantID *uint, startTime time.Time) (int64, error) {
	query := r.db.Model(&models.AuditLog{}).
		Where("created_at >= ?", startTime)

	if tenantID != nil {
		query = query.Where("tenant_id = ?", *tenantID)
	}

	var count int64
	err := query.Count(&count).Error
	return count, err
}

// GetErrorCount 获取错误日志数量（log_level = 'ERROR' 或 http_status >= 500）
func (r *LogRepository) GetErrorCount(tenantID *uint, startTime time.Time) (int64, error) {
	query := r.db.Model(&models.AuditLog{}).
		Where("created_at >= ?", startTime).
		Where("log_level = ? OR http_status >= ?", "ERROR", 500)

	if tenantID != nil {
		query = query.Where("tenant_id = ?", *tenantID)
	}

	var count int64
	err := query.Count(&count).Error
	return count, err
}

// GetTopUsers 获取最活跃用户 TOP N
func (r *LogRepository) GetTopUsers(tenantID *uint, startTime time.Time, limit int) ([]UserStats, error) {
	query := r.db.Model(&models.AuditLog{}).
		Select("user_id, username, COUNT(*) as count").
		Where("created_at >= ?", startTime).
		Where("user_id > 0"). // 排除系统用户
		Group("user_id, username").
		Order("count DESC").
		Limit(limit)

	if tenantID != nil {
		query = query.Where("tenant_id = ?", *tenantID)
	}

	var users []UserStats
	err := query.Scan(&users).Error
	return users, err
}

// GetTopModules 获取最活跃模块 TOP N
func (r *LogRepository) GetTopModules(tenantID *uint, startTime time.Time, limit int) ([]ModuleStats, error) {
	query := r.db.Model(&models.AuditLog{}).
		Select("module_name, COUNT(*) as count").
		Where("created_at >= ?", startTime).
		Where("module_name != ''").
		Group("module_name").
		Order("count DESC").
		Limit(limit)

	if tenantID != nil {
		query = query.Where("tenant_id = ?", *tenantID)
	}

	var modules []ModuleStats
	err := query.Scan(&modules).Error
	return modules, err
}

// GetActionDistribution 获取操作类型分布
func (r *LogRepository) GetActionDistribution(tenantID *uint, startTime time.Time) ([]ActionStats, error) {
	query := r.db.Model(&models.AuditLog{}).
		Select("SPLIT_PART(action, ' ', 1) as action_type, COUNT(*) as count").
		Where("created_at >= ?", startTime).
		Group("SPLIT_PART(action, ' ', 1)").
		Order("count DESC")

	if tenantID != nil {
		query = query.Where("tenant_id = ?", *tenantID)
	}

	var actions []ActionStats
	err := query.Scan(&actions).Error
	return actions, err
}

// ============================================
// 日志归档方法（新增）
// ============================================

// GetLogsBeforeDate 查询指定日期之前的日志（用于归档）
func (r *LogRepository) GetLogsBeforeDate(cutoffDate time.Time, limit int) ([]models.AuditLog, error) {
	var logs []models.AuditLog
	err := r.db.Model(&models.AuditLog{}).
		Where("created_at < ?", cutoffDate).
		Order("created_at ASC").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

// DeleteByIDs 批量删除日志（用于归档后清理）
func (r *LogRepository) DeleteByIDs(logIDs []uint) error {
	return r.db.Where("id IN ?", logIDs).Delete(&models.AuditLog{}).Error
}
