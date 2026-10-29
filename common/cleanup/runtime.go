package cleanup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Runner struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	DB     *gorm.DB
	Owner  string
	System *client.SystemServiceClient
	Log    *slog.Logger
	// Handle owns domain behavior and must atomically commit its private result,
	// receipt and terminal. PG mutations use Run.Effect; HTTP never holds C.
	Handle func(context.Context, *Run) error
	// Lost is composed after C and before failure, never after P. Graph uses it
	// to retain its admission/operation block. It must not dispatch effects.
	Lost func(context.Context, *gorm.DB, execution.TaskExecution) error
}

func (r *Runner) Start(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return fmt.Errorf("cleanup Owner runtime already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() { defer close(r.done); _ = r.Serve(runCtx) }()
	return nil
}

func (r *Runner) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

type Run struct {
	Execution execution.TaskExecution
	Input     events.CleanupRequest
	Lease     execution.Lease
	runner    *Runner
}

func (r *Runner) validate() error {
	if r == nil || r.DB == nil || r.System == nil || r.Handle == nil || !events.CleanupModuleValid(r.Owner) {
		return fmt.Errorf("complete cleanup Owner runtime is required")
	}
	return nil
}

// Serve occupies one bounded slot per Owner process. No message spawns a detached
// runner; shutdown cancels the current operation and waits for it to return.
func (r *Runner) Serve(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	worker := r.Owner + "-cleanup/" + uuid.NewString()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := r.step(ctx, worker); err != nil && ctx.Err() == nil && r.Log != nil {
			r.Log.Warn("cleanup runtime step failed", "owner", r.Owner, "error_code", "cleanup_runtime_step_failed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (r *Runner) step(ctx context.Context, worker string) error {
	if err := r.recoverExpired(ctx); err != nil {
		return err
	}
	var candidates []execution.TaskExecution
	query := r.DB.WithContext(ctx).Where("module = ? AND task_type = ? AND source = ? AND status = ? AND execution_boundary = ?", r.Owner, execution.TaskTypeCleanupExecutor, execution.ModuleSystem, execution.ExecutionStatusPending, execution.ExecutionBoundaryBounded)
	if r.DB.Dialector.Name() == "postgres" {
		query = query.Where("execution_config ? 'cleanup_request'")
	}
	if err := query.Order("created_at, id").Limit(32).Find(&candidates).Error; err != nil {
		return err
	}
	for _, candidate := range candidates {
		input, err := execution.CleanupRequestFromExecution(candidate)
		if err != nil {
			continue
		}
		access, accessErr := r.authorize(ctx, candidate, "claim", execution.Lease{})
		identity := execution.PendingIdentity{ExecutionID: candidate.ExecutionID, TenantID: candidate.TenantID, Module: r.Owner, TaskType: execution.TaskTypeCleanupExecutor, Source: execution.ModuleSystem}
		item, lease, err := execution.ClaimCleanupChild(ctx, r.DB, identity, execution.ClaimOptions{Module: r.Owner, TaskType: execution.TaskTypeCleanupExecutor, Source: execution.ModuleSystem, WorkerID: worker, LeaseDuration: 2 * time.Minute, RequireAuthorization: true}, func(tx *gorm.DB, child execution.TaskExecution, _ events.CleanupRequest) error {
			if accessErr != nil {
				return accessErr
			}
			return freshAccess(tx, child, access, "claim")
		})
		// Common performs permanent closure rejection before invoking prepare.
		// Unavailable/revoked authority cannot claim, even while P is still open.
		if err != nil {
			if accessErr != nil {
				if noteErr := execution.NoteCleanupAuthorizationFailure(ctx, r.DB, identity, authorizationFailureCode(accessErr)); noteErr != nil {
					return noteErr
				}
				continue
			}
			if errors.Is(err, commonapi.ErrConflict) {
				continue
			}
			return err
		}
		if lease == nil {
			continue
		}
		return r.execute(ctx, &Run{Execution: *item, Input: input, Lease: *lease, runner: r})
	}
	return nil
}

func (r *Runner) authorize(ctx context.Context, item execution.TaskExecution, stage string, lease execution.Lease) (*client.CleanupAccess, error) {
	if item.ExecutionAuthorizationID == nil {
		return nil, &authorizationFailure{code: "cleanup_authorization_denied"}
	}
	scope, err := execution.CleanupAuthorizationFor(item)
	if err != nil {
		return nil, err
	}
	request := client.CleanupAccessRequest{ExecutionID: item.ExecutionID, Stage: stage, Scope: scope}
	if stage == "effect" {
		request.Attempt, request.LeaseToken = lease.Attempt, lease.Token
	}
	access, err := r.System.WithTenantID(uint(item.TenantID)).GetCleanupAccess(ctx, strconv.FormatInt(*item.ExecutionAuthorizationID, 10), request)
	if err != nil {
		return nil, &authorizationFailure{code: authorizationFailureCode(err)}
	}
	return access, nil
}

func freshAccess(tx *gorm.DB, item execution.TaskExecution, access *client.CleanupAccess, stage string) error {
	scope, err := execution.CleanupAuthorizationFor(item)
	if err != nil {
		return err
	}
	if access == nil || item.ExecutionAuthorizationID == nil || access.AuthorizationID != strconv.FormatInt(*item.ExecutionAuthorizationID, 10) || access.ExecutionID != item.ExecutionID || access.TenantID != strconv.Itoa(item.TenantID) || access.Stage != stage || access.Scope != scope || access.Attempt != item.Attempt {
		return &authorizationFailure{code: "cleanup_authorization_denied"}
	}
	now, err := execution.DatabaseNow(tx)
	if err != nil {
		return err
	}
	if !now.Before(access.ExpiresAt) {
		return &authorizationFailure{code: "cleanup_authorization_denied"}
	}
	return nil
}

func (r *Runner) execute(ctx context.Context, run *Run) error {
	if run.Execution.AuthorizationExpiresAt == nil {
		return fmt.Errorf("cleanup authority expiry missing")
	}
	language, err := execution.DisplayLanguage(&run.Execution)
	if err != nil {
		return err
	}
	ctx, err = commoni18n.WithLanguage(execution.ContextWithLease(ctx, run.Lease), language)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithDeadline(ctx, *run.Execution.AuthorizationExpiresAt)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-heartbeat.C:
				if err := execution.RenewLease(runCtx, r.DB, run.Lease, 2*time.Minute); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	err = r.Handle(runCtx, run)
	cancel()
	<-done
	checkCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	terminal, checkErr := execution.AttemptIsTerminal(checkCtx, r.DB, run.Lease)
	if checkErr != nil {
		return errors.Join(err, checkErr)
	}
	if terminal {
		return err
	}
	// Unexpected failure has no invented receipt or professional statistics.
	// Loss convergence cannot dispatch another external operation or replay work.
	closeErr := r.DB.WithContext(checkCtx).Transaction(func(tx *gorm.DB) error {
		item, e := execution.LockActiveLease(checkCtx, tx, run.Lease)
		if e != nil {
			return e
		}
		if r.Lost != nil {
			if e := r.Lost(checkCtx, tx, *item); e != nil {
				return e
			}
		}
		return execution.CompleteWithLease(checkCtx, tx, run.Lease, execution.ExecutionStatusFailed, map[string]interface{}{"error_details": models.JSONMap{"code": ownerFailureCode(err)}})
	})
	return errors.Join(err, closeErr)
}

func (r *Runner) recoverExpired(ctx context.Context) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := execution.FindExpiredForUpdate(ctx, tx, execution.ExpiredOptions{Module: r.Owner, TaskType: execution.TaskTypeCleanupExecutor, Source: execution.ModuleSystem, Limit: 16})
		if err != nil {
			return err
		}
		for _, item := range items {
			if _, err := execution.CleanupRequestFromExecution(item); err != nil {
				return err
			}
			lease, err := execution.LeaseFromExecution(item)
			if err != nil {
				return err
			}
			if r.Lost != nil {
				if err := r.Lost(ctx, tx, item); err != nil {
					return err
				}
			}
			if err := execution.FailExpired(ctx, tx, lease, map[string]interface{}{"error_details": models.JSONMap{"code": "cleanup_lease_expired"}}); err != nil {
				return err
			}
		}
		return nil
	})
}

// Effect rechecks current original User authority outside the short transaction,
// then takes C before Owner locks and rechecks expiry after domain work. The
// callback must contain PG operations only, including the final Complete call.
func (r *Run) Effect(ctx context.Context, write func(*gorm.DB) error) error {
	access, err := r.runner.authorize(ctx, r.Execution, "effect", r.Lease)
	if err != nil {
		return err
	}
	return r.effectWithAccess(ctx, access, write)
}

// PrepareExternal authorizes and durably registers one exact Owner operation.
// prepare contains PG only and must reject replay. The returned context bounds
// the subsequent lock-free provider call by this decision, the parent context,
// and the execution authorization. Cancellation is not proof of external stop.
// Unknown results retain Owner isolation; this helper never retries or finishes C.
func (r *Run) PrepareExternal(ctx context.Context, prepare func(*gorm.DB) error) (context.Context, context.CancelFunc, error) {
	access, err := r.runner.authorize(ctx, r.Execution, "effect", r.Lease)
	if err != nil {
		return nil, nil, err
	}
	if err := r.effectWithAccess(ctx, access, prepare); err != nil {
		return nil, nil, err
	}
	deadline := access.ExpiresAt
	if r.Execution.AuthorizationExpiresAt == nil {
		return nil, nil, fmt.Errorf("cleanup authority expiry missing")
	}
	if r.Execution.AuthorizationExpiresAt.Before(deadline) {
		deadline = *r.Execution.AuthorizationExpiresAt
	}
	effectCtx, cancel := context.WithDeadline(ctx, deadline)
	if err := effectCtx.Err(); err != nil {
		cancel()
		return nil, nil, err
	}
	return effectCtx, cancel, nil
}

func (r *Run) effectWithAccess(ctx context.Context, access *client.CleanupAccess, write func(*gorm.DB) error) error {
	return r.runner.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := execution.LockActiveLease(ctx, tx, r.Lease)
		if err != nil {
			return err
		}
		if err := freshAccess(tx, *item, access, "effect"); err != nil {
			return err
		}
		if err := write(tx); err != nil {
			return err
		}
		// A callback may have atomically completed C. Both paths recheck the
		// authority decision's deadline; completion itself guarded the lease.
		if err := freshAccess(tx, *item, access, "effect"); err != nil {
			return err
		}
		terminal, err := execution.AttemptIsTerminal(ctx, tx, r.Lease)
		if err != nil {
			return err
		}
		if terminal {
			return nil
		}
		_, err = execution.LockActiveLease(ctx, tx, r.Lease)
		return err
	})
}

func (r *Run) Complete(ctx context.Context, result events.CleanupResultData, reason string, statistics map[string]int64) error {
	receipt, err := execution.NewCleanupReceipt(r.Execution, result, reason)
	if err != nil {
		return err
	}
	return r.Effect(ctx, func(tx *gorm.DB) error { return Complete(ctx, tx, r.Lease, receipt, statistics) })
}

// Closed diagnostics intentionally do not retain upstream bodies or credentials.
type authorizationFailure struct{ code string }

func (e *authorizationFailure) Error() string { return e.code }
func authorizationFailureCode(err error) string {
	var failure *authorizationFailure
	if errors.As(err, &failure) {
		return failure.code
	}
	if status, ok := client.SystemAPIStatusCode(err); ok && (status == http.StatusForbidden || status == http.StatusUnauthorized) {
		return "cleanup_authorization_denied"
	}
	return "cleanup_authorization_unavailable"
}
func ownerFailureCode(err error) string {
	var failure *authorizationFailure
	if errors.As(err, &failure) {
		return failure.code
	}
	return "cleanup_owner_stopped_without_receipt"
}
