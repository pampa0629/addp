package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/monitor/internal/repository"
)

type fakeExecutionRuntimeMetricsRepository struct {
	rows      []repository.ExecutionRuntimeMetricRow
	err       error
	tenantID  int
	module    string
	startedAt time.Time
	endedAt   time.Time
}

func (f *fakeExecutionRuntimeMetricsRepository) List(
	_ context.Context,
	tenantID int,
	module string,
	windowStartedAt time.Time,
	windowEndedAt time.Time,
) ([]repository.ExecutionRuntimeMetricRow, error) {
	f.tenantID = tenantID
	f.module = module
	f.startedAt = windowStartedAt
	f.endedAt = windowEndedAt
	return f.rows, f.err
}

func TestGetExecutionRuntimeMetricsBuildsWindowAndDerivedRates(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 30, 0, 0, time.UTC)
	repo := &fakeExecutionRuntimeMetricsRepository{rows: []repository.ExecutionRuntimeMetricRow{{
		Module: "quality", TaskType: "rule_check", ExecutionBoundary: "bounded",
		CreatedCount: 40, CompletedCount: 24, SuccessCount: 18, FailedCount: 4, TimeoutCount: 2,
		PendingCount: 10, RunningCount: 6, AutomaticRetryCount: 5, UserRetryCount: 2, RecoveryCount: 1,
		AvgQueueDurationMs: 1234.567, P95QueueDurationMs: 9999.999,
		AvgExecutionDurationMs: 2345.678, P95ExecutionDurationMs: 8888.888,
	}}}
	service := NewStatisticsServiceWithRuntimeMetrics(nil, repo)
	service.now = func() time.Time { return now }

	result, err := service.GetExecutionRuntimeMetrics(context.Background(), ExecutionRuntimeMetricsRequest{
		TenantID: 7, Module: "quality", Duration: "24h",
	})
	if err != nil {
		t.Fatalf("GetExecutionRuntimeMetrics() error = %v", err)
	}
	if repo.tenantID != 7 || repo.module != "quality" {
		t.Fatalf("repository filter = tenant %d module %q", repo.tenantID, repo.module)
	}
	if !repo.endedAt.Equal(now) || !repo.startedAt.Equal(now.Add(-24*time.Hour)) {
		t.Fatalf("repository window = %s..%s", repo.startedAt, repo.endedAt)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1", len(result.Groups))
	}
	metric := result.Groups[0]
	if metric.ThroughputPerHour != 1 {
		t.Fatalf("throughput_per_hour = %v, want 1", metric.ThroughputPerHour)
	}
	if metric.FailureRate != 25 {
		t.Fatalf("failure_rate = %v, want 25", metric.FailureRate)
	}
	if metric.AutomaticRetryRate != 12.5 || metric.UserRetryRate != 5 || metric.RecoveryRate != 2.5 {
		t.Fatalf("retry/recovery rates = %v/%v/%v", metric.AutomaticRetryRate, metric.UserRetryRate, metric.RecoveryRate)
	}
	if metric.AvgQueueDurationMs != 1234.57 || metric.P95ExecutionDurationMs != 8888.89 {
		t.Fatalf("rounded durations = %v/%v", metric.AvgQueueDurationMs, metric.P95ExecutionDurationMs)
	}
}

func TestGetExecutionRuntimeMetricsRejectsUnknownDuration(t *testing.T) {
	service := NewStatisticsServiceWithRuntimeMetrics(nil, &fakeExecutionRuntimeMetricsRepository{})
	_, err := service.GetExecutionRuntimeMetrics(context.Background(), ExecutionRuntimeMetricsRequest{Duration: "1h"})
	if !errors.Is(err, ErrInvalidRuntimeMetricDuration) {
		t.Fatalf("error = %v, want ErrInvalidRuntimeMetricDuration", err)
	}
}

func TestGetExecutionRuntimeMetricsPropagatesRepositoryError(t *testing.T) {
	want := errors.New("query failed")
	service := NewStatisticsServiceWithRuntimeMetrics(nil, &fakeExecutionRuntimeMetricsRepository{err: want})
	_, err := service.GetExecutionRuntimeMetrics(context.Background(), ExecutionRuntimeMetricsRequest{Duration: "7d"})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
