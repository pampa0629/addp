package service

import (
	"context"
	"fmt"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type QuickViewPolicyResponse struct {
	Version                   uint64 `json:"version"`
	DirectFlatGeobufMaxRows   int    `json:"direct_flatgeobuf_max_rows"`
	RealtimeTileTimeoutMS     int    `json:"realtime_tile_timeout_ms"`
	RealtimeTileRetryAfterSec int    `json:"realtime_tile_retry_after_sec"`
}
type UpdateQuickViewPolicyInput struct {
	Version                   uint64 `json:"version"`
	DirectFlatGeobufMaxRows   int    `json:"direct_flatgeobuf_max_rows" binding:"required"`
	RealtimeTileTimeoutMS     int    `json:"realtime_tile_timeout_ms" binding:"required"`
	RealtimeTileRetryAfterSec int    `json:"realtime_tile_retry_after_sec" binding:"required"`
}
type QuickViewPolicyService struct {
	repo    *repository.QuickViewPolicyRepository
	applier func(QuickViewPolicyResponse)
}

func NewQuickViewPolicyService(repo *repository.QuickViewPolicyRepository) *QuickViewPolicyService {
	return &QuickViewPolicyService{repo: repo}
}
func (s *QuickViewPolicyService) SetApplier(applier func(QuickViewPolicyResponse)) {
	s.applier = applier
}
func (s *QuickViewPolicyService) Get(ctx context.Context) (QuickViewPolicyResponse, error) {
	value, err := s.repo.Get(ctx)
	if err != nil {
		return QuickViewPolicyResponse{}, err
	}
	if value == nil {
		value = defaultQuickViewPolicy()
	}
	return quickViewPolicyResponse(value), nil
}
func (s *QuickViewPolicyService) Update(ctx context.Context, input UpdateQuickViewPolicyInput, updatedBy uint) (QuickViewPolicyResponse, error) {
	if input.DirectFlatGeobufMaxRows < 1 || input.DirectFlatGeobufMaxRows > 1000000 {
		return QuickViewPolicyResponse{}, fmt.Errorf("direct_flatgeobuf_max_rows must be between 1 and 1000000")
	}
	if input.RealtimeTileTimeoutMS < 100 || input.RealtimeTileTimeoutMS > 120000 {
		return QuickViewPolicyResponse{}, fmt.Errorf("realtime_tile_timeout_ms must be between 100 and 120000")
	}
	if input.RealtimeTileRetryAfterSec < 1 || input.RealtimeTileRetryAfterSec > 3600 {
		return QuickViewPolicyResponse{}, fmt.Errorf("realtime_tile_retry_after_sec must be between 1 and 3600")
	}
	value := &models.QuickViewPolicy{DirectFlatGeobufMaxRows: input.DirectFlatGeobufMaxRows, RealtimeTileTimeoutMS: input.RealtimeTileTimeoutMS, RealtimeTileRetryAfterSec: input.RealtimeTileRetryAfterSec, UpdatedBy: updatedBy}
	if err := s.repo.Save(ctx, value, input.Version); err != nil {
		return QuickViewPolicyResponse{}, err
	}
	response := quickViewPolicyResponse(value)
	if s.applier != nil {
		s.applier(response)
	}
	return response, nil
}
func defaultQuickViewPolicy() *models.QuickViewPolicy {
	return &models.QuickViewPolicy{DirectFlatGeobufMaxRows: 2000, RealtimeTileTimeoutMS: 2500, RealtimeTileRetryAfterSec: 60}
}
func quickViewPolicyResponse(value *models.QuickViewPolicy) QuickViewPolicyResponse {
	return QuickViewPolicyResponse{Version: value.Version, DirectFlatGeobufMaxRows: value.DirectFlatGeobufMaxRows, RealtimeTileTimeoutMS: value.RealtimeTileTimeoutMS, RealtimeTileRetryAfterSec: value.RealtimeTileRetryAfterSec}
}
