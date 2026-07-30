package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/resourcetree"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
)

type IssuedWorkflowExecutionAuthorization struct {
	AuthorizationID            int64
	EngineEffects              map[uint][]string
	Effects                    []string
	ActorPrincipalID           int64
	ActorTenantMembershipID    int64
	IssuedAuthorizationVersion int64
	ExpiresAt                  time.Time
}

type workflowExecutionAuthorizationPlan struct {
	engineEffects map[uint][]string
	engineIDs     []string
	effects       []string
	expiresIn     int64
}

func (e *DevExecutor) prepareWorkflowExecutionAuthorization(
	ctx context.Context,
	devTask *models.DevTask,
	tenantID uint,
	userAccessToken string,
	executionID string,
) (*IssuedWorkflowExecutionAuthorization, error) {
	if devTask == nil || devTask.DevType != commonExecution.TaskTypeWorkflow {
		return nil, nil
	}
	if e.operatorDiscovery == nil || e.sqlEngine == nil || e.sqlEngine.executionAuthorizations == nil {
		return nil, fmt.Errorf("工作流执行授权服务不可用")
	}
	if strings.TrimSpace(userAccessToken) == "" {
		return nil, fmt.Errorf("异步工作流执行必须由当前 User Access Token 派生 Execution Authorization")
	}
	plan, err := e.buildWorkflowExecutionAuthorizationPlan(ctx, devTask, tenantID)
	if err != nil {
		return nil, err
	}
	parsedExecutionID, err := uuid.Parse(executionID)
	if err != nil {
		return nil, fmt.Errorf("执行 ID 无效: %w", err)
	}
	issued, err := e.sqlEngine.executionAuthorizations.Issue(ctx, userAccessToken, commonClient.IssueExecutionAuthorizationRequest{
		Audience: "develop", ExecutionID: parsedExecutionID.String(), EngineIDs: plan.engineIDs,
		Effects: plan.effects, ExpiresIn: plan.expiresIn,
	})
	if err != nil {
		return nil, fmt.Errorf("签发工作流执行授权失败: %w", err)
	}
	return issuedWorkflowExecutionAuthorization(issued, tenantID, plan)
}

func (e *DevExecutor) prepareWorkflowExecutionAuthorizationFromExecution(
	ctx context.Context,
	devTask *models.DevTask,
	tenantID uint,
	parentExecutionID uuid.UUID,
	executionID uuid.UUID,
) (*IssuedWorkflowExecutionAuthorization, error) {
	if devTask == nil || devTask.DevType != commonExecution.TaskTypeWorkflow {
		return nil, nil
	}
	if e.operatorDiscovery == nil || e.sqlEngine == nil || e.sqlEngine.systemService == nil ||
		parentExecutionID == uuid.Nil || executionID == uuid.Nil {
		return nil, fmt.Errorf("工作流执行授权服务不可用")
	}
	plan, err := e.buildWorkflowExecutionAuthorizationPlan(ctx, devTask, tenantID)
	if err != nil {
		return nil, err
	}
	issued, err := e.sqlEngine.systemService.WithTenantID(tenantID).IssueExecutionAuthorizationFromExecution(
		ctx,
		commonClient.IssueExecutionAuthorizationFromExecutionRequest{
			ParentExecutionID: parentExecutionID.String(), Audience: "develop",
			ExecutionID: executionID.String(), EngineIDs: plan.engineIDs,
			Effects: plan.effects, ExpiresIn: plan.expiresIn,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("从父执行签发工作流执行授权失败: %w", err)
	}
	return issuedWorkflowExecutionAuthorization(issued, tenantID, plan)
}

func (e *DevExecutor) buildWorkflowExecutionAuthorizationPlan(
	ctx context.Context,
	devTask *models.DevTask,
	tenantID uint,
) (*workflowExecutionAuthorizationPlan, error) {
	workflowDefinition, ok := devTask.Content["workflow_definition"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("工作流定义无效")
	}
	workflowEngineID := workflowEngineIDFromExecutionConfig(devTask.ExecutionConfig)
	if workflowEngineID == 0 {
		return nil, fmt.Errorf("工作流执行必须提供 execution_config.engine_id")
	}
	operators, err := e.operatorDiscovery.GetOperatorsByWorkflowEngineIDForTenant(ctx, workflowEngineID, tenantID)
	if err != nil {
		return nil, err
	}
	operatorByName := make(map[string]PublicOperatorDescriptor, len(operators)*2)
	for _, operator := range operators {
		operatorByName[operator.ID] = operator
		operatorByName[operator.Name] = operator
	}
	tasks, ok := workflowTasksFromInterface(workflowDefinition["tasks"])
	if !ok || len(tasks) == 0 {
		return nil, fmt.Errorf("工作流定义缺少有效 tasks")
	}

	engineEffects := make(map[uint]map[string]struct{})
	allEffects := make(map[string]struct{})
	for index, task := range tasks {
		operatorName := strings.TrimSpace(stringParam(task, "operator"))
		operator, exists := operatorByName[operatorName]
		if !exists {
			return nil, fmt.Errorf("任务 %d 的算子不存在或不支持 workflow: %s", index, operatorName)
		}
		for _, effect := range operator.Effects {
			allEffects[effect] = struct{}{}
		}
		params, _ := task["params"].(map[string]interface{})
		adapter, hasAdapter := workflowOperatorAdapterSpecFor(operator.EngineType, operator.ID)
		if !hasAdapter {
			continue
		}
		if adapter.AccessPlan != nil {
			if err := addLocatorEngineEffect(engineEffects, params, "locator", "read"); err != nil {
				return nil, fmt.Errorf("任务 %d 源资源无效: %w", index, err)
			}
			if err := addLocatorEngineEffect(engineEffects, params, "target_parent_locator", "write"); err != nil {
				return nil, fmt.Errorf("任务 %d 目标资源无效: %w", index, err)
			}
			continue
		}
		for _, input := range adapter.ResourceInputs {
			if err := addLocatorEngineEffect(engineEffects, params, input.PublicParam, "read"); err != nil {
				return nil, fmt.Errorf("任务 %d 输入资源无效: %w", index, err)
			}
		}
		for _, output := range adapter.ResourceOutputs {
			if err := addLocatorEngineEffect(engineEffects, params, output.ParentParam, "write"); err != nil {
				return nil, fmt.Errorf("任务 %d 输出资源无效: %w", index, err)
			}
		}
	}
	if len(allEffects) == 0 {
		return nil, fmt.Errorf("工作流未声明任何执行效果")
	}
	effects := orderedWorkflowEffects(allEffects)
	for _, effect := range effects {
		addEngineEffect(engineEffects, workflowEngineID, effect)
	}
	var executionConfig models.WorkflowExecutionConfig
	encodedConfig, err := json.Marshal(devTask.ExecutionConfig)
	if err != nil {
		return nil, fmt.Errorf("序列化工作流执行配置失败: %w", err)
	}
	if err := json.Unmarshal(encodedConfig, &executionConfig); err != nil {
		return nil, fmt.Errorf("解析工作流执行配置失败: %w", err)
	}
	if rawID, configured := executionConfig.EngineSpecific["spark_cluster_id"]; configured {
		engineID, err := positiveUintFromInterface(rawID)
		if err != nil {
			return nil, fmt.Errorf("spark_cluster_id 无效: %w", err)
		}
		addEngineEffect(engineEffects, engineID, "read")
		allEffects["read"] = struct{}{}
		effects = orderedWorkflowEffects(allEffects)
	}

	engineIDs := make([]uint, 0, len(engineEffects))
	engineIDTexts := make([]string, 0, len(engineEffects))
	for engineID := range engineEffects {
		engineIDs = append(engineIDs, engineID)
	}
	sort.Slice(engineIDs, func(i, j int) bool { return engineIDs[i] < engineIDs[j] })
	for _, engineID := range engineIDs {
		engineIDTexts = append(engineIDTexts, strconv.FormatUint(uint64(engineID), 10))
	}
	timeout := devTask.Timeout
	if timeout <= 0 {
		timeout = 300
	}
	expiresIn := timeout + 60
	if expiresIn > 3600 {
		expiresIn = 3600
	}
	normalizedEngineEffects := make(map[uint][]string, len(engineEffects))
	for engineID, values := range engineEffects {
		normalizedEngineEffects[engineID] = orderedWorkflowEffects(values)
	}
	return &workflowExecutionAuthorizationPlan{
		engineEffects: normalizedEngineEffects,
		engineIDs:     engineIDTexts,
		effects:       effects,
		expiresIn:     int64(expiresIn),
	}, nil
}

func issuedWorkflowExecutionAuthorization(
	issued *commonClient.IssuedExecutionAuthorization,
	tenantID uint,
	plan *workflowExecutionAuthorizationPlan,
) (*IssuedWorkflowExecutionAuthorization, error) {
	if issued == nil || plan == nil || issued.TenantID != strconv.FormatUint(uint64(tenantID), 10) {
		return nil, fmt.Errorf("工作流执行授权租户与当前上下文不一致")
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
	return &IssuedWorkflowExecutionAuthorization{
		AuthorizationID: authorizationID, EngineEffects: plan.engineEffects, Effects: plan.effects,
		ActorPrincipalID: actorPrincipalID, ActorTenantMembershipID: membershipID,
		IssuedAuthorizationVersion: authorizationVersion, ExpiresAt: issued.ExpiresAt.UTC(),
	}, nil
}

func addLocatorEngineEffect(engineEffects map[uint]map[string]struct{}, params map[string]interface{}, name, effect string) error {
	value := strings.TrimSpace(stringParam(params, name))
	if value == "" {
		return nil
	}
	locator, err := resourcetree.ParseURI(value)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	addEngineEffect(engineEffects, locator.EngineID, effect)
	return nil
}

func addEngineEffect(engineEffects map[uint]map[string]struct{}, engineID uint, effect string) {
	if engineID == 0 || effect == "" {
		return
	}
	if engineEffects[engineID] == nil {
		engineEffects[engineID] = make(map[string]struct{})
	}
	engineEffects[engineID][effect] = struct{}{}
}

func orderedWorkflowEffects(values map[string]struct{}) []string {
	order := []string{"read", "write", "ddl", "external_effect"}
	result := make([]string, 0, len(values))
	for _, effect := range order {
		if _, exists := values[effect]; exists {
			result = append(result, effect)
		}
	}
	return result
}
