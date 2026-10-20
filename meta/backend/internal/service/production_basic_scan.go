package service

import (
	"context"
	"errors"
	"fmt"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type transferProductionReceiptReader interface {
	ReadProductionReceipt(context.Context, uint, string) (*client.TransferProductionReceipt, error)
}

func (s *ScanExecutionService) WithTransferProductionReceiptReader(reader transferProductionReceiptReader) *ScanExecutionService {
	s.productionReceipts = reader
	return s
}

// No client-supplied targets or actors. Receipt IO precedes every Meta
// transaction; queued/terminal scan recovery needs no live producer or human.
func (s *ScanExecutionService) EnsureTransferProductionBasicScan(ctx context.Context, tenantID uint, id string) (*client.MetaProductionBasicScan, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil || parsed.String() != id || tenantID == 0 {
		return nil, commonapi.ErrBadRequest
	}
	if s == nil || s.db == nil {
		return nil, errors.New("production scan dependency unavailable")
	}
	if _, transactional := s.db.Statement.ConnPool.(gorm.TxCommitter); transactional {
		return nil, commonapi.ErrBadRequest
	}
	prior, err := findProductionBasicScan(s.db.WithContext(ctx), tenantID, id)
	if err == nil {
		return productionBasicScanResult(prior, id)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if s.productionReceipts == nil || s.engineService == nil {
		return nil, errors.New("production scan dependency unavailable")
	}
	receipt, err := s.productionReceipts.ReadProductionReceipt(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if !validProductionBasicScanReceipt(receipt, tenantID, id) {
		return nil, commonapi.ErrConflict
	}
	resource, err := s.engineService.GetResourceByIDWithContext(ctx, receipt.Target.EngineID, tenantID)
	if err != nil {
		return nil, err
	}
	if resource.EngineType != "postgresql" {
		return nil, commonapi.ErrConflict
	}
	businessPath := plugin.EngineCatalogPathWithoutRoot(receipt.Target)
	parts := make([]string, len(businessPath.Segments))
	for index, segment := range businessPath.Segments {
		parts[index] = segment.Name
	}
	locator := (&resourcetree.ResourceLocator{EngineID: receipt.Target.EngineID, Type: resourcetree.TypeTable, Path: parts}).ToURI()
	req := &models.ScanRequest{EngineID: receipt.Target.EngineID, Targets: []string{locator}, ScanDepth: "basic", Force: true, Source: execution.ModuleTransfer}
	// The public request does not carry a completed parent. Only this private
	// adapter can attach its authenticated owner receipt to the shared creator.
	run, err := s.createManualRun(ctx, tenantID, uint(receipt.UserOrigin.PrincipalID), req, func(tx *gorm.DB, child *execution.TaskExecution) (*execution.TaskExecution, error) {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", fmt.Sprintf("meta.initial_production|%d|transfer|%s", tenantID, id)).Error; err != nil {
				return nil, err
			}
		}
		prior, err := findProductionBasicScan(tx, tenantID, id)
		if err == nil {
			return prior, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		child.ParentExecutionID = &id
		child.ActorPrincipalID = &receipt.UserOrigin.PrincipalID
		child.ActorTenantMembershipID = &receipt.UserOrigin.TenantMembershipID
		child.IssuedAuthorizationVersion = &receipt.UserOrigin.AuthorizationVersion
		if child.Metadata == nil {
			child.Metadata = map[string]interface{}{}
		}
		child.Metadata["initial_production_execution_id"] = id
		target := receipt.Target
		target.Segments = append([]plugin.EngineCatalogSegment(nil), receipt.Target.Segments...)
		child.ExecutionConfig["initial_production_target"] = target
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	return productionBasicScanResult(run, id)
}

func validProductionBasicScanReceipt(r *client.TransferProductionReceipt, tenantID uint, id string) bool {
	return r != nil && r.Validate() == nil && r.TenantID == int64(tenantID) && r.ExecutionID == id && r.Attempt == 1 && r.RetryOfExecutionID == nil && r.CreatedAndCommitted && r.Preparation == plugin.TableTargetCreated && r.UserOrigin != nil
}

func findProductionBasicScan(db *gorm.DB, tenantID uint, id string) (*execution.TaskExecution, error) {
	var run execution.TaskExecution
	err := db.Where("tenant_id=? AND module=? AND task_type=? AND parent_execution_id=? AND metadata->>'initial_production_execution_id'=?", tenantID, execution.ModuleMeta, execution.TaskTypeScan, id, id).Order("id ASC").Take(&run).Error
	return &run, err
}

func productionBasicScanResult(run *execution.TaskExecution, id string) (*client.MetaProductionBasicScan, error) {
	result := &client.MetaProductionBasicScan{ProductionExecutionID: id, ScanExecutionID: run.ExecutionID, Status: run.Status}
	return result, result.Validate()
}
