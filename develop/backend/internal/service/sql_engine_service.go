package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/develop/backend/internal/config"
	"github.com/google/uuid"
)

var (
	ErrControlledSQLExecutionUnsupported = errors.New("该引擎暂不支持受控 SQL 执行")
	ErrSQLExecutionUnclassifiable        = errors.New("SQL 效果无法可靠判定")
	ErrSQLConnectionTestFailed           = errors.New("数据源连接测试失败")
	ErrSampleQueryUnavailable            = dbbridge.ErrSampleQueryUnavailable
	ErrSampleQueryResourceInvalid        = errors.New("查询模板资源定位符无效")
)

// SQLEngineService executes SQL only through a User-derived Execution
// Authorization. It never accepts or retains a User Access Token outside the
// current method call and never reads engine connection details through the
// legacy internal-key client.
type SQLEngineService struct {
	cfg                     *config.Config
	systemService           *commonClient.SystemServiceClient
	executionAuthorizations *commonClient.SystemExecutionAuthorizationClient
}

func NewSQLEngineService(
	cfg *config.Config,
	systemService *commonClient.SystemServiceClient,
	executionAuthorizations *commonClient.SystemExecutionAuthorizationClient,
) *SQLEngineService {
	return &SQLEngineService{
		cfg: cfg, systemService: systemService,
		executionAuthorizations: executionAuthorizations,
	}
}

type SQLResult struct {
	Columns      []string                 `json:"columns"`
	Rows         []map[string]interface{} `json:"rows"`
	RowsAffected int64                    `json:"rows_affected"`
	Effect       SQLExecutionEffect       `json:"effect"`
}

type IssuedSQLExecutionAuthorization struct {
	AuthorizationID            int64
	Effect                     SQLExecutionEffect
	EngineIDs                  []uint
	ActorPrincipalID           int64
	ActorTenantMembershipID    int64
	IssuedAuthorizationVersion int64
	ExpiresAt                  time.Time
}

func (s *SQLEngineService) IssueSQLExecutionAuthorization(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	executionID uuid.UUID,
	engineID uint,
	sqlContent string,
	timeout int,
) (*IssuedSQLExecutionAuthorization, error) {
	if s == nil || s.cfg == nil || s.executionAuthorizations == nil || tenantID == 0 || engineID == 0 || executionID == uuid.Nil {
		return nil, fmt.Errorf("SQL 执行授权服务未正确初始化")
	}
	effect, expiresIn, err := s.sqlExecutionAuthorizationRequest(sqlContent, timeout)
	if err != nil {
		return nil, err
	}
	return s.issueExecutionAuthorization(
		ctx, tenantID, userAccessToken, executionID, []uint{engineID}, effect, expiresIn, "develop",
	)
}

func (s *SQLEngineService) IssueReadExecutionAuthorization(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	executionID uuid.UUID,
	engineIDs []uint,
	timeout int,
) (*IssuedSQLExecutionAuthorization, error) {
	if s == nil || s.cfg == nil {
		return nil, fmt.Errorf("SQL 执行授权服务未正确初始化")
	}
	return s.issueExecutionAuthorization(
		ctx, tenantID, userAccessToken, executionID, engineIDs, SQLExecutionEffectRead,
		int64(s.normalizedTimeout(timeout)+30), "develop",
	)
}

func (s *SQLEngineService) IssueFederatedReadExecutionAuthorization(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	executionID uuid.UUID,
	engineIDs []uint,
	timeout int,
) (*IssuedSQLExecutionAuthorization, error) {
	return s.issueExecutionAuthorization(
		ctx, tenantID, userAccessToken, executionID, engineIDs, SQLExecutionEffectRead,
		int64(s.normalizedTimeout(timeout)+30), "duckdb",
	)
}

func (s *SQLEngineService) issueExecutionAuthorization(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	executionID uuid.UUID,
	engineIDs []uint,
	effect SQLExecutionEffect,
	expiresIn int64,
	audience string,
) (*IssuedSQLExecutionAuthorization, error) {
	if s == nil || s.cfg == nil || s.executionAuthorizations == nil || tenantID == 0 ||
		executionID == uuid.Nil || !validEngineIDs(engineIDs) {
		return nil, fmt.Errorf("SQL 执行授权服务未正确初始化")
	}
	issued, err := s.executionAuthorizations.Issue(ctx, userAccessToken, commonClient.IssueExecutionAuthorizationRequest{
		Audience: audience, ExecutionID: executionID.String(), EngineIDs: formatEngineIDs(engineIDs),
		Effects: []string{string(effect)}, ExpiresIn: expiresIn,
	})
	if err != nil {
		return nil, fmt.Errorf("签发执行授权失败: %w", err)
	}
	return issuedSQLExecutionAuthorization(issued, tenantID, effect, engineIDs)
}

func (s *SQLEngineService) TestAuthorizedConnection(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	executionID uuid.UUID,
	engineID uint,
) error {
	if s == nil || s.cfg == nil || s.systemService == nil || s.executionAuthorizations == nil ||
		tenantID == 0 || engineID == 0 || executionID == uuid.Nil {
		return fmt.Errorf("SQL 连接测试服务未正确初始化")
	}
	timeout := s.normalizedTimeout(0)
	authorization, err := s.IssueReadExecutionAuthorization(
		ctx, tenantID, userAccessToken, executionID, []uint{engineID}, timeout,
	)
	if err != nil {
		return fmt.Errorf("签发连接测试授权失败: %w", err)
	}
	engine, err := s.executionEngine(ctx, tenantID, executionID, engineID, authorization)
	if err != nil {
		return err
	}

	testCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	if err := dbbridge.TestConnection(testCtx, engine); err != nil {
		return fmt.Errorf("%w: %v", ErrSQLConnectionTestFailed, err)
	}
	return nil
}

func (s *SQLEngineService) IssueSQLExecutionAuthorizationFromExecution(
	ctx context.Context,
	tenantID uint,
	parentExecutionID uuid.UUID,
	executionID uuid.UUID,
	engineID uint,
	sqlContent string,
	timeout int,
) (*IssuedSQLExecutionAuthorization, error) {
	if s == nil || s.cfg == nil || s.systemService == nil || tenantID == 0 || engineID == 0 ||
		parentExecutionID == uuid.Nil || executionID == uuid.Nil {
		return nil, fmt.Errorf("SQL 执行授权服务未正确初始化")
	}
	effect, expiresIn, err := s.sqlExecutionAuthorizationRequest(sqlContent, timeout)
	if err != nil {
		return nil, err
	}
	return s.issueExecutionAuthorizationFromExecution(
		ctx, tenantID, parentExecutionID, executionID, []uint{engineID}, effect, expiresIn, "develop",
	)
}

func (s *SQLEngineService) IssueReadExecutionAuthorizationFromExecution(
	ctx context.Context,
	tenantID uint,
	parentExecutionID uuid.UUID,
	executionID uuid.UUID,
	engineIDs []uint,
	timeout int,
) (*IssuedSQLExecutionAuthorization, error) {
	if s == nil || s.cfg == nil {
		return nil, fmt.Errorf("SQL 执行授权服务未正确初始化")
	}
	return s.issueExecutionAuthorizationFromExecution(
		ctx, tenantID, parentExecutionID, executionID, engineIDs, SQLExecutionEffectRead,
		int64(s.normalizedTimeout(timeout)+30), "develop",
	)
}

func (s *SQLEngineService) IssueFederatedReadExecutionAuthorizationFromExecution(
	ctx context.Context,
	tenantID uint,
	parentExecutionID uuid.UUID,
	executionID uuid.UUID,
	engineIDs []uint,
	timeout int,
) (*IssuedSQLExecutionAuthorization, error) {
	return s.issueExecutionAuthorizationFromExecution(
		ctx, tenantID, parentExecutionID, executionID, engineIDs, SQLExecutionEffectRead,
		int64(s.normalizedTimeout(timeout)+30), "duckdb",
	)
}

func (s *SQLEngineService) issueExecutionAuthorizationFromExecution(
	ctx context.Context,
	tenantID uint,
	parentExecutionID uuid.UUID,
	executionID uuid.UUID,
	engineIDs []uint,
	effect SQLExecutionEffect,
	expiresIn int64,
	audience string,
) (*IssuedSQLExecutionAuthorization, error) {
	if s == nil || s.cfg == nil || s.systemService == nil || tenantID == 0 || !validEngineIDs(engineIDs) ||
		parentExecutionID == uuid.Nil || executionID == uuid.Nil {
		return nil, fmt.Errorf("SQL 执行授权服务未正确初始化")
	}
	issued, err := s.systemService.WithTenantID(tenantID).IssueExecutionAuthorizationFromExecution(
		ctx,
		commonClient.IssueExecutionAuthorizationFromExecutionRequest{
			ParentExecutionID: parentExecutionID.String(),
			Audience:          audience,
			ExecutionID:       executionID.String(),
			EngineIDs:         formatEngineIDs(engineIDs),
			Effects:           []string{string(effect)},
			ExpiresIn:         expiresIn,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("从父执行签发 SQL 执行授权失败: %w", err)
	}
	return issuedSQLExecutionAuthorization(issued, tenantID, effect, engineIDs)
}

func (s *SQLEngineService) sqlExecutionAuthorizationRequest(
	sqlContent string,
	timeout int,
) (SQLExecutionEffect, int64, error) {
	effect, err := ClassifySQLExecutionEffect(sqlContent)
	if err != nil {
		return "", 0, fmt.Errorf("%w: %v", ErrSQLExecutionUnclassifiable, err)
	}
	timeout = s.normalizedTimeout(timeout)
	return effect, int64(timeout + 30), nil
}

func issuedSQLExecutionAuthorization(
	issued *commonClient.IssuedExecutionAuthorization,
	tenantID uint,
	effect SQLExecutionEffect,
	engineIDs []uint,
) (*IssuedSQLExecutionAuthorization, error) {
	if issued == nil || issued.TenantID != strconv.FormatUint(uint64(tenantID), 10) {
		return nil, fmt.Errorf("执行授权租户与当前上下文不一致")
	}
	authorizationID, err := parseIssuedAuthorizationID(issued.ID)
	if err != nil {
		return nil, err
	}
	actorPrincipalID, err := parseIssuedAuthorizationID(issued.ActorPrincipalID)
	if err != nil {
		return nil, err
	}
	membershipID, err := parseIssuedAuthorizationID(issued.TenantMembershipID)
	if err != nil {
		return nil, err
	}
	authorizationVersion, err := parseIssuedAuthorizationID(issued.IssuedAuthorizationVersion)
	if err != nil {
		return nil, err
	}
	return &IssuedSQLExecutionAuthorization{
		AuthorizationID: authorizationID, Effect: effect, EngineIDs: append([]uint(nil), engineIDs...), ActorPrincipalID: actorPrincipalID,
		ActorTenantMembershipID: membershipID, IssuedAuthorizationVersion: authorizationVersion,
		ExpiresAt: issued.ExpiresAt.UTC(),
	}, nil
}

func (s *SQLEngineService) ExecuteIssuedSQLAuthorization(
	ctx context.Context,
	tenantID uint,
	executionID uuid.UUID,
	engineID uint,
	sqlContent string,
	timeout int,
	limit int,
	authorization *IssuedSQLExecutionAuthorization,
) (*SQLResult, error) {
	timeout = s.normalizedTimeout(timeout)
	engine, err := s.executionEngine(ctx, tenantID, executionID, engineID, authorization)
	if err != nil {
		return nil, err
	}
	if !dbbridge.SupportsReadOnlySQLExecution(engine.EngineType) {
		return nil, ErrControlledSQLExecutionUnsupported
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	if authorization.Effect == SQLExecutionEffectRead {
		queryResult, err := dbbridge.ExecuteReadOnlyQuery(execCtx, engine, sqlContent, limit)
		if err != nil {
			return nil, err
		}
		rows := queryResult.Rows
		if rows == nil {
			rows = []map[string]interface{}{}
		}
		return &SQLResult{
			Columns: queryResult.Columns, Rows: rows, RowsAffected: int64(len(rows)), Effect: authorization.Effect,
		}, nil
	}

	rowsAffected, err := dbbridge.ExecuteStatement(execCtx, engine, sqlContent)
	if err != nil {
		return nil, err
	}
	return &SQLResult{
		Columns: []string{}, Rows: []map[string]interface{}{}, RowsAffected: rowsAffected, Effect: authorization.Effect,
	}, nil
}

func (s *SQLEngineService) executionEngine(
	ctx context.Context,
	tenantID uint,
	executionID uuid.UUID,
	engineID uint,
	authorization *IssuedSQLExecutionAuthorization,
) (*commonModels.Engine, error) {
	if s == nil || s.systemService == nil || tenantID == 0 || engineID == 0 || executionID == uuid.Nil ||
		authorization == nil || authorization.AuthorizationID <= 0 || authorization.Effect == "" ||
		!containsEngineID(authorization.EngineIDs, engineID) ||
		!authorization.ExpiresAt.After(time.Now().UTC()) {
		return nil, fmt.Errorf("执行授权无效或已过期")
	}
	access, err := s.systemService.WithTenantID(tenantID).GetExecutionEngineAccess(
		ctx,
		strconv.FormatInt(authorization.AuthorizationID, 10),
		commonClient.ExecutionEngineAccessRequest{
			ExecutionID:     executionID.String(),
			EngineID:        strconv.FormatUint(uint64(engineID), 10),
			RequiredEffects: []string{string(authorization.Effect)},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("获取执行期引擎访问失败: %w", err)
	}
	return access.Engine, nil
}

func (s *SQLEngineService) ExecutionEngines(
	ctx context.Context,
	tenantID uint,
	executionID uuid.UUID,
	authorization *IssuedSQLExecutionAuthorization,
) ([]commonModels.Engine, error) {
	if authorization == nil || len(authorization.EngineIDs) == 0 {
		return nil, nil
	}
	engines := make([]commonModels.Engine, 0, len(authorization.EngineIDs))
	for _, engineID := range authorization.EngineIDs {
		engine, err := s.executionEngine(ctx, tenantID, executionID, engineID, authorization)
		if err != nil {
			return nil, err
		}
		engines = append(engines, *engine)
	}
	return engines, nil
}

func parseIssuedAuthorizationID(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, fmt.Errorf("System 返回了无效的执行授权标识")
	}
	return parsed, nil
}

func (s *SQLEngineService) GenerateAuthorizedSampleQuery(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	executionID uuid.UUID,
	engineID uint,
	locator *resourcetree.ResourceLocator,
) (string, string, error) {
	var selectedPath *plugin.CatalogPath
	if locator != nil {
		if locator.EngineID != engineID {
			return "", "", fmt.Errorf("%w: 引擎 ID 不匹配", ErrSampleQueryResourceInvalid)
		}
	}
	authorization, err := s.IssueReadExecutionAuthorization(
		ctx, tenantID, userAccessToken, executionID, []uint{engineID}, 10,
	)
	if err != nil {
		return "", "", err
	}
	engine, err := s.executionEngine(ctx, tenantID, executionID, engineID, authorization)
	if err != nil {
		return "", "", err
	}
	if locator != nil {
		model, modelErr := dbbridge.CatalogModel(engine.EngineType)
		if modelErr != nil {
			return "", "", fmt.Errorf("%w: %v", ErrSampleQueryResourceInvalid, modelErr)
		}
		path, pathErr := resourcetree.ProviderCatalogPathFromLocator(model, locator)
		if pathErr != nil || len(path.Segments) < 2 {
			if pathErr == nil {
				pathErr = fmt.Errorf("资源不是可查询数据项")
			}
			return "", "", fmt.Errorf("%w: %v", ErrSampleQueryResourceInvalid, pathErr)
		}
		selectedPath = &path
	}
	return generateExecutableSampleQuery(ctx, engine, selectedPath)
}

func generateExecutableSampleQuery(ctx context.Context, engine *commonModels.Engine, selectedPath *plugin.CatalogPath) (string, string, error) {
	return dbbridge.GenerateExecutableSampleQuery(ctx, engine, "", dbbridge.ExecutableSampleQueryOptions{
		QueryLimit:      10,
		ValidationLimit: 10,
		Path:            selectedPath,
	})
}

func formatEngineIDs(engineIDs []uint) []string {
	result := make([]string, 0, len(engineIDs))
	for _, engineID := range engineIDs {
		result = append(result, strconv.FormatUint(uint64(engineID), 10))
	}
	return result
}

func validEngineIDs(engineIDs []uint) bool {
	if len(engineIDs) == 0 {
		return false
	}
	seen := make(map[uint]struct{}, len(engineIDs))
	for _, engineID := range engineIDs {
		if engineID == 0 {
			return false
		}
		if _, exists := seen[engineID]; exists {
			return false
		}
		seen[engineID] = struct{}{}
	}
	return true
}

func containsEngineID(engineIDs []uint, target uint) bool {
	for _, engineID := range engineIDs {
		if engineID == target {
			return true
		}
	}
	return false
}

func (s *SQLEngineService) normalizedTimeout(timeout int) int {
	if timeout <= 0 {
		timeout = s.cfg.DefaultQueryTimeout
	}
	if timeout > s.cfg.MaxQueryTimeout {
		timeout = s.cfg.MaxQueryTimeout
	}
	return timeout
}
