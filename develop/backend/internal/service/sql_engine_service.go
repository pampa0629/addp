package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/config"
	"github.com/google/uuid"
)

var (
	ErrControlledSQLExecutionUnsupported = errors.New("该引擎暂不支持受控 SQL 执行")
	ErrSQLExecutionUnclassifiable        = errors.New("SQL 效果无法可靠判定")
	ErrSQLConnectionTestFailed           = errors.New("数据源连接测试失败")
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
	ActorPrincipalID           int64
	ActorTenantMembershipID    int64
	IssuedAuthorizationVersion int64
	ExpiresAt                  time.Time
}

func (s *SQLEngineService) ExecuteAuthorizedSQL(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	executionID uuid.UUID,
	engineID uint,
	sqlContent string,
	timeout int,
) (*SQLResult, error) {
	if s == nil || s.cfg == nil || s.systemService == nil || s.executionAuthorizations == nil ||
		tenantID == 0 || engineID == 0 || executionID == uuid.Nil {
		return nil, fmt.Errorf("SQL 执行服务未正确初始化")
	}
	authorization, err := s.IssueSQLExecutionAuthorization(
		ctx, tenantID, userAccessToken, executionID, engineID, sqlContent, timeout,
	)
	if err != nil {
		return nil, err
	}
	return s.ExecuteIssuedSQLAuthorization(
		ctx, tenantID, executionID, engineID, sqlContent, timeout, authorization,
	)
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
	engineIDText := strconv.FormatUint(uint64(engineID), 10)
	issued, err := s.executionAuthorizations.Issue(ctx, userAccessToken, commonClient.IssueExecutionAuthorizationRequest{
		Audience: "develop", ExecutionID: executionID.String(), EngineIDs: []string{engineIDText},
		Effects: []string{string(effect)}, ExpiresIn: expiresIn,
	})
	if err != nil {
		return nil, fmt.Errorf("签发执行授权失败: %w", err)
	}
	return issuedSQLExecutionAuthorization(issued, tenantID, effect)
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
	engineIDText := strconv.FormatUint(uint64(engineID), 10)
	issued, err := s.executionAuthorizations.Issue(ctx, userAccessToken, commonClient.IssueExecutionAuthorizationRequest{
		Audience: "develop", ExecutionID: executionID.String(), EngineIDs: []string{engineIDText},
		Effects: []string{string(SQLExecutionEffectRead)}, ExpiresIn: int64(timeout + 30),
	})
	if err != nil {
		return fmt.Errorf("签发连接测试授权失败: %w", err)
	}
	authorization, err := issuedSQLExecutionAuthorization(issued, tenantID, SQLExecutionEffectRead)
	if err != nil {
		return err
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
	issued, err := s.systemService.WithTenantID(tenantID).IssueExecutionAuthorizationFromExecution(
		ctx,
		commonClient.IssueExecutionAuthorizationFromExecutionRequest{
			ParentExecutionID: parentExecutionID.String(),
			Audience:          "develop",
			ExecutionID:       executionID.String(),
			EngineIDs:         []string{strconv.FormatUint(uint64(engineID), 10)},
			Effects:           []string{string(effect)},
			ExpiresIn:         expiresIn,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("从父执行签发 SQL 执行授权失败: %w", err)
	}
	return issuedSQLExecutionAuthorization(issued, tenantID, effect)
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
		AuthorizationID: authorizationID, Effect: effect, ActorPrincipalID: actorPrincipalID,
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
		queryResult, err := dbbridge.ExecuteReadOnlyQuery(execCtx, engine, sqlContent)
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

func parseIssuedAuthorizationID(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, fmt.Errorf("System 返回了无效的执行授权标识")
	}
	return parsed, nil
}

func (s *SQLEngineService) GenerateSampleQuery(ctx context.Context, tenantID, engineID uint) (string, string, error) {
	descriptor, err := s.systemService.WithTenantID(tenantID).GetEngineRuntimeDescriptor(ctx, engineID)
	if err != nil {
		return "", "", fmt.Errorf("获取引擎描述失败：%w", err)
	}
	resource := descriptor.AsEngine()
	if resource == nil {
		return "", "", fmt.Errorf("引擎描述为空")
	}
	query, language := dbbridge.GenerateSampleQuery(ctx, resource)
	return query, language, nil
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
