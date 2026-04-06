package data

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
)

type GraphQueryExecutor struct {
	repo         *repository.GraphQueryServiceRepository
	systemClient *commonClient.SystemClient
}

func NewGraphQueryExecutor(
	repo *repository.GraphQueryServiceRepository,
	systemClient *commonClient.SystemClient,
) *GraphQueryExecutor {
	return &GraphQueryExecutor{repo: repo, systemClient: systemClient}
}

// Execute 执行图查询服务
// tenantID == 0 表示公开访问（跳过租户校验，仅允许 public_access=true 的服务）
func (e *GraphQueryExecutor) Execute(
	ctx context.Context,
	serviceName string,
	tenantID uint,
	req *models.GraphQueryExecuteRequest,
) (*models.GraphQueryResponse, error) {
	startTime := time.Now()

	// 1. 获取服务配置
	service, err := e.repo.GetByName(serviceName)
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			return nil, fmt.Errorf("service '%s' not found", serviceName)
		}
		return nil, fmt.Errorf("get service failed: %w", err)
	}

	// 2. 访问控制
	if tenantID == 0 {
		if !service.PublicAccess {
			return nil, fmt.Errorf("service '%s' requires authentication", serviceName)
		}
	} else {
		if service.TenantID != tenantID {
			return nil, fmt.Errorf("service '%s' not found", serviceName)
		}
	}

	// 3. 检查服务状态
	if service.Status != "active" {
		return nil, fmt.Errorf("service '%s' is %s", serviceName, service.Status)
	}

	// 4. 获取引擎配置
	engine, err := e.systemClient.GetEngine(service.EngineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 5. 准备分页参数
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	maxRecords := service.MaxRecords
	if maxRecords <= 0 {
		maxRecords = 500
	}
	if pageSize > maxRecords {
		pageSize = maxRecords
	}
	offset := (page - 1) * pageSize

	// 6. 按配置类型构建 Cypher
	params := req.Parameters
	if params == nil {
		params = make(map[string]interface{})
	}

	var cypher string
	var isLabelMode bool

	switch service.ConfigType {
	case "label":
		isLabelMode = true
		cypher = buildLabelCypher(service, params, offset, pageSize)
	case "cypher":
		cypher, err = bindCypherParams(service.CypherQuery, params, offset, pageSize)
		if err != nil {
			return nil, fmt.Errorf("parameter binding failed: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported config_type: %s", service.ConfigType)
	}

	log.Printf("[GraphQueryExecutor] service=%s cypher=%s", serviceName, cypher)

	// 7. 执行查询
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}

	// 8. 构建响应
	resp := &models.GraphQueryResponse{
		RowsCount: len(result.Rows),
	}

	// 表格数据（table | both）
	if service.IsTableResult() {
		resp.Columns = result.Columns
		resp.Rows = result.Rows
	}

	// 图数据（graph | both）
	if service.IsGraphResult() && result.GraphData != nil {
		resp.GraphData = result.GraphData
	}

	// label 模式提供分页信息
	if isLabelMode {
		total, countErr := e.countLabelNodes(ctx, engine, service, params)
		if countErr != nil {
			log.Printf("[GraphQueryExecutor] count query failed (non-fatal): %v", countErr)
		} else {
			hasMore := int64(page*pageSize) < total
			resp.TotalCount = &total
			resp.Page = &page
			resp.PageSize = &pageSize
			resp.HasMore = &hasMore
		}
	}

	duration := time.Since(startTime).Milliseconds()
	log.Printf("[GraphQueryExecutor] service=%s completed in %dms rows=%d", serviceName, duration, len(result.Rows))

	return resp, nil
}

// countLabelNodes 查询 label 模式下满足过滤条件的节点总数
func (e *GraphQueryExecutor) countLabelNodes(
	ctx context.Context,
	engine *commonmodels.Engine,
	service *models.GraphQueryService,
	filters map[string]interface{},
) (int64, error) {
	countCypher := buildLabelCountCypher(service, filters)
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, countCypher)
	if err != nil {
		return 0, err
	}
	if len(result.Rows) > 0 {
		if v, ok := result.Rows[0]["total"]; ok {
			switch n := v.(type) {
			case int64:
				return n, nil
			case float64:
				return int64(n), nil
			}
		}
	}
	return 0, nil
}

// buildLabelCypher 根据节点标签和过滤参数生成 Cypher
func buildLabelCypher(service *models.GraphQueryService, filters map[string]interface{}, offset, limit int) string {
	label := service.NodeLabel
	filterableProps := service.GetFilterableProperties()
	returnProps := service.GetProperties()

	filterableSet := make(map[string]bool)
	for _, p := range filterableProps {
		filterableSet[p] = true
	}

	var whereParts []string
	for k, v := range filters {
		if !filterableSet[k] {
			continue
		}
		whereParts = append(whereParts, fmt.Sprintf("n.`%s` = %s", k, formatCypherValue(v)))
	}

	cypher := fmt.Sprintf("MATCH (n:`%s`)", escapeCypherLabel(label))
	if len(whereParts) > 0 {
		cypher += " WHERE " + strings.Join(whereParts, " AND ")
	}

	if len(returnProps) > 0 {
		var returnParts []string
		for _, p := range returnProps {
			returnParts = append(returnParts, fmt.Sprintf("n.`%s` AS `%s`", p, p))
		}
		cypher += " RETURN " + strings.Join(returnParts, ", ")
	} else {
		cypher += " RETURN n"
	}

	cypher += fmt.Sprintf(" SKIP %d LIMIT %d", offset, limit)
	return cypher
}

// buildLabelCountCypher 生成计数 Cypher
func buildLabelCountCypher(service *models.GraphQueryService, filters map[string]interface{}) string {
	label := service.NodeLabel
	filterableProps := service.GetFilterableProperties()

	filterableSet := make(map[string]bool)
	for _, p := range filterableProps {
		filterableSet[p] = true
	}

	var whereParts []string
	for k, v := range filters {
		if !filterableSet[k] {
			continue
		}
		whereParts = append(whereParts, fmt.Sprintf("n.`%s` = %s", k, formatCypherValue(v)))
	}

	cypher := fmt.Sprintf("MATCH (n:`%s`)", escapeCypherLabel(label))
	if len(whereParts) > 0 {
		cypher += " WHERE " + strings.Join(whereParts, " AND ")
	}
	cypher += " RETURN count(n) AS total"
	return cypher
}

// bindCypherParams 将用户参数安全地绑定到 Cypher 模板中
var paramRe = regexp.MustCompile(`\$(\w+)`)

func bindCypherParams(template string, params map[string]interface{}, offset, limit int) (string, error) {
	builtinParams := map[string]interface{}{
		"offset":    offset,
		"limit":     limit,
		"skip":      offset,
		"page_size": limit,
	}

	var bindErr error
	result := paramRe.ReplaceAllStringFunc(template, func(match string) string {
		if bindErr != nil {
			return match
		}
		name := match[1:] // 去掉 $ 前缀
		nameLower := strings.ToLower(name)

		// 内置分页参数优先
		if v, ok := builtinParams[nameLower]; ok {
			return fmt.Sprintf("%v", v)
		}

		// 用户参数（大小写不敏感）
		for k, v := range params {
			if strings.EqualFold(k, name) {
				return formatCypherValue(v)
			}
		}

		bindErr = fmt.Errorf("parameter '$%s' is required but not provided", name)
		return match
	})

	if bindErr != nil {
		return "", bindErr
	}
	return result, nil
}

// formatCypherValue 将 Go 值格式化为 Cypher 字面量（含字符串转义）
func formatCypherValue(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch val := v.(type) {
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case string:
		escaped := strings.ReplaceAll(val, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `'`, `\'`)
		return fmt.Sprintf("'%s'", escaped)
	default:
		s := fmt.Sprintf("%v", val)
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `'`, `\'`)
		return fmt.Sprintf("'%s'", escaped)
	}
}

// escapeCypherLabel 对节点标签名做安全处理
func escapeCypherLabel(label string) string {
	return strings.ReplaceAll(label, "`", "``")
}
