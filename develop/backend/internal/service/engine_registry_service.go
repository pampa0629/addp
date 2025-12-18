package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	commonModels "github.com/addp/common/models"
)

// EngineRegistryService 引擎注册服务
type EngineRegistryService struct {
	systemURL      string
	internalAPIKey string
	developURL     string
}

// NewEngineRegistryService 创建引擎注册服务
func NewEngineRegistryService(systemURL, internalAPIKey, developURL string) *EngineRegistryService {
	return &EngineRegistryService{
		systemURL:      systemURL,
		internalAPIKey: internalAPIKey,
		developURL:     developURL,
	}
}

// RegisterEngine 注册 Develop 引擎（单一引擎，支持 sql 和 workflow）
func (s *EngineRegistryService) RegisterEngine() error {
	registration := commonModels.CapabilityRegistrationRequest{
		UniqueIdentifier: "develop.default",
		Name:             "develop_engine",
		DisplayName:      "开发任务",
		ResourceType:     "compute_engine",
		IsBuiltin:        true,
		Description:      "开发工作台任务执行引擎，支持 SQL 查询和空间工作流",
		Capabilities: &commonModels.Capability{
			Compute: []commonModels.ComputeCapability{
				{
					Type:     "sql",
					DevModes: []string{"sql"},
					Features: []string{"async", "timeout", "result_preview", "multi_database"},
					SupportedSources: []string{"postgresql", "mysql", "doris", "clickhouse"},
				},
				{
					Type:     "workflow",
					DevModes: []string{"workflow"},
					Features: []string{"dag", "async", "spatial_operators", "memory_efficient"},
					SupportedSources: []string{"geojson", "shapefile", "postgis", "file"},
				},
			},
		},
		TaskAPIConfig: &commonModels.TaskAPIConfig{
			BaseURL: s.developURL,
			Endpoints: map[string]commonModels.APIEndpoint{
				// Create: 验证任务是否存在（GET /api/develop/items/{task_id}）
				"create": {
					Method: "GET",
					Path:   "/api/develop/items/{{.TaskID}}",
					ResponseMapping: &commonModels.ResponseMapping{
						TaskIDField: "id",
					},
				},
				// Execute: 执行任务（POST /api/develop/items/{task_id}/execute）
				"execute": {
					Method: "POST",
					Path:   "/api/develop/items/{{.TaskID}}/execute",
					ResponseMapping: &commonModels.ResponseMapping{
						TaskIDField: "execution_id",
					},
				},
				// Status: 查询执行状态（GET /api/develop/executions/{execution_id}）
				"status": {
					Method: "GET",
					Path:   "/api/develop/executions/{{.ExecutionID}}",
					ResponseMapping: &commonModels.ResponseMapping{
						StatusField:   "status",
						MessageField:  "error_message",
						ProgressField: "progress",
					},
				},
				// List: 任务列表（GET /api/develop/tasks/list）
				"list": {
					Method: "GET",
					Path:   "/api/develop/tasks/list",
					QueryParams: map[string]string{
						"unique_identifier": "{{.UniqueIdentifier}}",
						"page":              "{{.Page}}",
						"page_size":         "{{.PageSize}}",
					},
					ResponseMapping: &commonModels.ResponseMapping{
						DataField: "items",
					},
				},
			},
			Timeout: map[string]int{
				"create":  10,
				"execute": 600, // 统一超时 600s
				"status":  10,
				"list":    30,
			},
		},
		HealthCheckConfig: &commonModels.HealthCheckConfig{
			Endpoint: "/health",
			Timeout:  5,
			Interval: 60,
		},
	}

	return s.sendRegistration(&registration)
}

// sendRegistration 发送注册请求到 System
func (s *EngineRegistryService) sendRegistration(req *commonModels.CapabilityRegistrationRequest) error {
	bodyJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal registration: %w", err)
	}

	httpReq, err := http.NewRequest("POST", s.systemURL+"/internal/registry/capabilities", bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-API-Key", s.internalAPIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("registration failed with status %d", resp.StatusCode)
	}

	log.Printf("✅ Develop engine registered successfully (unique_identifier: develop.default)")
	return nil
}
