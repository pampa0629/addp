package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	commonlogger "github.com/addp/common/logger"
	"github.com/addp/standard/internal/repository"
	"gorm.io/gorm"
)

const (
	standardLifecycleActive                    = "active"
	standardLifecycleDeleting                  = "deleting"
	standardReferenceDeletionReconcileInterval = time.Minute
)

var ErrModelReferenceGuardUnavailable = errors.New("standard reference guard unavailable")

type StandardResourceReferencedError struct {
	Impact *commonclient.StandardReferenceGuardResponse
}

func (e *StandardResourceReferencedError) Error() string {
	return "standard resource is still referenced"
}

func (e *StandardResourceReferencedError) Unwrap() error { return commonapi.ErrConflict }

type StandardReferenceLocalDelete func(*gorm.DB, int64, int64) error

type standardReferenceDeletionOutcome int

const (
	standardReferenceDeletionCompleted standardReferenceDeletionOutcome = iota
	standardReferenceDeletionNotFound
	standardReferenceDeletionReferenced
	standardReferenceDeletionLocalFailed
	standardReferenceDeletionPendingModelFinalize
)

type StandardReferenceDeletionService struct {
	db           *gorm.DB
	model        *commonclient.ModelClient
	quality      *commonclient.QualityClient
	operations   *repository.StandardReferenceDeletionRepository
	localDeletes map[string]StandardReferenceLocalDelete
	log          *slog.Logger
	stopCh       chan struct{}
	stopOnce     sync.Once
}

func NewStandardReferenceDeletionService(db *gorm.DB, modelClient *commonclient.ModelClient, qualityClient *commonclient.QualityClient) *StandardReferenceDeletionService {
	return &StandardReferenceDeletionService{
		db:           db,
		model:        modelClient,
		quality:      qualityClient,
		operations:   repository.NewStandardReferenceDeletionRepository(db),
		localDeletes: make(map[string]StandardReferenceLocalDelete),
		log:          commonlogger.With("component", "standard_reference_deletion"),
		stopCh:       make(chan struct{}),
	}
}

func (s *StandardReferenceDeletionService) RegisterLocalDelete(resourceType string, deleteLocal StandardReferenceLocalDelete) {
	if s == nil || deleteLocal == nil {
		return
	}
	s.localDeletes[resourceType] = deleteLocal
}

func (s *StandardReferenceDeletionService) Start(ctx context.Context) {
	if s == nil || s.db == nil || s.model == nil {
		return
	}
	go s.runReconciler(ctx)
}

func (s *StandardReferenceDeletionService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *StandardReferenceDeletionService) runReconciler(ctx context.Context) {
	s.reconcilePending(ctx)
	ticker := time.NewTicker(standardReferenceDeletionReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.reconcilePending(ctx)
		}
	}
}

func (s *StandardReferenceDeletionService) reconcilePending(ctx context.Context) {
	operations, err := s.operations.ListDue(time.Now(), 100)
	if err != nil {
		s.log.Error("读取标准引用删除协调记录失败", "error", err)
		return
	}
	for _, operation := range operations {
		err := s.process(ctx, operation.TenantID, operation.ResourceType, operation.ResourceID, nil, true)
		if err == nil {
			continue
		}
		var referenced *StandardResourceReferencedError
		if errors.As(err, &referenced) {
			continue
		}
		s.log.Error("标准引用删除补偿失败", "tenant_id", operation.TenantID, "resource_type", operation.ResourceType, "resource_id", operation.ResourceID, "error", err)
		if recordErr := s.operations.RecordFailure(operation.ID, err); recordErr != nil {
			s.log.Error("记录标准引用删除补偿失败", "operation_id", operation.ID, "error", recordErr)
		}
	}
}

func (s *StandardReferenceDeletionService) Delete(
	ctx context.Context,
	tenantID int64,
	resourceType string,
	resourceID int64,
	deleteLocal StandardReferenceLocalDelete,
) error {
	if s == nil || s.db == nil || s.model == nil || s.operations == nil {
		return ErrModelReferenceGuardUnavailable
	}
	existed, err := s.operations.Ensure(tenantID, resourceType, resourceID)
	if err != nil {
		return err
	}
	err = s.process(ctx, tenantID, resourceType, resourceID, deleteLocal, existed)
	if err != nil {
		if recordErr := s.operations.RecordFailureByResource(tenantID, resourceType, resourceID, err); recordErr != nil {
			s.log.Error("记录标准引用删除失败", "tenant_id", tenantID, "resource_type", resourceType, "resource_id", resourceID, "error", recordErr)
		}
	}
	return err
}

func (s *StandardReferenceDeletionService) process(
	ctx context.Context,
	tenantID int64,
	resourceType string,
	resourceID int64,
	deleteLocal StandardReferenceLocalDelete,
	existedAtEnsure bool,
) error {
	if resourceType == "domain" && s.quality == nil {
		return fmt.Errorf("%w: quality reference guard is not configured", ErrModelReferenceGuardUnavailable)
	}
	client := s.model.WithTenantID(uint(tenantID))
	qualityClient := s.quality
	if qualityClient != nil {
		qualityClient = qualityClient.WithTenantID(uint(tenantID))
	}
	qualityGuardEnabled := resourceType == "domain" && qualityClient != nil
	var outcome = standardReferenceDeletionCompleted
	var impact *commonclient.StandardReferenceGuardResponse
	var localDeleteErr error
	err := s.db.Transaction(func(tx *gorm.DB) error {
		operation, err := s.operations.LockOperation(tx, tenantID, resourceType, resourceID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if !existedAtEnsure {
				outcome = standardReferenceDeletionNotFound
			}
			return nil
		}
		if err != nil {
			return err
		}
		_, exists, err := s.operations.LockResource(tx, tenantID, resourceType, resourceID)
		if err != nil {
			return err
		}
		if !exists {
			resourceNotFound := false
			if _, err := client.SetStandardReferenceGuard(ctx, resourceType, resourceID, commonclient.StandardReferenceGuardDeleted); err != nil {
				if status, ok := commonclient.TenantAPIStatusCode(err); !ok || status != 409 {
					return fmt.Errorf("%w: finalize missing standard resource guard: %v", ErrModelReferenceGuardUnavailable, err)
				}
				resourceNotFound = true
			}
			if qualityGuardEnabled {
				if _, err := qualityClient.SetStandardReferenceGuard(ctx, resourceType, resourceID, commonclient.StandardReferenceGuardDeleted); err != nil {
					if status, ok := commonclient.TenantAPIStatusCode(err); !ok || status != 409 {
						return fmt.Errorf("%w: finalize quality reference guard: %v", ErrModelReferenceGuardUnavailable, err)
					}
					resourceNotFound = true
				}
			}
			if err := s.operations.DeleteOperation(tx, operation.ID); err != nil {
				return err
			}
			if resourceNotFound {
				outcome = standardReferenceDeletionNotFound
			}
			return nil
		}
		if err := s.operations.SetDeleting(tx, resourceType, resourceID, tenantID); err != nil {
			return err
		}

		if deleteLocal == nil {
			deleteLocal = s.localDeletes[resourceType]
		}
		if deleteLocal == nil {
			return fmt.Errorf("%w: local delete handler is not configured", ErrModelReferenceGuardUnavailable)
		}
		impact, err = client.SetStandardReferenceGuard(ctx, resourceType, resourceID, commonclient.StandardReferenceGuardFrozen)
		if err != nil {
			return fmt.Errorf("%w: freeze model reference guard: %v", ErrModelReferenceGuardUnavailable, err)
		}
		// Freeze every consumer before deciding whether to reject deletion. A prior
		// attempt may have frozen Quality even when its response was lost.
		if qualityGuardEnabled {
			qualityImpact, err := qualityClient.SetStandardReferenceGuard(ctx, resourceType, resourceID, commonclient.StandardReferenceGuardFrozen)
			if err != nil {
				return fmt.Errorf("%w: freeze quality reference guard: %v", ErrModelReferenceGuardUnavailable, err)
			}
			impact.ReferenceCount += qualityImpact.ReferenceCount
			impact.Summary = append(impact.Summary, qualityImpact.Summary...)
			impact.Sample = append(impact.Sample, qualityImpact.Sample...)
			impact.SampleTruncated = impact.SampleTruncated || qualityImpact.SampleTruncated
		}
		if impact.ReferenceCount > 0 {
			if qualityGuardEnabled {
				if _, err := qualityClient.SetStandardReferenceGuard(ctx, resourceType, resourceID, commonclient.StandardReferenceGuardOpen); err != nil {
					return fmt.Errorf("%w: release quality reference guard: %v", ErrModelReferenceGuardUnavailable, err)
				}
			}
			if _, err := client.SetStandardReferenceGuard(ctx, resourceType, resourceID, commonclient.StandardReferenceGuardOpen); err != nil {
				return fmt.Errorf("%w: release model reference guard: %v", ErrModelReferenceGuardUnavailable, err)
			}
			if err := s.operations.SetActive(tx, resourceType, resourceID, tenantID); err != nil {
				return fmt.Errorf("%w: restore standard lifecycle: %v", ErrModelReferenceGuardUnavailable, err)
			}
			if err := s.operations.DeleteOperation(tx, operation.ID); err != nil {
				return err
			}
			outcome = standardReferenceDeletionReferenced
			return nil
		}

		if err := tx.Transaction(func(deleteTx *gorm.DB) error {
			return deleteLocal(deleteTx, resourceID, tenantID)
		}); err != nil {
			localDeleteErr = err
			if qualityGuardEnabled {
				if _, restoreErr := qualityClient.SetStandardReferenceGuard(ctx, resourceType, resourceID, commonclient.StandardReferenceGuardOpen); restoreErr != nil {
					return fmt.Errorf("%w: delete standard resource: %v; restore quality guard: %v", ErrModelReferenceGuardUnavailable, err, restoreErr)
				}
			}
			if _, restoreErr := client.SetStandardReferenceGuard(ctx, resourceType, resourceID, commonclient.StandardReferenceGuardOpen); restoreErr != nil {
				return fmt.Errorf("%w: delete standard resource: %v; restore guard: %v", ErrModelReferenceGuardUnavailable, err, restoreErr)
			}
			if restoreErr := s.operations.SetActive(tx, resourceType, resourceID, tenantID); restoreErr != nil {
				return fmt.Errorf("%w: delete standard resource: %v; restore lifecycle: %v", ErrModelReferenceGuardUnavailable, err, restoreErr)
			}
			if err := s.operations.DeleteOperation(tx, operation.ID); err != nil {
				return err
			}
			outcome = standardReferenceDeletionLocalFailed
			return nil
		}
		outcome = standardReferenceDeletionPendingModelFinalize
		return nil
	})
	if err != nil {
		return err
	}
	switch outcome {
	case standardReferenceDeletionNotFound:
		return commonapi.ErrNotFound
	case standardReferenceDeletionReferenced:
		return &StandardResourceReferencedError{Impact: impact}
	case standardReferenceDeletionLocalFailed:
		return localDeleteErr
	case standardReferenceDeletionPendingModelFinalize:
		return s.process(ctx, tenantID, resourceType, resourceID, nil, true)
	default:
		return nil
	}
}
