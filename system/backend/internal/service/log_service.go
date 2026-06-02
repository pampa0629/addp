package service

import (
	"errors"
	"time"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
)

type LogService struct {
	repo     *repository.LogRepository
	userRepo *repository.UserRepository
}

func NewLogService(repo *repository.LogRepository, userRepo *repository.UserRepository) *LogService {
	return &LogService{
		repo:     repo,
		userRepo: userRepo,
	}
}

func (s *LogService) Create(log *models.AuditLog) error {
	return s.repo.Create(log)
}

// List 查询日志列表（支持高级过滤）
func (s *LogService) List(page, pageSize int, filters *models.AuditLogFilters, currentUserID uint) ([]models.AuditLog, int64, error) {
	offset := (page - 1) * pageSize

	// 获取当前用户信息
	currentUser, err := s.userRepo.GetByID(currentUserID)
	if err != nil {
		return nil, 0, errors.New("当前用户不存在")
	}

	// SuperAdmin可以查看所有日志
	if currentUser.UserType == models.UserTypeSuperAdmin {
		return s.repo.List(offset, pageSize, filters)
	}

	// 租户管理员和普通用户只能查看本租户的日志
	if currentUser.TenantID == nil {
		return []models.AuditLog{}, 0, nil
	}
	return s.repo.ListByTenant(*currentUser.TenantID, offset, pageSize, filters)
}

func (s *LogService) GetByID(id uint) (*models.AuditLog, error) {
	return s.repo.GetByID(id)
}

// ============================================
// 统计分析方法（新增）
// ============================================

// LogStatsResponse 日志统计响应
type LogStatsResponse struct {
	TodayCount         int64                    `json:"today_count"`         // 今日日志数
	WeekCount          int64                    `json:"week_count"`          // 7日日志数
	MonthCount         int64                    `json:"month_count"`         // 30日日志数
	ErrorCount         int64                    `json:"error_count"`         // 今日错误数
	TopUsers           []repository.UserStats   `json:"top_users"`           // TOP 5 活跃用户
	TopModules         []repository.ModuleStats `json:"top_modules"`         // TOP 5 活跃模块
	ActionDistribution []repository.ActionStats `json:"action_distribution"` // 操作类型分布
}

// TrendsResponse 时间趋势响应
type TrendsResponse struct {
	Daily  []repository.TrendDataPoint `json:"daily"`  // 30天趋势
	Hourly []repository.TrendDataPoint `json:"hourly"` // 24小时趋势
}

// GetStats 获取日志统计（自动应用租户隔离）
func (s *LogService) GetStats(currentUserID uint) (*LogStatsResponse, error) {
	// 获取当前用户信息
	currentUser, err := s.userRepo.GetByID(currentUserID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}

	// 确定租户ID（SuperAdmin 为 nil，其他用户使用自己的 tenantID）
	var tenantID *uint
	if currentUser.UserType != models.UserTypeSuperAdmin {
		if currentUser.TenantID == nil {
			return &LogStatsResponse{}, nil // 用户无租户，返回空数据
		}
		tenantID = currentUser.TenantID
	}

	// 并发查询各项统计数据
	var (
		todayCount int64
		weekCount  int64
		monthCount int64
		errorCount int64
		topUsers   []repository.UserStats
		topModules []repository.ModuleStats
		actions    []repository.ActionStats
		errChan    = make(chan error, 7)
	)

	// 今日日志数
	go func() {
		count, err := s.repo.GetTodayCount(tenantID)
		if err == nil {
			todayCount = count
		}
		errChan <- err
	}()

	// 7日日志数
	go func() {
		count, err := s.repo.GetCountByTimeRange(tenantID, getStartTime(7))
		if err == nil {
			weekCount = count
		}
		errChan <- err
	}()

	// 30日日志数
	go func() {
		count, err := s.repo.GetCountByTimeRange(tenantID, getStartTime(30))
		if err == nil {
			monthCount = count
		}
		errChan <- err
	}()

	// 今日错误数
	go func() {
		count, err := s.repo.GetErrorCount(tenantID, getStartTime(1))
		if err == nil {
			errorCount = count
		}
		errChan <- err
	}()

	// TOP 5 活跃用户（7日）
	go func() {
		users, err := s.repo.GetTopUsers(tenantID, getStartTime(7), 5)
		if err == nil {
			topUsers = users
		}
		errChan <- err
	}()

	// TOP 5 活跃模块（7日）
	go func() {
		modules, err := s.repo.GetTopModules(tenantID, getStartTime(7), 5)
		if err == nil {
			topModules = modules
		}
		errChan <- err
	}()

	// 操作类型分布（7日）
	go func() {
		acts, err := s.repo.GetActionDistribution(tenantID, getStartTime(7))
		if err == nil {
			actions = acts
		}
		errChan <- err
	}()

	// 等待所有查询完成
	for i := 0; i < 7; i++ {
		if err := <-errChan; err != nil {
			// 记录错误但继续（部分统计失败不影响其他）
			// 可以考虑添加日志记录
		}
	}

	return &LogStatsResponse{
		TodayCount:         todayCount,
		WeekCount:          weekCount,
		MonthCount:         monthCount,
		ErrorCount:         errorCount,
		TopUsers:           topUsers,
		TopModules:         topModules,
		ActionDistribution: actions,
	}, nil
}

// GetTrends 获取时间趋势（自动应用租户隔离）
func (s *LogService) GetTrends(currentUserID uint) (*TrendsResponse, error) {
	// 获取当前用户信息
	currentUser, err := s.userRepo.GetByID(currentUserID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}

	// 确定租户ID
	var tenantID *uint
	if currentUser.UserType != models.UserTypeSuperAdmin {
		if currentUser.TenantID == nil {
			return &TrendsResponse{}, nil
		}
		tenantID = currentUser.TenantID
	}

	// 并发查询
	var (
		daily   []repository.TrendDataPoint
		hourly  []repository.TrendDataPoint
		errChan = make(chan error, 2)
	)

	// 30天趋势
	go func() {
		trends, err := s.repo.GetDailyTrends(tenantID, 30)
		if err == nil {
			daily = trends
		}
		errChan <- err
	}()

	// 24小时趋势
	go func() {
		trends, err := s.repo.GetHourlyTrends(tenantID)
		if err == nil {
			hourly = trends
		}
		errChan <- err
	}()

	// 等待完成
	for i := 0; i < 2; i++ {
		<-errChan
	}

	return &TrendsResponse{
		Daily:  daily,
		Hourly: hourly,
	}, nil
}

// getStartTime 辅助函数：获取N天前的开始时间
func getStartTime(days int) time.Time {
	return time.Now().AddDate(0, 0, -days).Truncate(24 * time.Hour)
}
