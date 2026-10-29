package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/google/uuid"
)

type BuildSupervisorOptions struct {
	LeaseDuration time.Duration
	Heartbeat     time.Duration
	Poll          time.Duration
}

func (o BuildSupervisorOptions) Validate() error {
	if o.LeaseDuration <= 0 || o.Heartbeat <= 0 || o.Heartbeat >= o.LeaseDuration || o.Poll <= 0 {
		return fmt.Errorf("graph supervisor requires positive poll and heartbeat shorter than lease")
	}
	return nil
}

type buildInputKey struct{}
type effectScopeKey struct{}
type effectScope struct {
	repo                            *repository.BuildRepository
	graphID, tenantID, reviewItemID uint
	commandID                       string
	mu                              sync.Mutex
	targetDigest                    string
	targetDescriptor                []byte
}

var commandRuntimeID = "graph-backend/" + uuid.NewString()

var errCopilotUnauthorized = errors.New("copilot authorization rejected before execution")

func buildInputFromContext(ctx context.Context) (*models.BuildInput, bool) {
	v, ok := ctx.Value(buildInputKey{}).(*models.BuildInput)
	return v, ok && v != nil
}

func effectScopeFromContext(ctx context.Context) *effectScope {
	v, _ := ctx.Value(effectScopeKey{}).(*effectScope)
	return v
}

func (s *GraphDataService) mutationContext(ctx context.Context, graphID, tenantID uint) context.Context {
	if effectScopeFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, effectScopeKey{}, &effectScope{repo: repository.NewBuildRepository(s.graphRepo.DB()), graphID: graphID, tenantID: tenantID, commandID: uuid.NewString()})
}

func bindEffectTarget(ctx context.Context, kg *models.KnowledgeGraph, engineVersion int64) error {
	scope := effectScopeFromContext(ctx)
	if scope == nil {
		return nil
	}
	if scope.graphID != kg.ID || scope.tenantID != kg.TenantID {
		return ErrGraphTargetInvalid
	}
	b, err := json.Marshal(models.GraphOperationTarget{EngineID: kg.EngineID, EngineVersion: engineVersion, CatalogPath: kg.CatalogPath, IndexManagement: kg.IndexManagement})
	if err != nil {
		return err
	}
	hash := sha256.Sum256(b)
	digest := hex.EncodeToString(hash[:])
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if scope.targetDigest != "" && scope.targetDigest != digest {
		return ErrGraphTargetInvalid
	}
	scope.targetDigest = digest
	scope.targetDescriptor = append([]byte(nil), b...)
	return nil
}

// recordExternalCall is installed only on explicit mutating command contexts.
// The ledger commits before the actual call; ambiguous errors remain blocking.
func recordExternalCall[T any](ctx context.Context, kind string, request []byte, call func() (T, error)) (T, error) {
	var zero T
	scope := effectScopeFromContext(ctx)
	if scope == nil {
		return call()
	}
	scope.mu.Lock()
	digest := scope.targetDigest
	descriptor := append([]byte(nil), scope.targetDescriptor...)
	scope.mu.Unlock()
	if digest == "" {
		return zero, fmt.Errorf("frozen external target is required")
	}
	requestHash := sha256.Sum256(request)
	op := models.GraphOperation{RunnerID: commandRuntimeID, RequestDigest: hex.EncodeToString(requestHash[:]), TargetDescriptor: string(descriptor), ID: uuid.NewString(), TenantID: scope.tenantID, GraphID: scope.graphID, Kind: kind, TargetDigest: digest, ReviewItemID: scope.reviewItemID, CommandID: scope.commandID}
	lease, leased := execution.LeaseFromContext(ctx)
	var err error
	if leased {
		op.ExecutionID, op.Attempt = lease.ExecutionID, lease.Attempt
		err = scope.repo.BeginBuildOperation(ctx, &op)
	} else {
		err = scope.repo.BeginCommandOperation(ctx, &op)
	}
	if err != nil {
		return zero, err
	}
	result, callErr := call()
	encoded, encodeErr := json.Marshal(result)
	// A complete 401 from the fixed authenticated Copilot endpoint precedes
	// extraction. This does not classify native engine or transport failures.
	known := (callErr == nil || errors.Is(callErr, errCopilotUnauthorized)) && encodeErr == nil
	evidence := ""
	if known {
		hash := sha256.Sum256(encoded)
		evidence = hex.EncodeToString(hash[:])
	}
	settleCtx := context.WithoutCancel(ctx)
	if leased {
		err = scope.repo.SettleBuildOperation(settleCtx, op, evidence, known)
	} else {
		err = scope.repo.SettleCommandOperation(settleCtx, op, evidence, known)
	}
	if err != nil && leased && evidence != "" {
		// Retaining a late local return cannot grant the old attempt write rights.
		if lateErr := scope.repo.AppendLateEvidence(settleCtx, op, evidence); lateErr != nil {
			log.Printf("retain graph late operation evidence: %v", lateErr)
		}
	}
	if err := errors.Join(callErr, encodeErr, err); err != nil {
		return zero, err
	}
	return result, nil
}

func (s *BuildService) StartSupervisor(ctx context.Context, options BuildSupervisorOptions) error {
	if err := options.Validate(); err != nil {
		return err
	}
	s.supervisorMu.Lock()
	defer s.supervisorMu.Unlock()
	if s.supervisorCancel != nil {
		return fmt.Errorf("graph build supervisor already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.supervisorCancel = cancel
	s.supervisorDone = make(chan struct{})
	owner := "graph-backend/" + uuid.NewString()
	go func() {
		defer close(s.supervisorDone)
		ticker := time.NewTicker(options.Poll)
		defer ticker.Stop()
		for {
			if runCtx.Err() != nil {
				return
			}
			if err := s.superviseOnce(runCtx, owner, options); err != nil && runCtx.Err() == nil {
				log.Printf("graph build supervisor: %v", err)
			}
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

func (s *BuildService) StopSupervisor() {
	s.supervisorMu.Lock()
	cancel, done := s.supervisorCancel, s.supervisorDone
	s.supervisorMu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (s *BuildService) superviseOnce(ctx context.Context, owner string, options BuildSupervisorOptions) error {
	if err := options.Validate(); err != nil {
		return err
	}
	if err := s.buildRepo.FailExpiredBuilds(ctx); err != nil {
		return err
	}
	candidates, err := s.buildRepo.PendingBuilds(ctx, 32)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}

	var e *execution.TaskExecution
	var lease *execution.Lease
	var task *models.BuildTask
	var control models.BuildExecutionControl
	for _, candidate := range candidates {
		e, lease, task, control, err = s.buildRepo.ClaimBuild(ctx, candidate, owner, options.LeaseDuration)
		if errors.Is(err, commonapi.ErrConflict) {
			continue
		}
		if err != nil {
			return err
		}
		if e != nil {
			break
		}
	}
	if e == nil {
		return nil
	}
	input, err := repository.DecodeBuildInput(*e, control, *task)
	if err != nil {
		return err
	}
	// Execution uses frozen values; task carries only this attempt's runtime facts.
	task.ConfidenceThreshold, task.ChunkSize, task.ChunkOverlap, task.DocContextSize = input.Task.ConfidenceThreshold, input.Task.ChunkSize, input.Task.ChunkOverlap, input.Task.DocContextSize
	lang, err := execution.DisplayLanguage(e)
	if err != nil {
		return err
	}
	runtimeCtx, err := commoni18n.WithLanguage(ctx, lang)
	if err != nil {
		return err
	}
	leaseCtx := execution.ContextWithLease(runtimeCtx, *lease)
	leaseCtx = context.WithValue(leaseCtx, buildInputKey{}, input)
	scope := &effectScope{repo: s.buildRepo, graphID: task.GraphID, tenantID: task.TenantID}
	leaseCtx = context.WithValue(leaseCtx, effectScopeKey{}, scope)
	kg := models.KnowledgeGraph{ID: input.GraphID, TenantID: input.TenantID, EngineID: input.EngineID, CatalogPath: input.CatalogPath, IndexManagement: input.IndexManagement}
	if err := bindEffectTarget(leaseCtx, &kg, input.EngineVersion); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(leaseCtx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		poll := time.NewTicker(options.Poll)
		defer poll.Stop()
		heartbeat := time.NewTicker(options.Heartbeat)
		defer heartbeat.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-poll.C:
				requested, err := s.buildRepo.CancellationRequested(runCtx, e.ExecutionID, e.TenantID)
				if err != nil || requested {
					cancel()
					return
				}
			case <-heartbeat.C:
				if err := execution.RenewLease(runCtx, s.buildRepo.DB(), *lease, options.LeaseDuration); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	materials := make([]models.BuildMaterial, len(input.Materials))
	for i, m := range input.Materials {
		materials[i] = m.Material
	}
	s.executeTask(runCtx, task, materials, int(task.TenantID), e.ExecutionID)
	cancel()
	<-done
	return nil
}

func frozenGraph(ctx context.Context, graphID, tenantID uint) (*models.KnowledgeGraph, error) {
	input, ok := buildInputFromContext(ctx)
	if !ok {
		return nil, nil
	}
	if input.GraphID != graphID || input.TenantID != tenantID {
		return nil, ErrGraphTargetInvalid
	}
	return &models.KnowledgeGraph{ID: graphID, TenantID: tenantID, OntologyID: input.Ontology.ID, EngineID: input.EngineID, CatalogPath: input.CatalogPath, IndexManagement: input.IndexManagement}, nil
}

func sameCatalogTarget(a, b interface{}) bool { return reflect.DeepEqual(a, b) }
