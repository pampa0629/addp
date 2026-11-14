package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
)

var ErrCaptureMetricWindow = errors.New("invalid CDC metric history window")

type CaptureMetricWindow struct {
	Start, End time.Time
	Step       time.Duration
}

// NewCaptureMetricWindow is the HTTP boundary's sole window validation.
func NewCaptureMetricWindow(start, end, now time.Time) (CaptureMetricWindow, error) {
	if end.After(now) {
		return CaptureMetricWindow{}, ErrCaptureMetricWindow
	}
	start, end = start.UTC().Truncate(time.Minute), end.UTC().Truncate(time.Minute)
	if !start.Before(end) || start.Before(now.UTC().Truncate(time.Minute).Add(-models.CaptureMetricRetention)) || end.Sub(start) > models.CaptureMetricRetention {
		return CaptureMetricWindow{}, ErrCaptureMetricWindow
	}
	minutes := int64(end.Sub(start) / time.Minute)
	return CaptureMetricWindow{Start: start, End: end, Step: time.Duration((minutes+999)/1000) * time.Minute}, nil
}

func (s *TaskService) CaptureMetricHistory(ctx context.Context, id, tenantID uint, generation uint64, window CaptureMetricWindow, now time.Time) (*models.CaptureMetricHistoryResponse, error) {
	task, err := s.taskRepo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if task.TenantID != tenantID || !planner.IsDatabaseCDCTaskConfig(task.Config) {
		return nil, commonAPI.ErrNotFound
	}
	queryStart := window.Start
	if cutoff := now.Add(-models.CaptureMetricRetention); queryStart.Before(cutoff) {
		queryStart = cutoff
	}
	rows, err := repository.NewCaptureRepository(s.db).MetricHistory(ctx, task, generation, queryStart, window.End)
	if err != nil {
		return nil, err
	}
	result := &models.CaptureMetricHistoryResponse{TaskID: id, Generation: generation, Start: window.Start, End: window.End,
		StepSeconds: int64(window.Step / time.Second), QueriedAt: now, Points: []models.CaptureMetricPoint{}}
	for stamp := window.Start; stamp.Before(window.End); stamp = stamp.Add(window.Step) {
		result.Points = append(result.Points, models.CaptureMetricPoint{Timestamp: stamp})
	}
	for _, row := range rows {
		var point models.CaptureMetricPoint
		data, err := json.Marshal(row.Point)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &point); err != nil {
			return nil, err
		}
		index := int(row.Minute.Sub(window.Start) / window.Step)
		point.Timestamp = result.Points[index].Timestamp
		result.Points[index] = point
	}
	return result, nil
}
