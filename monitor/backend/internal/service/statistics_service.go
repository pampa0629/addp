package service

import (
	"context"
	"time"

	commonExecution "github.com/addp/common/execution"
)

// StatisticsService 统计服务
type StatisticsService struct {
	repo *commonExecution.TaskExecutionRepository
}

// NewStatisticsService 创建统计服务
func NewStatisticsService(repo *commonExecution.TaskExecutionRepository) *StatisticsService {
	return &StatisticsService{
		repo: repo,
	}
}

// StatisticsRequest 统计请求
type StatisticsRequest struct {
	TenantID int
	Module   string
	Duration string // "24h", "7d", "30d"
}

// StatisticsResponse 统计响应
type StatisticsResponse struct {
	Total              int64   `json:"total"`
	SuccessCount       int64   `json:"success_count"`
	FailedCount        int64   `json:"failed_count"`
	RunningCount       int64   `json:"running_count"`
	SuccessRate        float64 `json:"success_rate"`
	AvgExecutionTimeMs float64 `json:"avg_execution_time_ms"`
}

// GetStatistics 获取统计数据
func (s *StatisticsService) GetStatistics(ctx context.Context, req *StatisticsRequest) (*StatisticsResponse, error) {
	// 根据 duration 计算时间范围
	var startDate *time.Time
	now := time.Now()

	switch req.Duration {
	case "24h":
		t := now.Add(-24 * time.Hour)
		startDate = &t
	case "7d":
		t := now.Add(-7 * 24 * time.Hour)
		startDate = &t
	case "30d":
		t := now.Add(-30 * 24 * time.Hour)
		startDate = &t
	}

	// 获取统计数据
	stats, err := s.repo.GetStatistics(ctx, req.TenantID, req.Module, startDate, nil)
	if err != nil {
		return nil, err
	}

	return &StatisticsResponse{
		Total:              stats.Total,
		SuccessCount:       stats.SuccessCount,
		FailedCount:        stats.FailedCount,
		RunningCount:       stats.RunningCount,
		SuccessRate:        stats.SuccessRate,
		AvgExecutionTimeMs: stats.AvgExecutionTimeMs,
	}, nil
}

// TrendDataPoint 趋势数据点（与 repository 保持一致）
type TrendDataPoint struct {
	Date         string  `json:"date"`
	Total        int64   `json:"total"`
	SuccessCount int64   `json:"success_count"`
	FailedCount  int64   `json:"failed_count"`
	AvgTimeMs    float64 `json:"avg_time_ms"`
}

// GetTrendData 获取趋势数据（按天聚合）
func (s *StatisticsService) GetTrendData(ctx context.Context, tenantID int, module string, days int) ([]TrendDataPoint, error) {
	// 获取趋势数据
	trendData, err := s.repo.GetTrendData(ctx, tenantID, module, days)
	if err != nil {
		return nil, err
	}

	// 转换为响应格式（将 time.Time 转为字符串）
	result := make([]TrendDataPoint, len(trendData))
	for i, data := range trendData {
		result[i] = TrendDataPoint{
			Date:         data.Date.Format("2006-01-02"),
			Total:        data.Total,
			SuccessCount: data.SuccessCount,
			FailedCount:  data.FailedCount,
			AvgTimeMs:    data.AvgTimeMs,
		}
	}

	return result, nil
}
