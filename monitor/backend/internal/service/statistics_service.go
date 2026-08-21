package service

import (
	"context"
	"errors"
	"math"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/monitor/internal/repository"
)

var ErrInvalidRuntimeMetricDuration = errors.New("invalid execution runtime metric duration")

type ExecutionRuntimeMetricsRepository interface {
	List(ctx context.Context, tenantID int, module string, windowStartedAt, windowEndedAt time.Time) ([]repository.ExecutionRuntimeMetricRow, error)
}

// StatisticsService 统计服务
type StatisticsService struct {
	repo               *commonExecution.TaskExecutionRepository
	runtimeMetricsRepo ExecutionRuntimeMetricsRepository
	now                func() time.Time
}

// NewStatisticsService 创建统计服务
func NewStatisticsService(repo *commonExecution.TaskExecutionRepository) *StatisticsService {
	return &StatisticsService{
		repo: repo,
		now:  time.Now,
	}
}

// NewStatisticsServiceWithRuntimeMetrics creates a statistics service with grouped runtime metrics enabled.
func NewStatisticsServiceWithRuntimeMetrics(
	repo *commonExecution.TaskExecutionRepository,
	runtimeMetricsRepo ExecutionRuntimeMetricsRepository,
) *StatisticsService {
	service := NewStatisticsService(repo)
	service.runtimeMetricsRepo = runtimeMetricsRepo
	return service
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
	stats, err := s.repo.GetStatistics(ctx, commonExecution.TaskExecutionFilter{
		TenantID:  req.TenantID,
		Module:    req.Module,
		StartDate: startDate,
	})
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

// ExecutionRuntimeMetricsRequest selects one stable observation window.
type ExecutionRuntimeMetricsRequest struct {
	TenantID int
	Module   string
	Duration string
}

// ExecutionRuntimeMetric describes one module, task type and execution-boundary group.
type ExecutionRuntimeMetric struct {
	Module                 string  `json:"module"`
	TaskType               string  `json:"task_type"`
	ExecutionBoundary      string  `json:"execution_boundary"`
	CreatedCount           int64   `json:"created_count"`
	CompletedCount         int64   `json:"completed_count"`
	SuccessCount           int64   `json:"success_count"`
	FailedCount            int64   `json:"failed_count"`
	TimeoutCount           int64   `json:"timeout_count"`
	CancelledCount         int64   `json:"cancelled_count"`
	PendingCount           int64   `json:"pending_count"`
	RunningCount           int64   `json:"running_count"`
	AutomaticRetryCount    int64   `json:"automatic_retry_count"`
	UserRetryCount         int64   `json:"user_retry_count"`
	RecoveryCount          int64   `json:"recovery_count"`
	ThroughputPerHour      float64 `json:"throughput_per_hour"`
	FailureRate            float64 `json:"failure_rate"`
	AutomaticRetryRate     float64 `json:"automatic_retry_rate"`
	UserRetryRate          float64 `json:"user_retry_rate"`
	RecoveryRate           float64 `json:"recovery_rate"`
	AvgQueueDurationMs     float64 `json:"avg_queue_duration_ms"`
	P95QueueDurationMs     float64 `json:"p95_queue_duration_ms"`
	AvgExecutionDurationMs float64 `json:"avg_execution_duration_ms"`
	P95ExecutionDurationMs float64 `json:"p95_execution_duration_ms"`
}

// ExecutionRuntimeMetricsResponse contains windowed metrics plus all-time current backlog counts.
type ExecutionRuntimeMetricsResponse struct {
	Duration        string                   `json:"duration"`
	WindowStartedAt time.Time                `json:"window_started_at"`
	WindowEndedAt   time.Time                `json:"window_ended_at"`
	Groups          []ExecutionRuntimeMetric `json:"groups"`
}

func (s *StatisticsService) GetExecutionRuntimeMetrics(
	ctx context.Context,
	req ExecutionRuntimeMetricsRequest,
) (*ExecutionRuntimeMetricsResponse, error) {
	window, ok := runtimeMetricWindows[req.Duration]
	if !ok {
		return nil, ErrInvalidRuntimeMetricDuration
	}
	if s.runtimeMetricsRepo == nil {
		return nil, errors.New("execution runtime metrics repository is not configured")
	}

	windowEndedAt := s.now().UTC()
	windowStartedAt := windowEndedAt.Add(-window)
	rows, err := s.runtimeMetricsRepo.List(ctx, req.TenantID, req.Module, windowStartedAt, windowEndedAt)
	if err != nil {
		return nil, err
	}

	windowHours := window.Hours()
	groups := make([]ExecutionRuntimeMetric, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, ExecutionRuntimeMetric{
			Module:                 row.Module,
			TaskType:               row.TaskType,
			ExecutionBoundary:      row.ExecutionBoundary,
			CreatedCount:           row.CreatedCount,
			CompletedCount:         row.CompletedCount,
			SuccessCount:           row.SuccessCount,
			FailedCount:            row.FailedCount,
			TimeoutCount:           row.TimeoutCount,
			CancelledCount:         row.CancelledCount,
			PendingCount:           row.PendingCount,
			RunningCount:           row.RunningCount,
			AutomaticRetryCount:    row.AutomaticRetryCount,
			UserRetryCount:         row.UserRetryCount,
			RecoveryCount:          row.RecoveryCount,
			ThroughputPerHour:      roundedRatio(row.CompletedCount, windowHours),
			FailureRate:            roundedPercentage(row.FailedCount+row.TimeoutCount, row.CompletedCount),
			AutomaticRetryRate:     roundedPercentage(row.AutomaticRetryCount, row.CreatedCount),
			UserRetryRate:          roundedPercentage(row.UserRetryCount, row.CreatedCount),
			RecoveryRate:           roundedPercentage(row.RecoveryCount, row.CreatedCount),
			AvgQueueDurationMs:     roundTwo(row.AvgQueueDurationMs),
			P95QueueDurationMs:     roundTwo(row.P95QueueDurationMs),
			AvgExecutionDurationMs: roundTwo(row.AvgExecutionDurationMs),
			P95ExecutionDurationMs: roundTwo(row.P95ExecutionDurationMs),
		})
	}

	return &ExecutionRuntimeMetricsResponse{
		Duration:        req.Duration,
		WindowStartedAt: windowStartedAt,
		WindowEndedAt:   windowEndedAt,
		Groups:          groups,
	}, nil
}

var runtimeMetricWindows = map[string]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

func roundedPercentage(numerator, denominator int64) float64 {
	if denominator == 0 {
		return 0
	}
	return roundTwo(float64(numerator) / float64(denominator) * 100)
}

func roundedRatio(numerator int64, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return roundTwo(float64(numerator) / denominator)
}

func roundTwo(value float64) float64 {
	return math.Round(value*100) / 100
}
