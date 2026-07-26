package iam

import (
	"context"
	"fmt"
	"strings"

	commonapi "github.com/addp/common/api"
)

type AuditQueryService struct {
	repository *Repository
}

func NewAuditQueryService(repository *Repository) *AuditQueryService {
	return &AuditQueryService{repository: repository}
}

func (s *AuditQueryService) List(
	ctx context.Context,
	query AuditQuery,
	page int,
	pageSize int,
) ([]AuditLog, int64, error) {
	if err := s.validateQuery(query); err != nil {
		return nil, 0, err
	}
	if err := validateManagementPagination(page, pageSize); err != nil {
		return nil, 0, err
	}
	var logs []AuditLog
	var total int64
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		var err error
		logs, total, err = tx.ListAuditLogs(ctx, query, page, pageSize)
		return err
	})
	return logs, total, err
}

func (s *AuditQueryService) Get(ctx context.Context, auditID int64, tenantID *int64) (*AuditLog, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if auditID <= 0 || (tenantID != nil && *tenantID <= 0) {
		return nil, fmt.Errorf("%w: invalid audit identity", commonapi.ErrBadRequest)
	}
	return s.repository.GetAuditLog(ctx, auditID, tenantID)
}

func (s *AuditQueryService) Summary(ctx context.Context, query AuditQuery) (*AuditSummary, error) {
	if err := s.validateQuery(query); err != nil {
		return nil, err
	}
	return s.repository.GetAuditSummary(ctx, query)
}

func (s *AuditQueryService) Trends(ctx context.Context, query AuditQuery) ([]AuditTrendPoint, error) {
	if err := s.validateQuery(query); err != nil {
		return nil, err
	}
	return s.repository.GetAuditTrends(ctx, query)
}

func (s *AuditQueryService) validateQuery(query AuditQuery) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if query.TenantID != nil && *query.TenantID <= 0 {
		return fmt.Errorf("%w: invalid tenant", commonapi.ErrBadRequest)
	}
	if query.StartTime != nil && query.EndTime != nil && !query.StartTime.Before(*query.EndTime) {
		return fmt.Errorf("%w: audit start time must be before end time", commonapi.ErrBadRequest)
	}
	if query.Result != "" && !containsAuditValue(query.Result, []string{"succeeded", "failed", "denied", "ignored"}) {
		return fmt.Errorf("%w: invalid audit result", commonapi.ErrBadRequest)
	}
	if query.RiskLevel != "" && !containsAuditValue(query.RiskLevel, []string{"low", "medium", "high", "critical"}) {
		return fmt.Errorf("%w: invalid audit risk level", commonapi.ErrBadRequest)
	}
	query.EventName = strings.TrimSpace(query.EventName)
	query.ModuleName = strings.TrimSpace(query.ModuleName)
	query.RequestID = strings.TrimSpace(query.RequestID)
	return nil
}

func containsAuditValue(value string, allowed []string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
