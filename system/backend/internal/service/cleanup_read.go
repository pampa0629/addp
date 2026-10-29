package service

import (
	"context"
	"database/sql"
	"errors"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CleanupReader is supplied only by authenticated HTTP composition. A bearer
// exists only for this synchronous request, and is never persisted or logged.
type CleanupReader struct {
	TenantID, UserID uint
	OwnerProtocol    bool
	Bearer           string
}

type cleanupRegistryReader struct{ db *gorm.DB }

func (r cleanupRegistryReader) ListActiveModules(ctx context.Context) ([]*client.ModuleInfo, error) {
	if r.db == nil {
		return nil, client.ErrExecutionOwnerUnavailable
	}
	modules, err := NewModuleRegistryService(repository.NewModuleRegistryRepository(r.db.WithContext(ctx))).ListActiveModules()
	if err != nil {
		return nil, err
	}
	out := make([]*client.ModuleInfo, 0, len(modules))
	for _, m := range modules {
		if m == nil {
			continue
		}
		target := &client.ModuleInfo{ModuleName: m.ModuleName, Enabled: m.Enabled}
		for _, i := range m.Instances {
			target.Instances = append(target.Instances, client.ModuleRuntimeInstanceInfo{Role: i.Role, Status: i.Status, LeaseExpiresAt: i.LeaseExpiresAt, ModuleURL: i.ModuleURL})
		}
		out = append(out, target)
	}
	return out, nil
}
func cleanupPublicTask(s *cleanupSnapshot) models.CleanupTask {
	in, p := s.Input, s.Parent
	return models.CleanupTask{TaskID: in.RequestID, Action: in.Action, TenantID: in.TenantID, CleanupMode: in.CleanupMode, TriggerType: in.TriggerType, CauseEvent: in.CauseEvent, Status: p.Status, ExpectedModules: in.ExpectedModules, Context: in.Context, ExecutionID: p.ExecutionID, RequestedBy: in.RequestedBy, CreatedAt: in.RequestedAt, StartedAt: p.StartedAt, CompletedAt: p.CompletedAt, TimeoutAt: in.Deadline, BasedOnScan: in.BasedOnScan}
}
func cleanupChildReadable(row execution.TaskExecution, scopes []execution.ReadScope) bool {
	for _, s := range scopes {
		if s.TenantID != row.TenantID || s.Module != row.Module || row.TriggeredBy == nil || int64(*row.TriggeredBy) != s.PrincipalID || row.SourceTaskID != nil {
			continue
		}
		for _, g := range s.Grants {
			if g.TaskType == execution.TaskTypeCleanupExecutor && g.OwnAdHoc {
				return true
			}
		}
	}
	return false
}
func (s *CleanupOrchestratorService) GetTaskStatus(ctx context.Context, id string, reader CleanupReader) (*models.TaskStatusResponse, error) {
	// No Owner HTTP or child reads until parent Tenant access is established.
	parsed, parseErr := uuid.Parse(id)
	if parseErr != nil || parsed == uuid.Nil || parsed.String() != id {
		return nil, commonapi.ErrBadRequest
	}
	if reader.TenantID == 0 || reader.UserID == 0 {
		return nil, commonapi.ErrForbidden
	}
	var record models.CleanupRequestRecord
	err := s.coordinator.db.WithContext(ctx).Where("request_id = ? AND tenant_id = ?", id, reader.TenantID).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, commonapi.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	input, err := record.Input()
	if err != nil {
		return nil, err
	}
	var scopes []execution.ReadScope
	if reader.OwnerProtocol {
		if s.reads == nil {
			return nil, client.ErrExecutionOwnerUnavailable
		}
		scopes, err = s.reads.Resolve(ctx, input.ExpectedModules, int(reader.TenantID), int64(reader.UserID), reader.Bearer)
		if err != nil {
			return nil, client.ErrExecutionOwnerUnavailable
		}
	}
	snapshot, err := s.coordinator.snapshot(ctx, id, reader.TenantID)
	if err != nil {
		return nil, err
	}
	if snapshot.Input.Digest != input.Digest {
		return nil, commonapi.ErrNotFound
	}
	result := &models.TaskStatusResponse{TaskID: id, Action: input.Action, Status: snapshot.Parent.Status, Task: cleanupPublicTask(snapshot), ObservedAt: snapshot.ObservedAt}
	cutoff, err := snapshot.Record.FrozenCutoff()
	if err != nil {
		return nil, err
	}
	if cutoff != nil {
		result.CutoffAt = &cutoff.ObservedAt
	}
	if !reader.OwnerProtocol {
		return result, nil
	}
	visible := []models.CleanupModuleExecution{}
	allowed := map[string]bool{}
	for _, c := range snapshot.Children {
		if !cleanupChildReadable(c, scopes) {
			continue
		}
		allowed[c.Module] = true
		entry := models.CleanupModuleExecution{Observation: execution.Observe(&c), CleanupAdmission: execution.ProjectCleanupAdmission(c, &snapshot.Parent, snapshot.ObservedAt)}
		if receipt, e := execution.ReadCleanupReceipt(c); e == nil {
			entry.Receipt = receipt
		}
		visible = append(visible, entry)
	}
	result.ModuleExecutions = &visible
	if cutoff != nil {
		receipts := []execution.CleanupReceipt{}
		for _, r := range cutoff.Receipts {
			if allowed[r.Module] {
				receipts = append(receipts, r)
			}
		}
		if len(receipts) > 0 {
			summary, impact, digest, err := cleanupReceiptSummary(receipts)
			if err != nil {
				return nil, err
			}
			result.Summary = &models.TaskSummary{CleanupResultSummary: summary, Impact: impact, ImpactDigest: digest}
		}
	}
	return result, nil
}

func (s *CleanupOrchestratorService) GetTaskHistory(ctx context.Context, tenant, user uint, page, pageSize int) ([]models.CleanupTask, int64, error) {
	if tenant == 0 || user == 0 {
		return nil, 0, commonapi.ErrForbidden
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, 0, commonapi.ErrBadRequest
	}
	out := []models.CleanupTask{}
	var total int64
	err := s.coordinator.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&models.CleanupRequestRecord{}).Where("tenant_id = ?", tenant)
		if err := q.Count(&total).Error; err != nil {
			return err
		}
		var records []models.CleanupRequestRecord
		if err := q.Order("created_at DESC, request_id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&records).Error; err != nil {
			return err
		}
		for _, r := range records {
			in, err := r.Input()
			if err != nil {
				return err
			}
			var p execution.TaskExecution
			if err := tx.Where("execution_id = ? AND tenant_id = ?", r.ParentExecutionID, tenant).First(&p).Error; err != nil {
				return err
			}
			bound, err := execution.CleanupRequestFromExecution(p)
			if err != nil || bound.Digest != in.Digest {
				return commonapi.ErrConflict
			}
			out = append(out, cleanupPublicTask(&cleanupSnapshot{Record: r, Input: in, Parent: p}))
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return out, total, err
}
