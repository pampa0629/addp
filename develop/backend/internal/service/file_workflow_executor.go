package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	"github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
)

type privateFileCleanup struct {
	batch *fileWorkflowInputs
	state fileWorkflowInputCleanup
}

// The map owns only live batches created by this instance. It is not another
// queue, authorization cache or reconstruction of a persisted path.
type privateFileWorkflowExecutor struct {
	repository *repository.FileExecutionRepository
	instanceID string
	authority  fileSourceAuthority
	provider   plugin.ContentReadableProvider
	endpoint   func(context.Context, uint, uint) (plugin.ConnectionInfo, error)
	credential func(context.Context, uint) (string, error)
	mu         sync.Mutex
	pending    map[string]privateFileCleanup
}

func newPrivateFileWorkflowExecutor(repo *repository.FileExecutionRepository, workflow *WorkflowEngineService, instanceID string) *privateFileWorkflowExecutor {
	f := &privateFileWorkflowExecutor{repository: repo, instanceID: instanceID, pending: map[string]privateFileCleanup{}}
	if workflow == nil || workflow.systemService == nil {
		return f
	}
	f.authority = workflow.systemService
	f.credential = workflow.systemService.TenantServiceAccessToken
	if p, err := plugin.Get("nfs"); err == nil {
		f.provider, _ = p.(plugin.ContentReadableProvider)
	}
	f.endpoint = func(ctx context.Context, tenant, id uint) (plugin.ConnectionInfo, error) {
		descriptor, err := workflow.systemService.WithTenantID(tenant).GetEngineRuntimeDescriptor(ctx, id)
		if err != nil {
			return nil, err
		}
		engine := descriptor.AsEngine()
		if engine == nil || engine.ID != id || engine.EngineType != "geopython_workflow" || !engineselection.IsAvailableForComputeEntrypoint(engine, "workflow") {
			return nil, errFileWorkflowInputs
		}
		return plugin.ConnectionInfo(engine.ConnectionInfo), nil
	}
	return f
}

func (s *ExecutionService) PrivateFilesUnstopped(active map[string]struct{}) int {
	f := s.privateFiles
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for id, p := range f.pending {
		if _, running := active[id]; running {
			continue
		}
		if !p.state.SourcesStopped || !p.state.RuntimeStopped {
			count++
		}
	}
	return count
}

// A terminal execution does not terminate actual I/O. The original supervisor
// recovery round may observe/clean these batches, but cannot replay their work.
func (s *ExecutionService) RecoverPrivateFiles(ctx context.Context) error {
	f := s.privateFiles
	f.mu.Lock()
	pending := make(map[string]privateFileCleanup, len(f.pending))
	for id, p := range f.pending {
		pending[id] = p
	}
	f.mu.Unlock()
	var failures []error
	for id, p := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		b := p.batch
		var state fileWorkflowInputCleanup
		var err error
		if b.runtimeID != "" {
			var original *repository.PrivateFileRuntimeRecord
			original, err = f.repository.ReadPrivateRuntime(ctx, b.manifest.TenantID, b.manifest.ExecutionID)
			if err == nil {
				state, err = b.CleanupRecorded(ctx, f.instanceID, *original)
			}
		} else {
			var original *repository.PrivateInputDirectoryRecord
			original, err = f.repository.ReadPrivateInputDirectory(ctx, b.manifest.TenantID, b.manifest.ExecutionID)
			if err == nil {
				state, err = b.CleanupInputDirectory(ctx, f.instanceID, *original)
			}
		}
		f.mu.Lock()
		if state.LocalRemoved {
			delete(f.pending, id)
		} else if state.observed {
			f.pending[id] = privateFileCleanup{b, state}
		}
		f.mu.Unlock()
		if err != nil {
			failures = append(failures, errFileSourceCleanupPending)
		}
	}
	return errors.Join(failures...)
}

func (f *privateFileWorkflowExecutor) cleanup(b *fileWorkflowInputs) {
	if b == nil {
		return
	}
	// A cancelled business context cannot stand for completed physical cleanup.
	// The existing Runtime cleanup client bounds its own HTTP observation.
	state, _ := b.Cleanup(context.Background())
	if !state.LocalRemoved {
		f.mu.Lock()
		f.pending[b.manifest.ExecutionID] = privateFileCleanup{b, state}
		f.mu.Unlock()
	}
}

func (s *ExecutionService) executePrivateFileWorkflow(ctx context.Context, item *execution.TaskExecution, lease execution.Lease, facts *client.DevelopFileExecutionFacts, issued client.IssuedDevelopFileAuthorization, language string) error {
	f := s.privateFiles
	unavailable := func() error {
		return s.fail(ctx, item, lease, *item.StartedAt, "authorization_unavailable", fmt.Errorf("%s", commoni18n.ForLanguage(language, "develop.workflow.private_file_unavailable")))
	}
	// Publication/initial authorization are separate and remain closed. This
	// branch cannot let a registered save escape via generic connection expansion.
	if item.StartedAt == nil {
		return s.fail(ctx, item, lease, time.Time{}, "snapshot_invalid", fmt.Errorf("%s", commoni18n.ForLanguage(language, "develop.workflow.private_file_unavailable")))
	}
	if facts.Intent.Target != nil || f.authority == nil || f.provider == nil || f.endpoint == nil || f.credential == nil {
		return unavailable()
	}
	var config map[string]json.RawMessage
	var engineID uint
	if json.Unmarshal(facts.ConfigJSON, &config) != nil || json.Unmarshal(config["engine_id"], &engineID) != nil || engineID == 0 {
		return unavailable()
	}
	deadline := item.StartedAt.Add(time.Duration(facts.Intent.TimeoutSeconds) * time.Second)
	if issued.Authorization.ExpiresAt.Before(deadline) {
		deadline = issued.Authorization.ExpiresAt
	}
	runCtx, cancel := context.WithDeadline(execution.ContextWithLease(ctx, lease), deadline)
	defer cancel()
	connection, err := f.endpoint(runCtx, uint(item.TenantID), engineID)
	if err != nil {
		return unavailable()
	}
	b, err := prepareFileWorkflowInputs(runCtx, f.instanceID, f.authority, f.provider, facts, issued, lease, func(c context.Context, instance, dir string) error {
		return f.repository.RecordPrivateInputDirectory(c, lease, instance, dir)
	})
	defer f.cleanup(b)
	var status *plugin.WorkflowExecutionStatus
	if err == nil {
		err = b.Upload(runCtx, connection, uuid.NewString(), func(c context.Context) (string, error) { return f.credential(c, uint(item.TenantID)) }, func(c context.Context, record repository.PrivateFileRuntimeRecord) error {
			return f.repository.RecordPrivateRuntime(c, lease, record)
		})
	}
	if err == nil {
		_, err = b.Execute(runCtx, language)
	}
	for err == nil {
		err = b.live()
		if err != nil {
			break
		}
		callCtx, stop := b.bounded(runCtx)
		status, err = b.runtime.Execution(callCtx)
		stop()
		if err != nil || status.Status == "success" || status.Status == "failed" || status.Status == "error" {
			break
		}
		if status.Status != "pending" && status.Status != "running" {
			err = errFileWorkflowInputs
			break
		}
		timer := time.NewTimer(workflowRuntimeStatusPollInterval)
		select {
		case <-runCtx.Done():
			err = runCtx.Err()
		case <-b.ctx.Done():
			err = context.Cause(b.ctx)
		case <-timer.C:
		}
		timer.Stop()
	}
	// Do not turn late/withdrawn input into a successful business result.
	if err == nil {
		err = b.live()
	}
	result := commonModels.JSONMap{}
	message := ""
	if status != nil && err == nil {
		raw := map[string]interface{}{"status": status.Status, "error_code": status.ErrorCode}
		if status.Diagnostic != nil {
			data, _ := json.Marshal(status.Diagnostic)
			var diagnostic map[string]interface{}
			_ = json.Unmarshal(data, &diagnostic)
			raw["diagnostic"] = diagnostic
		}
		result["runtime_status"] = raw
		if status.Status == "success" && status.Result != nil {
			data, marshalErr := json.Marshal(status.Result)
			if marshalErr == nil {
				if preview, ok := workflowFinalResultForExecution(string(data)); ok {
					result["final_result"] = preview
				}
			}
		}
	}
	if err != nil || status == nil || status.Status != "success" {
		message = commoni18n.ForLanguage(language, "develop.workflow.private_file_failed")
	}
	var definition map[string]interface{}
	if b != nil {
		decoder := json.NewDecoder(bytes.NewBufferString(b.manifest.WorkflowJSON))
		decoder.UseNumber()
		_ = decoder.Decode(&definition)
	}
	// No outputs, produced-target scans, lineage publication or Grant are created
	// by this read-only branch. Existing diagnostic validation owns node binding.
	metadata := queryExecutionMetadata(result)
	for key, value := range item.Metadata {
		metadata[key] = value
	}
	terminal := execution.ExecutionStatusSuccess
	fields := terminalQueryFields(metadata, nil)
	if message != "" {
		terminal = execution.ExecutionStatusFailed
		suffix := "execution_failed"
		if errors.Is(err, context.DeadlineExceeded) {
			terminal = execution.ExecutionStatusTimeout
			suffix = "execution_timeout"
		}
		fields["error_details"] = executionFailureDetails(item.TaskType, suffix, message, result, definition)
	}
	if terminal == execution.ExecutionStatusSuccess {
		finalCtx, stop := b.bounded(runCtx)
		defer stop()
		return s.queue.CompleteWithLease(finalCtx, item, lease, terminal, time.Now().UTC(), fields)
	}
	return s.queue.CompleteWithLease(ctx, item, lease, terminal, time.Now().UTC(), fields)
}
