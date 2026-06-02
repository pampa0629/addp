package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AssetClient 资产管理模块客户端
// 供其他模块（如 portal）调用 asset API 使用
type AssetClient struct {
	baseURL     string
	httpClient  *http.Client
	authToken   string
	internalKey string
}

func NewAssetClient(baseURL string) *AssetClient {
	return &AssetClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func NewAssetClientWithToken(baseURL, token string) *AssetClient {
	return &AssetClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		authToken:  token,
	}
}

func NewAssetClientWithInternalKey(baseURL, internalKey string) *AssetClient {
	return &AssetClient{
		baseURL:     baseURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		internalKey: internalKey,
	}
}

func (c *AssetClient) addAuth(req *http.Request) {
	if c.internalKey != "" {
		req.Header.Set("X-Internal-API-Key", c.internalKey)
	} else if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
}

// ============================================================
// 核心数据结构
// ============================================================

// AssetStatus 资产状态
type AssetStatus = string

const (
	AssetStatusDraft     AssetStatus = "draft"     // 草稿（自动发现后默认状态）
	AssetStatusPublished AssetStatus = "published" // 已上架（管理员直接上架）
	AssetStatusOffline   AssetStatus = "offline"   // 已下架
)

// ApplicationStatus 申请状态
type ApplicationStatus = string

const (
	ApplicationStatusPending  ApplicationStatus = "pending"  // 待审批
	ApplicationStatusApproved ApplicationStatus = "approved" // 已批准
	ApplicationStatusRejected ApplicationStatus = "rejected" // 已拒绝
	ApplicationStatusRevoked  ApplicationStatus = "revoked"  // 已撤销
)

// AssetSummary 资产摘要（用于列表展示）
type AssetSummary struct {
	ID          int64       `json:"id"`
	TenantID    int64       `json:"tenant_id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	TypeID      int64       `json:"type_id"`
	TypeName    string      `json:"type_name"`
	CatalogID   *int64      `json:"catalog_id,omitempty"`
	CatalogName string      `json:"catalog_name,omitempty"`
	Tags        []string    `json:"tags"`
	Status      AssetStatus `json:"status"`
	OwnerID     int64       `json:"owner_id"`
	OwnerName   string      `json:"owner_name"`
	CreatedAt   string      `json:"created_at"`
	UpdatedAt   string      `json:"updated_at"`
}

// AssetDetail 资产详情（含扩展字段）
type AssetDetail struct {
	AssetSummary
	SourceModule    string          `json:"source_module"`
	SourceReference string          `json:"source_reference"`
	ExtFields       json.RawMessage `json:"ext_fields"` // 保留原始 JSON，兼容数组或对象格式
}

// AssetListResponse 资产列表响应
type AssetListResponse struct {
	Total int64          `json:"total"`
	Items []AssetSummary `json:"data"` // SendPaginatedResponse 返回 "data" key
}

// AssetQueryOptions 资产查询选项
type AssetQueryOptions struct {
	CatalogID *int64 // nil 不过滤；-1 只看未归类
	Status    AssetStatus
	Keyword   string
	TypeID    int64
	Page      int
	PageSize  int
}

// TypeStat 资产类型统计
type TypeStat struct {
	TypeID   int64  `json:"type_id"`
	TypeCode string `json:"type_code"`
	TypeName string `json:"type_name"`
	Count    int64  `json:"count"`
}

// AssetStatsResponse 资产统计响应
type AssetStatsResponse struct {
	TypeStats []TypeStat `json:"type_stats"`
	Total     int64      `json:"total"`
}

// ApplicationRequest 申请记录（含关联授权信息）
type ApplicationRequest struct {
	ID          int64             `json:"id"`
	TenantID    int64             `json:"tenant_id"`
	AssetID     int64             `json:"asset_id"`
	AssetName   string            `json:"asset_name"`
	ApplicantID int64             `json:"applicant_id"`
	Reason      string            `json:"reason"`
	DurationDay int               `json:"duration_day"`
	Status      ApplicationStatus `json:"status"`
	ReviewNote  string            `json:"review_note"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
	// 关联授权信息（approved 申请才有值）
	AuthID        *int64 `json:"auth_id,omitempty"`
	AuthIsActive  *bool  `json:"auth_is_active,omitempty"`
	AuthExpiresAt string `json:"auth_expires_at,omitempty"`
	AuthRevokedAt string `json:"auth_revoked_at,omitempty"`
}

// CreateApplicationRequest 提交申请的参数
type CreateApplicationRequest struct {
	AssetID     int64  `json:"asset_id"`
	ApplicantID int64  `json:"applicant_id"`
	Reason      string `json:"reason"`
	DurationDay int    `json:"duration_day"`
}

// Authorization 授权记录
type Authorization struct {
	ID         int64  `json:"id"`
	TenantID   int64  `json:"tenant_id"`
	AssetID    int64  `json:"asset_id"`
	AssetName  string `json:"asset_name"`
	UserID     int64  `json:"user_id"`
	Credential string `json:"credential"`
	ExpiresAt  string `json:"expires_at"`
	IsActive   bool   `json:"is_active"`
	CreatedAt  string `json:"created_at"`
}

// CatalogEntry 目录树节点
type CatalogEntry struct {
	ID       int64          `json:"id"`
	Name     string         `json:"name"`
	ParentID *int64         `json:"parent_id,omitempty"`
	Children []CatalogEntry `json:"children,omitempty"`
	Count    int64          `json:"count"` // 该分类下资产数量
}

// ============================================================
// API 方法
// ============================================================

// GetAssets 获取资产列表（支持分页和过滤）
func (c *AssetClient) GetAssets(tenantID int64, opts AssetQueryOptions) (*AssetListResponse, error) {
	url := fmt.Sprintf("%s/api/v1/asset/assets", c.baseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	q := req.URL.Query()
	if opts.CatalogID != nil {
		q.Set("catalog_id", fmt.Sprintf("%d", *opts.CatalogID))
	}
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if opts.Keyword != "" {
		q.Set("keyword", opts.Keyword)
	}
	if opts.TypeID > 0 {
		q.Set("type_id", fmt.Sprintf("%d", opts.TypeID))
	}
	if opts.Page > 0 {
		q.Set("page", fmt.Sprintf("%d", opts.Page))
	}
	if opts.PageSize > 0 {
		q.Set("page_size", fmt.Sprintf("%d", opts.PageSize))
	}
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asset api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result AssetListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// GetAssetDetail 获取资产详情
func (c *AssetClient) GetAssetDetail(assetID int64, tenantID int64) (*AssetDetail, error) {
	url := fmt.Sprintf("%s/api/v1/asset/assets/%d", c.baseURL, assetID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("asset %d not found", assetID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asset api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result AssetDetail
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// GetCatalogs 获取目录树
func (c *AssetClient) GetCatalogs(tenantID int64) ([]CatalogEntry, error) {
	url := fmt.Sprintf("%s/api/v1/asset/catalogs/tree", c.baseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asset api returned status %d: %s", resp.StatusCode, string(body))
	}

	var wrapper struct {
		Data []CatalogEntry `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return wrapper.Data, nil
}

// CreateApplication 提交资产申请（由 portal 用户触发）
// Phase 4 实现完整内部逻辑
func (c *AssetClient) CreateApplication(tenantID int64, applyReq CreateApplicationRequest) (*ApplicationRequest, error) {
	url := fmt.Sprintf("%s/api/v1/asset/applications", c.baseURL)

	body, err := json.Marshal(applyReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asset api returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result ApplicationRequest
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// ApplicationListResponse 申请列表响应（分页格式）
type ApplicationListResponse struct {
	Total int64                `json:"total"`
	Items []ApplicationRequest `json:"data"`
}

// GetApplications 获取申请列表（供 portal "我的申请"使用）
// assetID 为 0 时不过滤资产
func (c *AssetClient) GetApplications(tenantID int64, applicantID int64, assetID int64) ([]ApplicationRequest, error) {
	url := fmt.Sprintf("%s/api/v1/asset/applications", c.baseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	q := req.URL.Query()
	if applicantID > 0 {
		q.Set("applicant_id", fmt.Sprintf("%d", applicantID))
	}
	if assetID > 0 {
		q.Set("asset_id", fmt.Sprintf("%d", assetID))
	}
	q.Set("page_size", "100")
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asset api returned status %d: %s", resp.StatusCode, string(body))
	}

	var wrapper ApplicationListResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return wrapper.Items, nil
}

// GetApplyStatus 查询当前用户对某资产的申请/授权状态
// 返回值："none" | "pending" | "approved"
func (c *AssetClient) GetApplyStatus(tenantID int64, assetID int64, userID int64) (string, error) {
	// 先检查是否有有效授权
	authURL := fmt.Sprintf("%s/api/v1/asset/authorizations", c.baseURL)
	authReq, err := http.NewRequest("GET", authURL, nil)
	if err != nil {
		return "none", nil
	}
	c.addAuth(authReq)
	authReq.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))
	q := authReq.URL.Query()
	q.Set("user_id", fmt.Sprintf("%d", userID))
	q.Set("asset_id", fmt.Sprintf("%d", assetID))
	q.Set("is_active", "true")
	q.Set("page_size", "1")
	authReq.URL.RawQuery = q.Encode()

	authResp, err := c.httpClient.Do(authReq)
	if err == nil && authResp.StatusCode == http.StatusOK {
		var wrapper struct {
			Total int64 `json:"total"`
		}
		if jsonErr := json.NewDecoder(authResp.Body).Decode(&wrapper); jsonErr == nil && wrapper.Total > 0 {
			authResp.Body.Close()
			return "approved", nil
		}
		authResp.Body.Close()
	}

	// 再检查是否有 pending 申请
	apps, err := c.GetApplications(tenantID, userID, assetID)
	if err != nil {
		return "none", nil // 查询失败降级为 none
	}
	for _, app := range apps {
		if app.Status == ApplicationStatusPending {
			return "pending", nil
		}
	}
	return "none", nil
}

// ============================================================
// 评价相关数据结构与方法
// ============================================================

// RatingItem 评价记录（含用户名）
type RatingItem struct {
	ID        int64    `json:"id"`
	AssetID   int64    `json:"asset_id"`
	UserID    int64    `json:"user_id"`
	UserName  string   `json:"user_name"`
	Score     float32  `json:"score"`
	Comment   string   `json:"comment"`
	Tags      []string `json:"tags"`
	IsHandled bool     `json:"is_handled"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// RatingListResponse 评价列表响应
type RatingListResponse struct {
	Total int64        `json:"total"`
	Items []RatingItem `json:"data"`
}

// UpsertRatingRequest 提交/更新评价请求
type UpsertRatingRequest struct {
	AssetID int64    `json:"asset_id"`
	UserID  int64    `json:"user_id"`
	Score   float32  `json:"score"`
	Comment string   `json:"comment"`
	Tags    []string `json:"tags"`
}

// GetRatings 获取资产评价列表
func (c *AssetClient) GetRatings(tenantID int64, assetID int64) ([]RatingItem, int64, error) {
	url := fmt.Sprintf("%s/api/v1/asset/ratings", c.baseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	q := req.URL.Query()
	q.Set("asset_id", fmt.Sprintf("%d", assetID))
	q.Set("page_size", "50")
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("asset api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result RatingListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, fmt.Errorf("failed to decode response: %w", err)
	}
	return result.Items, result.Total, nil
}

// UpsertRating 提交或更新评价（upsert 语义）
func (c *AssetClient) UpsertRating(tenantID int64, req UpsertRatingRequest) (*RatingItem, error) {
	url := fmt.Sprintf("%s/api/v1/asset/ratings", c.baseURL)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asset api returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result RatingItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// GetAssetStats 获取已上架资产的统计数据（各类型数量 + 总计）
func (c *AssetClient) GetAssetStats(tenantID int64) (*AssetStatsResponse, error) {
	url := fmt.Sprintf("%s/api/v1/asset/assets/stats", c.baseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asset api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result AssetStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}
