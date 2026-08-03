package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/federatedquery"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
)

var ErrQuerySampleUnavailable = dbbridge.ErrSampleQueryUnavailable

type QuerySampleService struct {
	system  *client.SystemServiceClient
	issuer  *client.SystemExecutionAuthorizationClient
	catalog *federatedquery.Catalog
}

func NewQuerySampleService(
	system *client.SystemServiceClient,
	issuer *client.SystemExecutionAuthorizationClient,
	meta *client.MetaClient,
) *QuerySampleService {
	return &QuerySampleService{
		system:  system,
		issuer:  issuer,
		catalog: federatedquery.NewCatalog(system, meta),
	}
}

func (s *QuerySampleService) Generate(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	engineID uint,
) (string, string, error) {
	if s == nil || s.system == nil || s.issuer == nil || s.catalog == nil || tenantID == 0 || engineID == 0 ||
		!strings.HasPrefix(userAccessToken, "addp_at_") {
		return "", "", fmt.Errorf("查询样例服务未正确初始化")
	}
	descriptor, err := s.system.WithTenantID(tenantID).GetEngineRuntimeDescriptor(ctx, engineID)
	if err != nil {
		return "", "", fmt.Errorf("获取查询引擎描述失败: %w", err)
	}
	if descriptor.LifecycleState != commonModels.EngineLifecycleActive {
		return "", "", fmt.Errorf("%w: 查询引擎未启用", ErrQuerySampleUnavailable)
	}
	enginePlugin, err := plugin.Get(descriptor.EngineType)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrQuerySampleUnavailable, err)
	}
	if provider, ok := enginePlugin.(plugin.FederatedQueryRuntimeProvider); ok {
		return s.generateFederated(ctx, tenantID, userAccessToken, descriptor, provider)
	}
	provider, ok := enginePlugin.(plugin.SQLQueryRuntimeProvider)
	if !ok || !containsQueryLanguage(provider.QueryLanguages(), "sql") {
		return "", "", fmt.Errorf("%w: 当前查询服务仅支持 SQL 引擎", ErrQuerySampleUnavailable)
	}
	return s.generateDirect(ctx, tenantID, userAccessToken, engineID)
}

func (s *QuerySampleService) generateDirect(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	engineID uint,
) (string, string, error) {
	executionID := uuid.New()
	issued, err := s.issuer.Issue(ctx, userAccessToken, client.IssueExecutionAuthorizationRequest{
		Audience:    "service",
		ExecutionID: executionID.String(),
		EngineIDs:   []string{strconv.FormatUint(uint64(engineID), 10)},
		Effects:     []string{"read"},
		ExpiresIn:   40,
	})
	if err != nil {
		return "", "", fmt.Errorf("签发查询样例执行授权失败: %w", err)
	}
	access, err := s.system.WithTenantID(tenantID).GetExecutionEngineAccess(ctx, issued.ID, client.ExecutionEngineAccessRequest{
		ExecutionID:     executionID.String(),
		EngineID:        strconv.FormatUint(uint64(engineID), 10),
		RequiredEffects: []string{"read"},
	})
	if err != nil {
		return "", "", fmt.Errorf("消费查询样例执行授权失败: %w", err)
	}
	return dbbridge.GenerateExecutableSampleQuery(ctx, access.Engine, "sql")
}

func (s *QuerySampleService) generateFederated(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	descriptor *commonModels.EngineRuntimeDescriptor,
	provider plugin.FederatedQueryRuntimeProvider,
) (string, string, error) {
	if descriptor == nil || descriptor.RuntimeEndpoint == nil {
		return "", "", fmt.Errorf("%w: 联邦查询 Runtime 不可用", ErrQuerySampleUnavailable)
	}
	sources, err := s.catalog.Sources(ctx, tenantID, descriptor.ID, provider)
	if err != nil {
		return "", "", err
	}
	candidates := federatedquery.Candidates(sources)
	callerToken, err := s.system.TenantServiceAccessToken(ctx, tenantID)
	if err != nil {
		return "", "", fmt.Errorf("获取 Service Runtime 凭据失败: %w", err)
	}
	var firstExecutionFailure error
	for _, candidate := range candidates {
		executionID := uuid.New()
		issued, issueErr := s.issuer.Issue(ctx, userAccessToken, client.IssueExecutionAuthorizationRequest{
			Audience:    "duckdb",
			ExecutionID: executionID.String(),
			EngineIDs:   []string{strconv.FormatUint(uint64(candidate.EngineID), 10)},
			Effects:     []string{"read"},
			ExpiresIn:   60,
		})
		if issueErr != nil {
			return "", "", fmt.Errorf("签发联邦查询样例执行授权失败: %w", issueErr)
		}
		result, executeErr := provider.ExecuteFederatedQuery(ctx, plugin.ConnectionInfo(descriptor.AsEngine().ConnectionInfo), plugin.FederatedQueryRequest{
			ExecutionID:              executionID.String(),
			ExecutionAuthorizationID: issued.ID,
			SourceEngineIDs:          []uint{candidate.EngineID},
			Query:                    candidate.Query,
			Language:                 "sql",
			Options: plugin.QueryOptions{
				Limit:    10,
				Timeout:  30 * time.Second,
				ReadOnly: true,
			},
			CallerAccessToken: callerToken,
		})
		if executeErr == nil && result != nil && len(result.Rows) > 0 {
			return candidate.Query, "sql", nil
		}
		if firstExecutionFailure == nil {
			if executeErr != nil {
				firstExecutionFailure = executeErr
			} else {
				firstExecutionFailure = errors.New("样例查询没有返回数据")
			}
		}
	}
	if firstExecutionFailure != nil {
		return "", "", fmt.Errorf("%w: %v", ErrQuerySampleUnavailable, firstExecutionFailure)
	}
	return "", "", fmt.Errorf("%w: 当前业务 Catalog 没有可执行候选", ErrQuerySampleUnavailable)
}

func containsQueryLanguage(languages []string, required string) bool {
	for _, language := range languages {
		if strings.EqualFold(strings.TrimSpace(language), required) {
			return true
		}
	}
	return false
}
