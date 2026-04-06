package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ModelClient Model 服务客户端（仅用于服务间调用）
type ModelClient struct {
	baseURL     string
	httpClient  *http.Client
	internalKey string
	tenantID    *uint
}

// NewModelClientWithInternalKey 创建 Model 客户端（服务间调用方式）
func NewModelClientWithInternalKey(baseURL, internalKey string) *ModelClient {
	return &ModelClient{
		baseURL:     baseURL,
		internalKey: internalKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetTenantID 设置租户 ID
func (c *ModelClient) SetTenantID(tenantID *uint) {
	c.tenantID = tenantID
}

func (c *ModelClient) addAuth(req *http.Request) {
	if c.internalKey != "" {
		req.Header.Set("X-Internal-API-Key", c.internalKey)
		if c.tenantID != nil {
			req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", *c.tenantID))
		}
	}
}

func (c *ModelClient) get(path string) ([]byte, error) {
	url := c.baseURL + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.addAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("model service error %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// ModelEntity Model 模块的业务实体 DTO
type ModelEntity struct {
	ID          uint                   `json:"id"`
	Name        string                 `json:"name"`
	Code        string                 `json:"code"`
	Description string                 `json:"description"`
	Attributes  []ModelEntityAttribute `json:"attributes"`
}

// ModelEntityAttribute 实体属性 DTO
type ModelEntityAttribute struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	DataType   string `json:"data_type"`
	Nullable   bool   `json:"nullable"`
	IsPK       bool   `json:"is_pk"`
	SortOrder  int    `json:"sort_order"`
}

// ModelEntityRelation 实体关系 DTO
type ModelEntityRelation struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	SourceEntity uint   `json:"source_entity"`
	TargetEntity uint   `json:"target_entity"`
	RelationType string `json:"relation_type"` // one_to_one/one_to_many/many_to_many
}

// ListEntities 获取实体列表（含属性）
func (c *ModelClient) ListEntities(tenantID uint) ([]ModelEntity, error) {
	savedTenantID := c.tenantID
	c.SetTenantID(&tenantID)
	defer func() { c.tenantID = savedTenantID }()

	// 获取实体列表（不分页，page_size=1000 保证全量）
	body, err := c.get("/api/v1/model/entities?page_size=1000")
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []ModelEntity `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse entities response: %w", err)
	}
	return resp.Data, nil
}

// GetEntityWithAttributes 获取单个实体（含属性）
func (c *ModelClient) GetEntityWithAttributes(tenantID, entityID uint) (*ModelEntity, error) {
	savedTenantID := c.tenantID
	c.SetTenantID(&tenantID)
	defer func() { c.tenantID = savedTenantID }()

	// 获取实体基本信息
	body, err := c.get(fmt.Sprintf("/api/v1/model/entities/%d", entityID))
	if err != nil {
		return nil, err
	}
	var entity ModelEntity
	if err := json.Unmarshal(body, &entity); err != nil {
		return nil, fmt.Errorf("parse entity: %w", err)
	}

	// 获取属性列表
	attrBody, err := c.get(fmt.Sprintf("/api/v1/model/entities/%d/attributes", entityID))
	if err == nil {
		var attrs []ModelEntityAttribute
		if json.Unmarshal(attrBody, &attrs) == nil {
			entity.Attributes = attrs
		}
	}
	return &entity, nil
}

// ListEntityRelations 获取实体关系列表
func (c *ModelClient) ListEntityRelations(tenantID uint) ([]ModelEntityRelation, error) {
	savedTenantID := c.tenantID
	c.SetTenantID(&tenantID)
	defer func() { c.tenantID = savedTenantID }()

	body, err := c.get("/api/v1/model/entity-relations")
	if err != nil {
		return nil, err
	}
	var relations []ModelEntityRelation
	if err := json.Unmarshal(body, &relations); err != nil {
		return nil, fmt.Errorf("parse entity relations: %w", err)
	}
	return relations, nil
}
