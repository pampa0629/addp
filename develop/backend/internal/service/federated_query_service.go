package service

import (
	"context"
	"fmt"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	"github.com/addp/common/federatedquery"
	commonModels "github.com/addp/common/models"
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
) (*commonModels.EngineRuntimeDescriptor, plugin.FederatedQueryRuntimeProvider, error) {
	if s == nil || s.systemClient == nil || tenantID == 0 || runtimeEngineID == 0 {
		return nil, nil, fmt.Errorf("联邦查询服务未正确初始化")
	}
	descriptor, err := s.systemClient.WithTenantID(tenantID).GetEngineRuntimeDescriptor(ctx, runtimeEngineID)
	if err != nil {
		return nil, nil, fmt.Errorf("获取联邦查询 Runtime 失败: %w", err)
	}
	if !engineselection.IsAvailableForComputeEntrypoint(descriptor.AsEngine(), "query") || descriptor.RuntimeEndpoint == nil {
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
	return descriptor, provider, nil
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

func (s *FederatedQueryService) ExecuteQuery(
	ctx context.Context,
	tenantID, runtimeEngineID uint,
	executionID uuid.UUID,
	authorizationID int64,
	query string,
	timeout int,
	limit int,
	sourceEngineIDs []uint,
) (*FederatedQueryResult, error) {
	descriptor, provider, err := s.runtime(ctx, tenantID, runtimeEngineID)
	if err != nil {
		return nil, err
	}
	if executionID == uuid.Nil || authorizationID <= 0 || len(sourceEngineIDs) == 0 {
		return nil, fmt.Errorf("联邦查询 Execution Authorization 无效")
	}
	if s.protectionGate == nil {
		return nil, fmt.Errorf("Develop 联邦查询保护门禁未配置")
	}
	endProtection, err := s.protectionGate.BeginUnresolvedRead(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	defer endProtection()
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
	result, err := provider.ExecuteFederatedQuery(ctx, plugin.ConnectionInfo(descriptor.AsEngine().ConnectionInfo), plugin.FederatedQueryRequest{
		ExecutionID: executionID.String(), ExecutionAuthorizationID: fmt.Sprintf("%d", authorizationID),
		SourceEngineIDs: append([]uint(nil), sourceEngineIDs...), Query: query, Language: "sql",
		Options:           plugin.QueryOptions{Limit: limit, Timeout: time.Duration(timeout) * time.Second, ReadOnly: true},
		CallerAccessToken: callerToken,
	})
	if err != nil {
		return nil, err
	}
	rows := result.Rows
	if rows == nil {
		rows = []map[string]interface{}{}
	}
	return &FederatedQueryResult{
		Columns: result.Columns, Rows: rows, RowCount: len(rows), ExecutionTimeMs: time.Since(start).Milliseconds(),
	}, nil
}

func (s *FederatedQueryService) CandidateQueries(sources []DataSource) []federatedquery.Candidate {
	return federatedquery.Candidates(sources, 10)
}

func (s *FederatedQueryService) GetSources(ctx context.Context, tenantID, runtimeEngineID uint) ([]DataSource, error) {
	_, provider, err := s.runtime(ctx, tenantID, runtimeEngineID)
	if err != nil {
		return nil, err
	}
	return s.catalog.Sources(ctx, tenantID, runtimeEngineID, provider)
}
