package dbbridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"github.com/addp/common/sqldialect"
	"github.com/beltran/gohive"
	"gorm.io/gorm"

	// 导入所有内置引擎插件，触发 init() 注册
	_ "github.com/addp/common/engine/plugins/builtin/all"
)

// BuildDSN 使用插件系统构建连接字符串
func BuildDSN(engine *models.Engine) (string, error) {
	return plugin.BuildDSN(toPluginEngine(engine))
}

// TestConnection 使用插件系统测试连接
func TestConnection(ctx context.Context, engine *models.Engine) error {
	return plugin.TestConnection(ctx, toPluginEngine(engine))
}

// GenerateCapabilities 使用插件系统生成结构化能力声明 JSON
func GenerateCapabilities(engineType string) (string, error) {
	return plugin.GenerateCapabilities(engineType)
}

// GetSensitiveFields 获取敏感字段列表
func GetSensitiveFields(engineType string) ([]string, error) {
	return plugin.GetSensitiveFields(engineType)
}

// GetRequiredFields 获取必填字段列表
func GetRequiredFields(engineType string) ([]string, error) {
	return plugin.GetRequiredFields(engineType)
}

// GetDefaultPort 获取默认端口
func GetDefaultPort(engineType string) (int, error) {
	return plugin.GetDefaultPort(engineType)
}

// ListAllTypes 列出所有已注册的数据库类型
func ListAllTypes() []string {
	return plugin.List()
}

// GetAllPlugins 获取所有插件信息（用于前端API）
func GetAllPlugins() map[string]PluginInfo {
	plugins := plugin.GetAll()
	result := make(map[string]PluginInfo)

	for dbType, p := range plugins {
		result[dbType] = PluginInfo{
			Type:            p.Type(),
			DisplayName:     p.DisplayName(),
			Origin:          p.EngineOrigin(),
			DefaultPort:     p.DefaultPort(),
			RequiredFields:  p.RequiredFields(),
			SensitiveFields: p.SensitiveFields(),
		}
	}

	return result
}

// PluginInfo 插件信息（用于API响应）
type PluginInfo struct {
	Type            string   `json:"type"`
	DisplayName     string   `json:"display_name"`
	Origin          string   `json:"origin"`
	DefaultPort     int      `json:"default_port"`
	RequiredFields  []string `json:"required_fields"`
	SensitiveFields []string `json:"sensitive_fields"`
}

// === 连接池管理方法（供Develop模块使用）===

// GetOrCreatePool 获取或创建连接池
// 这是推荐的获取连接池的方式，会自动管理连接池的生命周期
func GetOrCreatePool(engine *models.Engine, config *plugin.PoolConfig) (*gorm.DB, error) {
	return plugin.GetOrCreatePoolFromFactory(toPluginEngine(engine), config)
}

// DefaultPoolConfig 返回默认连接池配置
func DefaultPoolConfig() *plugin.PoolConfig {
	return plugin.DefaultPoolConfig()
}

// ClosePool 关闭指定引擎的连接池
// 通常在引擎被删除或更新时调用
func ClosePool(engineID uint) error {
	return plugin.ClosePool(engineID)
}

// CloseAllPools 关闭所有连接池
// 在应用关闭时调用，确保优雅关闭
func CloseAllPools() {
	plugin.CloseAllPools()
}

// GetPoolStats 获取所有连接池的统计信息
func GetPoolStats() map[uint]plugin.PoolStats {
	return plugin.GetPoolStats()
}

// === Catalog / facts 查询方法 ===

func toPluginEngine(engine *models.Engine) *plugin.Engine {
	pluginEngine := &plugin.Engine{
		ID:             engine.ID,
		EngineType:     engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}
	return pluginEngine
}

// ListWorkflowOperators 通过工作流运行时 Provider 动态发现算子。
func ListWorkflowOperators(ctx context.Context, engine *models.Engine) ([]models.OperatorDescriptor, error) {
	p, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}
	workflowProvider, ok := p.(plugin.WorkflowRuntimeProvider)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not implement WorkflowRuntimeProvider", engine.EngineType)
	}

	operators, err := workflowProvider.ListOperators(ctx, plugin.ConnectionInfo(engine.ConnectionInfo))
	if err != nil {
		return nil, err
	}
	return toModelOperators(engine, operators), nil
}

// ExecuteWorkflow 通过工作流运行时 Provider 执行工作流。
func ExecuteWorkflow(ctx context.Context, engine *models.Engine, req plugin.WorkflowExecuteRequest) (*plugin.WorkflowExecuteResult, error) {
	p, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}
	workflowProvider, ok := p.(plugin.WorkflowRuntimeProvider)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not implement WorkflowRuntimeProvider", engine.EngineType)
	}
	return workflowProvider.ExecuteWorkflow(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), req)
}

func toModelOperators(engine *models.Engine, operators []plugin.OperatorDescriptor) []models.OperatorDescriptor {
	result := make([]models.OperatorDescriptor, 0, len(operators))
	for _, op := range operators {
		result = append(result, models.OperatorDescriptor{
			ID:                  firstNonEmpty(op.ID, op.Name),
			Name:                op.Name,
			DisplayName:         op.DisplayName,
			Type:                op.Type,
			Category:            op.Category,
			Description:         op.Description,
			BriefDescription:    op.BriefDescription,
			DetailedDescription: op.DetailedDescription,
			Parameters:          toModelParameters(op.Parameters),
			Inputs:              toStringInputs(op.Inputs),
			OutputPorts:         toModelOutputPorts(op.OutputPorts),
			Module:              firstNonEmpty(op.Module, engine.EngineType),
		})
	}
	return result
}

func toModelParameters(parameters []plugin.ParameterDescriptor) []models.ParameterDescriptor {
	result := make([]models.ParameterDescriptor, 0, len(parameters))
	for _, param := range parameters {
		result = append(result, models.ParameterDescriptor{
			Name:        param.Name,
			Type:        param.Type,
			Required:    param.Required,
			Default:     param.Default,
			Description: param.Description,
			Enum:        param.Enum,
			Min:         param.Min,
			Max:         param.Max,
			Pattern:     param.Pattern,
			ItemType:    param.ItemType,
			Properties:  toModelParameterMap(param.Properties),
			DependsOn:   param.DependsOn,
			ShowWhen:    param.ShowWhen,
			Notes:       param.Notes,
			UIType:      param.UIType,
			UIConfig:    param.UIConfig,
		})
	}
	return result
}

func toModelParameterMap(parameters map[string]plugin.ParameterDescriptor) map[string]models.ParameterDescriptor {
	if len(parameters) == 0 {
		return nil
	}
	result := make(map[string]models.ParameterDescriptor, len(parameters))
	for name, param := range parameters {
		result[name] = models.ParameterDescriptor{
			Name:        param.Name,
			Type:        param.Type,
			Required:    param.Required,
			Default:     param.Default,
			Description: param.Description,
			Enum:        param.Enum,
			Min:         param.Min,
			Max:         param.Max,
			Pattern:     param.Pattern,
			ItemType:    param.ItemType,
			Properties:  toModelParameterMap(param.Properties),
			DependsOn:   param.DependsOn,
			ShowWhen:    param.ShowWhen,
			Notes:       param.Notes,
			UIType:      param.UIType,
			UIConfig:    param.UIConfig,
		}
	}
	return result
}

func toStringInputs(inputs []interface{}) []string {
	if len(inputs) == 0 {
		return nil
	}
	result := make([]string, 0, len(inputs))
	for _, input := range inputs {
		switch v := input.(type) {
		case string:
			result = append(result, v)
		case map[string]interface{}:
			if name, _ := v["name"].(string); name != "" {
				result = append(result, name)
			} else if typ, _ := v["type"].(string); typ != "" {
				result = append(result, typ)
			}
		default:
			if text := fmt.Sprintf("%v", v); text != "" {
				result = append(result, text)
			}
		}
	}
	return result
}

func toModelOutputPorts(ports []plugin.OutputPortDescriptor) []models.OutputPortDescriptor {
	result := make([]models.OutputPortDescriptor, 0, len(ports))
	for _, port := range ports {
		result = append(result, models.OutputPortDescriptor{
			Name:        port.Name,
			Type:        port.Type,
			Description: port.Description,
			IsDefault:   port.IsDefault,
		})
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// ListCatalogChildren 列出指定 catalog 路径下的实时子节点。
func ListCatalogChildren(ctx context.Context, engine *models.Engine, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	pluginEngine := toPluginEngine(engine)

	p, err := plugin.Get(pluginEngine.EngineType)
	if err != nil {
		return nil, err
	}
	modelProvider, ok := p.(plugin.CatalogModelProvider)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not implement CatalogModelProvider", pluginEngine.EngineType)
	}
	if len(parent.Segments) == 0 {
		return []plugin.CatalogEntry{
			plugin.CatalogRootEntry(modelProvider.CatalogModel(), engine.ID, engine.Name),
		}, nil
	}
	if parent.Version == "" {
		parent.Version = plugin.CatalogPathVersion
	}
	if parent.EngineID == 0 {
		parent.EngineID = pluginEngine.ID
	}
	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not implement CatalogProvider", pluginEngine.EngineType)
	}
	return catalogProvider.ListChildren(ctx, pluginEngine.ConnectionInfo, parent, opts)
}

// DescribeCatalogFacts 描述 catalog leaf 的 engine-native facts。
func DescribeCatalogFacts(ctx context.Context, engine *models.Engine, path plugin.CatalogPath, opts plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	return plugin.DescribeCatalogFacts(ctx, toPluginEngine(engine), path, opts)
}

// DescribeNamedCatalogFacts 描述指定命名空间下的具名 tabular catalog leaf facts。
func DescribeNamedCatalogFacts(ctx context.Context, engine *models.Engine, namespace, item string, opts plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	return plugin.DescribeNamedCatalogFacts(ctx, toPluginEngine(engine), namespace, item, opts)
}

// CountItemRows 获取 tabular 数据项行数。
func CountItemRows(ctx context.Context, engine *models.Engine, namespace, item string) (int64, error) {
	return plugin.CountItemRows(ctx, toPluginEngine(engine), namespace, item)
}

// ============ 统一查询执行 ============

// SupportsDirectQuery 检查引擎是否实现了非 SQL 原生查询运行时（MongoDB/Neo4j 等）
func SupportsDirectQuery(engineType string) bool {
	p, err := plugin.Get(engineType)
	if err != nil {
		return false
	}
	if _, ok := p.(plugin.SQLQueryRuntimeProvider); ok {
		return false
	}
	if qp, ok := p.(plugin.QueryRuntimeProvider); ok {
		if _, isSQLRuntime := qp.(plugin.SQLQueryRuntimeProvider); !isSQLRuntime {
			return true
		}
	}
	if _, ok := p.(plugin.GraphQueryProvider); ok {
		return true
	}
	return false
}

// GenerateSampleQuery 生成该引擎一个可直接执行的样例查询
func GenerateSampleQuery(ctx context.Context, engine *models.Engine) (query string, language string) {
	engineType := strings.ToLower(engine.EngineType)

	sampleCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	p, err := plugin.Get(engineType)
	if err == nil {
		connInfo := plugin.ConnectionInfo(engine.ConnectionInfo)

		// SQL 表格引擎优先通过实时 Catalog 发现真实数据表，避免默认示例生成不可执行的占位 SQL。
		if _, ok := p.(plugin.SQLQueryRuntimeProvider); ok {
			if cp, ok := p.(plugin.CatalogProvider); ok {
				if q, ok := generateCatalogSampleQuery(sampleCtx, p, cp, connInfo, engine.ID, engineType); ok {
					return q, "sql"
				}
			}
			return "SELECT 1", "sql"
		}

		// 原生查询引擎（MongoDB/Neo4j 等）：插件自带 GenerateSampleQuery。
		if qp, ok := p.(plugin.QueryRuntimeProvider); ok {
			return qp.GenerateSampleQuery(sampleCtx, connInfo, plugin.SampleQueryOptions{})
		}

		// 非 QueryRuntime 的表格引擎兜底仍通过 CatalogProvider 发现第一张可查询表。
		if cp, ok := p.(plugin.CatalogProvider); ok {
			if q, ok := generateCatalogSampleQuery(sampleCtx, p, cp, connInfo, engine.ID, engineType); ok {
				return q, "sql"
			}
		}
	}

	return "SELECT 1", "sql"
}

func generateCatalogSampleQuery(ctx context.Context, enginePlugin plugin.EnginePlugin, cp plugin.CatalogProvider, connInfo plugin.ConnectionInfo, engineID uint, engineType string) (string, bool) {
	modelProvider, ok := enginePlugin.(plugin.CatalogModelProvider)
	if !ok {
		return "", false
	}
	namespaces, err := cp.ListChildren(ctx, connInfo, plugin.CatalogRootPath(modelProvider.CatalogModel(), engineID), plugin.ListOptions{})
	if err != nil {
		return "", false
	}

	var fallbackNamespace, fallbackItem string
	for _, namespace := range namespaces {
		if namespace.Role != plugin.CatalogRoleBranch {
			continue
		}

		items, err := cp.ListChildren(ctx, connInfo, namespace.Path, plugin.ListOptions{})
		if err != nil {
			continue
		}

		for _, item := range items {
			if item.Role != plugin.CatalogRoleLeaf {
				continue
			}

			if fallbackItem == "" {
				fallbackNamespace = namespace.Name
				fallbackItem = item.Name
			}
			if catalogEntryRowCount(item) > 0 {
				return tableSampleSQL(engineType, namespace.Name, item.Name), true
			}
		}
	}

	if fallbackItem != "" {
		return tableSampleSQL(engineType, fallbackNamespace, fallbackItem), true
	}
	return "", false
}

func tableSampleSQL(engineType, namespace, table string) string {
	return sqldialect.SelectAllSampleSQL(engineType, namespace, table, 10)
}

func catalogEntryRowCount(entry plugin.CatalogEntry) int64 {
	if entry.Table == nil || entry.Table.RowCount == nil {
		return 0
	}
	return *entry.Table.RowCount
}

// ExecuteQuery 统一查询执行入口（适用于所有引擎类型）
//
// 路由规则（按优先级）：
//  1. 引擎实现了 QueryRuntimeProvider（MongoDB/Neo4j）→ 委托给插件原生执行
//  2. engineType == "spark" → gohive Thrift 协议执行
//  3. 其他 SQL 引擎（PostgreSQL/MySQL/Doris/ClickHouse）→ GORM 连接池执行
func ExecuteQuery(ctx context.Context, engine *models.Engine, query string) (*plugin.QueryResult, error) {
	engineType := strings.ToLower(engine.EngineType)
	queryOptions := plugin.QueryOptions{
		EngineID:   engine.ID,
		EngineType: engine.EngineType,
	}

	// 1. 原生查询运行时（MongoDB MQL、Neo4j Cypher 等）
	p, err := plugin.Get(engineType)
	if err == nil {
		if qp, ok := p.(plugin.QueryRuntimeProvider); ok {
			if _, isSQLRuntime := qp.(plugin.SQLQueryRuntimeProvider); !isSQLRuntime {
				return qp.ExecuteRuntimeQuery(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), plugin.QueryRequest{
					Language: firstQueryLanguage(qp.QueryLanguages()),
					Query:    query,
					Options:  queryOptions,
				})
			}
		}
	}

	// 2. Spark SQL（gohive Thrift 协议）
	if engineType == "spark" {
		return executeSparkQuery(ctx, engine, query)
	}

	// 3. 标准 SQL 运行时。当前通过 QueryOptions 传入 engine 上下文，以便复用连接池。
	if p != nil {
		if sqlRuntime, ok := p.(plugin.SQLQueryRuntimeProvider); ok {
			return sqlRuntime.ExecuteSQL(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), query, queryOptions)
		}
	}

	// 4. 标准 SQL 兜底（GORM 连接池）
	return executeSQLQuery(ctx, engine, query)
}

// ExecuteGraphQuery 统一图查询执行入口
// 对支持 GraphQueryProvider 的引擎（Neo4j 等）同时返回表格数据和图结构数据（节点/关系）
// 对其他引擎回退到 ExecuteQuery 并包装结果（GraphData 为 nil）
func ExecuteGraphQuery(ctx context.Context, engine *models.Engine, query string) (*plugin.GraphQueryResult, error) {
	engineType := strings.ToLower(engine.EngineType)

	p, err := plugin.Get(engineType)
	if err == nil {
		if gqp, ok := p.(plugin.GraphQueryProvider); ok {
			return gqp.ExecuteGraphQuery(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), query, plugin.QueryOptions{
				EngineID:   engine.ID,
				EngineType: engine.EngineType,
			})
		}
	}

	// 回退：普通查询，无图数据
	qr, err := ExecuteQuery(ctx, engine, query)
	if err != nil {
		return nil, err
	}
	return &plugin.GraphQueryResult{QueryResult: *qr}, nil
}

// ExecuteDML 执行 DML 语句（INSERT/UPDATE/DELETE），仅适用于 SQL 引擎，返回影响行数
// NoSQL 引擎的写操作请使用 ExecuteQuery（在命令中包含写操作即可）
func ExecuteDML(ctx context.Context, engine *models.Engine, query string) (int64, error) {
	db, err := GetOrCreatePool(engine, DefaultPoolConfig())
	if err != nil {
		return 0, fmt.Errorf("获取连接池失败：%w", err)
	}
	result := db.WithContext(ctx).Exec(query)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// executeSQLQuery 标准 SQL 引擎执行（PostgreSQL/MySQL/Doris/ClickHouse），使用 GORM 连接池
func executeSQLQuery(ctx context.Context, engine *models.Engine, query string) (*plugin.QueryResult, error) {
	db, err := GetOrCreatePool(engine, DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("获取连接池失败：%w", err)
	}

	rows, err := db.WithContext(ctx).Raw(query).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("获取列名失败：%w", err)
	}

	var resultRows []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("扫描行失败：%w", err)
		}
		row := make(map[string]interface{}, len(columns))
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		resultRows = append(resultRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历结果失败：%w", err)
	}

	return &plugin.QueryResult{Columns: columns, Rows: resultRows}, nil
}

func firstQueryLanguage(languages []string) string {
	if len(languages) == 0 {
		return ""
	}
	return languages[0]
}

// executeSparkQuery 通过 gohive Thrift 协议执行 Spark SQL
// 逻辑从 develop/backend/internal/service/sql_engine_service.go 迁移而来
func executeSparkQuery(ctx context.Context, engine *models.Engine, query string) (*plugin.QueryResult, error) {
	connInfo := engine.ConnectionInfo

	host, _ := connInfo["host"].(string)
	host = strings.TrimPrefix(strings.TrimPrefix(host, "http://"), "https://")

	portRaw := connInfo["port"]
	var port int
	switch v := portRaw.(type) {
	case float64:
		port = int(v)
	case int:
		port = v
	case string:
		port, _ = strconv.Atoi(v)
	}
	if port == 0 {
		port = 10000
	}

	database, _ := connInfo["database"].(string)
	if database == "" {
		database = "default"
	}
	user, _ := connInfo["user"].(string)
	password, _ := connInfo["password"].(string)

	if host == "" {
		return nil, fmt.Errorf("Spark 引擎缺少 host 配置")
	}

	configuration := gohive.NewConnectConfiguration()
	if user != "" {
		configuration.Username = user
		if password != "" {
			configuration.Password = password
		}
	}
	configuration.ConnectTimeout = 30 * time.Second
	configuration.SocketTimeout = 30 * time.Second

	connection, err := gohive.Connect(host, port, "NONE", configuration)
	if err != nil {
		return nil, fmt.Errorf("连接 Spark Thrift Server 失败：%w", err)
	}
	defer connection.Close()

	cursor := connection.Cursor()

	if database != "default" && database != "" {
		cursor.Exec(ctx, fmt.Sprintf("USE `%s`", database))
		if cursor.Err != nil {
			return nil, fmt.Errorf("切换数据库失败：%w", cursor.Err)
		}
	}

	cursor.Exec(ctx, query)
	if cursor.Err != nil {
		return nil, fmt.Errorf("执行 Spark SQL 失败：%w", cursor.Err)
	}

	var resultRows []map[string]interface{}
	var columns []string

	for cursor.HasMore(ctx) {
		row := cursor.RowMap(ctx)
		if cursor.Err != nil {
			return nil, fmt.Errorf("读取 Spark 结果失败：%w", cursor.Err)
		}
		if len(columns) == 0 {
			for k := range row {
				columns = append(columns, k)
			}
		}
		resultRows = append(resultRows, row)
	}

	return &plugin.QueryResult{Columns: columns, Rows: resultRows}, nil
}
