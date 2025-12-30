package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/addp/common/models"
	commonutils "github.com/addp/common/utils"
)

// SystemClient 系统服务客户端
type SystemClient struct {
	baseURL     string
	httpClient  *http.Client
	authToken   string // JWT Token (用于用户认证的 API)
	internalKey string // Internal API Key (用于服务间调用)
}

// NewSystemClient 创建系统客户端（用户认证方式）
func NewSystemClient(baseURL, authToken string) *SystemClient {
	return &SystemClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		authToken: authToken,
	}
}

// NewSystemClientWithInternalKey 创建系统客户端（服务间调用方式）
func NewSystemClientWithInternalKey(baseURL, internalKey string) *SystemClient {
	return &SystemClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		internalKey: internalKey,
	}
}

// addAuth 添加认证头（根据客户端类型选择 JWT 或 Internal Key）
func (c *SystemClient) addAuth(req *http.Request) {
	if c.internalKey != "" {
		// 服务间调用使用 Internal API Key
		req.Header.Set("X-Internal-API-Key", c.internalKey)
	} else if c.authToken != "" {
		// 用户调用使用 JWT Token
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
}

// GetEngine 获取引擎详情
func (c *SystemClient) GetEngine(engineID uint) (*models.Engine, error) {
	var url string
	// 如果使用内部 API Key，调用内部 API
	if c.internalKey != "" {
		url = fmt.Sprintf("%s/internal/engines/%d", c.baseURL, engineID)
	} else {
		url = fmt.Sprintf("%s/api/engines/%d", c.baseURL, engineID)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engine models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engine); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &engine, nil
}

// ListEngines 获取资源列表
func (c *SystemClient) ListEngines(engineType string, tenantID uint) ([]models.Engine, error) {
	var url string
	// 如果使用内部 API Key，调用内部 API
	if c.internalKey != "" {
		url = fmt.Sprintf("%s/internal/engines", c.baseURL)
	} else {
		url = fmt.Sprintf("%s/api/engines", c.baseURL)
	}

	queryAdded := false
	if engineType != "" {
		url += "?engine_type=" + engineType
		queryAdded = true
	}
	if tenantID > 0 {
		prefix := "?"
		if queryAdded || strings.Contains(url, "?") {
			prefix = "&"
		}
		url += fmt.Sprintf("%stenant_id=%d", prefix, tenantID)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engines []models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engines); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return engines, nil
}

// CreateEngine 创建资源（支持内部或用户接口）
func (c *SystemClient) CreateEngine(payload map[string]interface{}) (*models.Engine, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	var url string
	if c.internalKey != "" {
		url = fmt.Sprintf("%s/internal/engines", c.baseURL)
	} else {
		url = fmt.Sprintf("%s/api/engines", c.baseURL)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engine models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engine); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &engine, nil
}

// RegisterCapability 注册能力（服务间调用，无需 JWT）
func (c *SystemClient) RegisterCapability(req *models.CapabilityRegistrationRequest) error {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/internal/registry/capabilities", c.baseURL)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ListCapabilities 查询能力列表（支持过滤）
func (c *SystemClient) ListCapabilities(filters map[string]string) ([]*models.Engine, error) {
	url := fmt.Sprintf("%s/internal/registry/capabilities", c.baseURL)

	// 添加查询参数
	if len(filters) > 0 {
		queryParams := []string{}
		for k, v := range filters {
			queryParams = append(queryParams, fmt.Sprintf("%s=%s", k, v))
		}
		url += "?" + strings.Join(queryParams, "&")
	}

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engines []*models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engines); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return engines, nil
}

// GetCapabilityByIdentifier 根据 unique_identifier 查询能力
func (c *SystemClient) GetCapabilityByIdentifier(identifier string) (*models.Engine, error) {
	url := fmt.Sprintf("%s/internal/registry/capabilities/%s", c.baseURL, identifier)

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engine models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engine); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &engine, nil
}

// ListComputeEngines 查询所有具有计算能力的引擎
func (c *SystemClient) ListComputeEngines(filters map[string]string) ([]*models.Engine, error) {
	url := fmt.Sprintf("%s/internal/registry/compute-engines", c.baseURL)

	// 添加查询参数（如果需要）
	if len(filters) > 0 {
		queryParams := []string{}
		for k, v := range filters {
			queryParams = append(queryParams, fmt.Sprintf("%s=%s", k, v))
		}
		url += "?" + strings.Join(queryParams, "&")
	}

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engines []*models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engines); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return engines, nil
}

// ================ 能力过滤相关方法（新增） ================

// ListEnginesByCapability 通用方法：按能力过滤资源
func (c *SystemClient) ListEnginesByCapability(tenantID uint, storageTypes []string) ([]models.Engine, error) {
	// 只能使用内部 API
	if c.internalKey == "" {
		return nil, fmt.Errorf("capability filtering requires internal API key")
	}

	url := fmt.Sprintf("%s/internal/engines", c.baseURL)

	// 构建查询参数
	queryParams := []string{}
	if tenantID > 0 {
		queryParams = append(queryParams, fmt.Sprintf("tenant_id=%d", tenantID))
	}
	if len(storageTypes) > 0 {
		queryParams = append(queryParams, "storage_type="+strings.Join(storageTypes, ","))
	}

	if len(queryParams) > 0 {
		url += "?" + strings.Join(queryParams, "&")
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engines []models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engines); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return engines, nil
}

// ListRelationalDatabases 快捷方法：获取所有关系型数据库
func (c *SystemClient) ListRelationalDatabases(tenantID uint) ([]models.Engine, error) {
	return c.ListEnginesByCapability(tenantID, []string{"relational_db"})
}

// ListObjectStorages 快捷方法：获取所有对象存储
func (c *SystemClient) ListObjectStorages(tenantID uint) ([]models.Engine, error) {
	return c.ListEnginesByCapability(tenantID, []string{"object_storage"})
}

// ListScannableResources 快捷方法：获取所有可扫描的资源（元数据模块使用）
func (c *SystemClient) ListScannableResources(tenantID uint) ([]models.Engine, error) {
	return c.ListEnginesByCapability(tenantID, []string{"relational_db", "object_storage", "generic"})
}

// ListSQLQueryEngines 快捷方法：获取所有支持 SQL 查询的引擎（开发模块使用）
// 改用 dev_modes 过滤，不再依赖 compute.type
func (c *SystemClient) ListSQLQueryEngines(tenantID uint) ([]models.Engine, error) {
	// 1. 获取所有关系型数据库引擎
	allEngines, err := c.ListEnginesByCapability(tenantID, []string{"relational_db"})
	if err != nil {
		return nil, err
	}

	// 2. 过滤出支持 "sql" 开发模式的引擎
	sqlEngines := commonutils.FilterEnginesByDevMode(allEngines, "sql")

	return sqlEngines, nil
}

// ================ TaskProvider 相关方法（新增） ================

// ListTaskProviders 查询所有启用的任务提供者
func (c *SystemClient) ListTaskProviders() ([]*models.TaskProvider, error) {
	url := fmt.Sprintf("%s/internal/task-providers", c.baseURL)

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var providers []*models.TaskProvider
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return providers, nil
}

// GetTaskProvider 根据 module_name 查询任务提供者
func (c *SystemClient) GetTaskProvider(moduleName string) (*models.TaskProvider, error) {
	url := fmt.Sprintf("%s/internal/task-providers/%s", c.baseURL, moduleName)

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var provider models.TaskProvider
	if err := json.NewDecoder(resp.Body).Decode(&provider); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &provider, nil
}

// ================ 数据库元数据相关方法（新增） ================

// SchemaInfo 表示数据库Schema信息
type SchemaInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// TableInfo 表示数据库表信息
type TableInfo struct {
	Name        string `json:"name"`
	Schema      string `json:"schema"`
	Type        string `json:"type,omitempty"`        // TABLE, VIEW等
	Description string `json:"description,omitempty"`
}

// ListSchemas 列出指定资源的所有Schema/Database
func (c *SystemClient) ListSchemas(engineID uint) ([]SchemaInfo, error) {
	url := fmt.Sprintf("%s/api/engines/%d/schemas", c.baseURL, engineID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status  string       `json:"status"`
		Schemas []SchemaInfo `json:"schemas"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Schemas, nil
}

// ListTables 列出指定资源和Schema下的所有表
func (c *SystemClient) ListTables(engineID uint, schema string) ([]TableInfo, error) {
	url := fmt.Sprintf("%s/api/engines/%d/tables", c.baseURL, engineID)
	if schema != "" {
		url += "?schema=" + schema
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string      `json:"status"`
		Tables []TableInfo `json:"tables"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Tables, nil
}

// ================ 工作流引擎相关方法（新增） ================

// ListWorkflowEngines 获取支持 workflow 的计算引擎
func (c *SystemClient) ListWorkflowEngines(tenantID uint) ([]models.Engine, error) {
	endpoint := fmt.Sprintf("%s/internal/engines?tenant_id=%d", c.baseURL, tenantID)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engines []models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engines); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// 过滤出工作流引擎（使用 common/utils 的 SupportsDevMode 函数）
	filtered := make([]models.Engine, 0)
	for _, r := range engines {
		// 需要在这里导入 utils 包并使用 SupportsDevMode(&r, "workflow")
		// 但为了避免循环导入，直接在这里实现过滤逻辑
		if r.IsActive && r.Capabilities != nil && *r.Capabilities != "" {
			// 简单的 JSON 字符串匹配（临时方案，更好的做法是导入 utils）
			if strings.Contains(*r.Capabilities, "\"workflow\"") {
				filtered = append(filtered, r)
			}
		}
	}
	return filtered, nil
}

// ListSparkRuntimes 获取所有 Spark SQL 运行时
func (c *SystemClient) ListSparkRuntimes(tenantID uint) ([]models.Engine, error) {
	endpoint := fmt.Sprintf("%s/internal/engines?tenant_id=%d&engine_type=spark_sql", c.baseURL, tenantID)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engines []models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engines); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// 过滤出活跃的 Spark 运行时
	filtered := make([]models.Engine, 0)
	for _, r := range engines {
		if r.IsActive {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

// GetEngineByID 根据 ID 获取资源详情（简化别名）
func (c *SystemClient) GetEngineByID(engineID uint) (*models.Engine, error) {
	return c.GetEngine(engineID)
}

// DoRequest 发送通用HTTP请求（支持GET, POST, PUT, DELETE等）
// 用于调用各种System内部API
func (c *SystemClient) DoRequest(method, url string, payload interface{}, result interface{}) error {
	var bodyReader io.Reader
	if payload != nil {
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request returned status %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}
