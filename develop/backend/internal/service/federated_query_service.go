package service

import (
	"context"
	"fmt"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	"github.com/addp/common/federatedquery"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/protection"
	"github.com/google/uuid"
)

type FederatedQueryService struct {
	systemClient   *commonClient.SystemServiceClient
	catalog        *federatedquery.Catalog
	executeQuery   func(context.Context, uint, uint, uuid.UUID, int64, string, int, []uint) (*FederatedQueryResult, error)
	protectionGate interface {
		BeginUnresolvedRead(context.Context, uint) (func(), error)
	}
}

type FederatedQueryResult struct {
	Columns         []string                 `json:"columns"`
	Fields          []datatype.FieldInfo     `json:"fields"`
	Rows            []map[string]interface{} `json:"rows"`
	RowCount        int                      `json:"row_count"`
	ExecutionTimeMs int64                    `json:"execution_time_ms"`
}

func (s *FederatedQueryService) SetProtectionGate(gate interface {
	BeginUnresolvedRead(context.Context, uint) (func(), error)
}) {
	s.protectionGate = gate
}

type DataSource = federatedquery.Source
type TableRef = federatedquery.TableRef

func NewFederatedQueryService(systemClient *commonClient.SystemServiceClient, metaClient *commonClient.MetaClient) *FederatedQueryService {
	return &FederatedQueryService{
		systemClient: systemClient,
		catalog:      federatedquery.NewCatalog(systemClient, metaClient),
	}
}

func (s *FederatedQueryService) runtime(
	ctx context.Context, tenantID, runtimeEngineID uint,
) (*federatedquery.ExecutionBinding, plugin.FederatedQueryRuntimeProvider, error) {
	if s == nil {
		return nil, nil, fmt.Errorf("联邦查询服务未正确初始化")
	}
	return resolveFederatedRuntime(ctx, s.systemClient, tenantID, runtimeEngineID)
}

func resolveFederatedRuntime(ctx context.Context, systemClient *commonClient.SystemServiceClient, tenantID, runtimeEngineID uint) (*federatedquery.ExecutionBinding, plugin.FederatedQueryRuntimeProvider, error) {
	if systemClient == nil || tenantID == 0 || runtimeEngineID == 0 {
		return nil, nil, fmt.Errorf("联邦查询服务未正确初始化")
	}
	descriptor, err := systemClient.WithTenantID(tenantID).GetEngineRuntimeDescriptor(ctx, runtimeEngineID)
	if err != nil {
		return nil, nil, fmt.Errorf("获取联邦查询 Runtime 失败: %w", err)
	}
	if !engineselection.IsAvailableForComputeEntrypoint(descriptor.AsEngine(), "query") {
		return nil, nil, fmt.Errorf("联邦查询 Runtime 不可用")
	}
	enginePlugin, err := plugin.Get(descriptor.EngineType)
	if err != nil {
		return nil, nil, err
	}
	provider, ok := enginePlugin.(plugin.FederatedQueryRuntimeProvider)
	if !ok {
		return nil, nil, fmt.Errorf("引擎 %s 不支持联邦查询", descriptor.EngineType)
	}
	var candidates []commonModels.EngineRuntimeDescriptor
	caps, err := engineselection.ParseCapabilities(descriptor.Capabilities)
	if err != nil || caps == nil || caps.Compute == nil || caps.Compute.Query == nil || caps.Compute.Query.Federation == nil {
		return nil, nil, fmt.Errorf("联邦查询目标能力无效")
	}
	if caps.Compute.Query.Federation.RuntimeEngineType != "" {
		candidates, err = systemClient.WithTenantID(tenantID).ListEngineRuntimeDescriptors(ctx)
		if err != nil {
			return nil, nil, err
		}
	}
	binding, err := federatedquery.ResolveExecutionBinding(descriptor, candidates)
	if err != nil {
		return nil, nil, err
	}
	return binding, provider, nil
}

func (s *FederatedQueryService) IsRuntime(ctx context.Context, tenantID, engineID uint) bool {
	_, _, err := s.runtime(ctx, tenantID, engineID)
	return err == nil
}

func (s *FederatedQueryService) ReferencedEngineIDs(
	ctx context.Context, tenantID, runtimeEngineID uint, query string,
) ([]uint, error) {
	_, provider, err := s.runtime(ctx, tenantID, runtimeEngineID)
	if err != nil {
		return nil, err
	}
	return s.referencedEngineIDs(ctx, tenantID, runtimeEngineID, query, provider)
}

func (s *FederatedQueryService) referencedEngineIDs(ctx context.Context, tenantID, runtimeEngineID uint, query string, provider plugin.FederatedQueryRuntimeProvider) ([]uint, error) {
	descriptors, err := s.systemClient.WithTenantID(tenantID).ListEngineRuntimeDescriptors(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取源引擎列表失败: %w", err)
	}
	candidates := make([]plugin.FederatedQuerySource, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.ID == runtimeEngineID || !engineselection.IsAvailableStorageEngine(descriptor.AsEngine()) {
			continue
		}
		candidates = append(candidates, plugin.FederatedQuerySource{
			ID: descriptor.ID, Name: descriptor.Name, EngineType: descriptor.EngineType,
			LifecycleState: descriptor.LifecycleState,
		})
	}
	ids := provider.ResolveSourceEngineIDs(query, candidates)
	if len(ids) == 0 {
		return nil, fmt.Errorf("联邦查询必须引用至少一个当前可用的源引擎")
	}
	return ids, nil
}

// FreezeBinding resolves identities before enqueueing, without storing connections.
func (s *FederatedQueryService) FreezeBinding(ctx context.Context, tenantID, selectedID uint, query string) (*models.FederatedQueryBinding, error) {
	binding, provider, err := s.runtime(ctx, tenantID, selectedID)
	if err != nil {
		return nil, err
	}
	sources, err := s.referencedEngineIDs(ctx, tenantID, selectedID, query, provider)
	if err != nil {
		return nil, err
	}
	frozen := &models.FederatedQueryBinding{SchemaVersion: federatedBindingSchema, SelectedEngineID: selectedID, RuntimeEngineID: binding.Runtime.ID, Audience: binding.Audience, SourceEngineIDs: append([]uint(nil), sources...)}
	if err := validateFederatedBinding(frozen, selectedID); err != nil {
		return nil, federatedBindingError(ctx)
	}
	return frozen, nil
}

// Revalidate only the frozen target and actor; never discover a replacement.
func (s *FederatedQueryService) frozenRuntime(ctx context.Context, tenantID uint, frozen *models.FederatedQueryBinding) (*federatedquery.ExecutionBinding, plugin.FederatedQueryRuntimeProvider, error) {
	if s == nil || s.systemClient == nil || tenantID == 0 {
		return nil, nil, federatedBindingError(ctx)
	}
	if frozen == nil {
		return nil, nil, federatedBindingError(ctx)
	}
	if err := validateFederatedBinding(frozen, frozen.SelectedEngineID); err != nil {
		return nil, nil, federatedBindingError(ctx)
	}
	target, err := s.systemClient.WithTenantID(tenantID).GetEngineRuntimeDescriptor(ctx, frozen.SelectedEngineID)
	if err != nil {
		return nil, nil, err
	}
	if target.ID != frozen.SelectedEngineID {
		return nil, nil, federatedBindingError(ctx)
	}
	var candidates []commonModels.EngineRuntimeDescriptor
	if frozen.RuntimeEngineID != frozen.SelectedEngineID {
		actor, err := s.systemClient.WithTenantID(tenantID).GetEngineRuntimeDescriptor(ctx, frozen.RuntimeEngineID)
		if err != nil {
			return nil, nil, err
		}
		if actor.ID != frozen.RuntimeEngineID {
			return nil, nil, federatedBindingError(ctx)
		}
		candidates = append(candidates, *actor)
	}
	binding, err := federatedquery.ResolveExecutionBinding(target, candidates)
	if err != nil {
		return nil, nil, err
	}
	if binding.Runtime.ID != frozen.RuntimeEngineID || binding.Audience != frozen.Audience {
		return nil, nil, federatedBindingError(ctx)
	}
	enginePlugin, err := plugin.Get(target.EngineType)
	if err != nil {
		return nil, nil, err
	}
	provider, ok := enginePlugin.(plugin.FederatedQueryRuntimeProvider)
	if !ok {
		return nil, nil, federatedBindingError(ctx)
	}
	return binding, provider, nil
}

func (s *FederatedQueryService) ExecuteQuery(
	ctx context.Context,
	tenantID uint,
	frozen *models.FederatedQueryBinding,
	executionID uuid.UUID,
	authorizationID int64,
	query string,
	timeout int,
	limit int,
) (*FederatedQueryResult, error) {
	binding, provider, err := s.frozenRuntime(ctx, tenantID, frozen)
	if err != nil {
		return nil, err
	}
	if executionID == uuid.Nil || authorizationID <= 0 {
		return nil, fmt.Errorf("联邦查询 Execution Authorization 无效")
	}
	for _, sourceID := range frozen.SourceEngineIDs {
		if binding.TargetEngineID != 0 && sourceID == binding.TargetEngineID {
			return nil, fmt.Errorf("计算目标不能作为联邦数据源")
		}
	}
	if s.protectionGate == nil {
		return nil, fmt.Errorf("Develop 联邦查询保护门禁未配置")
	}
	if timeout <= 0 {
		timeout = 30
	}
	if limit <= 0 {
		limit = 1
	}
	callerToken, err := s.systemClient.TenantServiceAccessToken(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("获取 Develop Runtime 凭据失败: %w", err)
	}
	start := time.Now()
	req := plugin.FederatedQueryRequest{
		TargetEngineID: binding.TargetEngineID,
		ExecutionID:    executionID.String(), ExecutionAuthorizationID: fmt.Sprintf("%d", authorizationID),
		SourceEngineIDs: append([]uint(nil), frozen.SourceEngineIDs...), Query: query, Language: "sql",
		Options:           plugin.QueryOptions{Limit: limit, Timeout: time.Duration(timeout) * time.Second, ReadOnly: true},
		CallerAccessToken: callerToken,
	}
	var result *plugin.QueryResult
	if preparer, ok := provider.(plugin.PreparedFederatedQueryProvider); ok {
		gate, ok := s.protectionGate.(interface {
			BeginFederatedQuery(context.Context, uint, func(context.Context) (plugin.PreparedFederatedQuery, error)) (plugin.PreparedFederatedQuery, func(), error)
		})
		if !ok {
			return nil, fmt.Errorf("%w: prepared federation gate unavailable", protection.ErrRequired)
		}
		prepared, finish, prepareErr := gate.BeginFederatedQuery(ctx, tenantID, func(ctx context.Context) (plugin.PreparedFederatedQuery, error) {
			return preparer.PrepareFederatedQuery(ctx, plugin.ConnectionInfo(binding.Runtime.AsEngine().ConnectionInfo), req)
		})
		if prepareErr != nil {
			return nil, prepareErr
		}
		defer finish()
		result, err = prepared.Execute(ctx)
	} else {
		finish, gateErr := s.protectionGate.BeginUnresolvedRead(ctx, tenantID)
		if gateErr != nil {
			return nil, gateErr
		}
		defer finish()
		result, err = provider.ExecuteFederatedQuery(ctx, plugin.ConnectionInfo(binding.Runtime.AsEngine().ConnectionInfo), req)
	}
	if err != nil {
		return nil, err
	}
	rows := result.Rows
	if rows == nil {
		rows = []map[string]interface{}{}
	}
	return &FederatedQueryResult{
		Columns: result.Columns, Fields: result.Fields, Rows: rows, RowCount: len(rows), ExecutionTimeMs: time.Since(start).Milliseconds(),
	}, nil
}

func (s *FederatedQueryService) CandidateQueries(ctx context.Context, tenantID, targetID uint, sources []DataSource) ([]federatedquery.Candidate, error) {
	binding, _, err := s.runtime(ctx, tenantID, targetID)
	if err != nil {
		return nil, err
	}
	caps, err := engineselection.ParseCapabilities(binding.Target.Capabilities)
	if err != nil || caps == nil || caps.Compute == nil || caps.Compute.Query == nil {
		return nil, federatedBindingError(ctx)
	}
	return federatedquery.Candidates(sources, 10, caps.Compute.Query.IdentifierQuotes["sql"])
}

func (s *FederatedQueryService) GetSources(ctx context.Context, tenantID, runtimeEngineID uint) ([]DataSource, error) {
	_, provider, err := s.runtime(ctx, tenantID, runtimeEngineID)
	if err != nil {
		return nil, err
	}
	return s.catalog.Sources(ctx, tenantID, runtimeEngineID, provider)
}
