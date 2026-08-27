package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AssetClient 是 Portal 同步转发当前用户 Bearer 的消费面客户端。
// 客户端本身不保存任何用户凭据。
type AssetClient struct {
	baseURL    string
	httpClient *http.Client
}

type AssetAPIError struct {
	Method     string
	Path       string
	StatusCode int
}

func (e *AssetAPIError) Error() string {
	return fmt.Sprintf("Asset API %s %s returned HTTP %d", e.Method, e.Path, e.StatusCode)
}

func AssetAPIStatusCode(err error) (int, bool) {
	var apiError *AssetAPIError
	if errors.As(err, &apiError) {
		return apiError.StatusCode, true
	}
	return 0, false
}

func NewAssetClient(baseURL string) *AssetClient {
	return &AssetClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type AssetStatus = string

const (
	AssetStatusDraft     AssetStatus = "draft"
	AssetStatusPublished AssetStatus = "published"
	AssetStatusOffline   AssetStatus = "offline"
)

type ApplicationStatus = string

const (
	ApplicationStatusPending  ApplicationStatus = "pending"
	ApplicationStatusApproved ApplicationStatus = "approved"
	ApplicationStatusRejected ApplicationStatus = "rejected"
	ApplicationStatusRevoked  ApplicationStatus = "revoked"
)

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
	Version     int64       `json:"version"`
	CreatedAt   string      `json:"created_at"`
	UpdatedAt   string      `json:"updated_at"`
}

type AssetDetail struct {
	AssetSummary
	Components []AssetComponent `json:"components"`
	ExtFields  json.RawMessage  `json:"ext_fields"`
}

type AssetComponent struct {
	ID             int64  `json:"id"`
	AssetID        int64  `json:"asset_id"`
	CatalogEntryID string `json:"catalog_entry_id"`
	Role           string `json:"role"`
	SortOrder      int    `json:"sort_order"`
}

type AssetListResponse struct {
	Total int64          `json:"total"`
	Items []AssetSummary `json:"data"`
}

type AssetQueryOptions struct {
	CatalogID *int64
	Keyword   string
	TypeID    int64
	Page      int
	PageSize  int
}

type TypeStat struct {
	TypeID   int64  `json:"type_id"`
	TypeCode string `json:"type_code"`
	TypeName string `json:"type_name"`
	Count    int64  `json:"count"`
}

type AssetStatsResponse struct {
	TypeStats []TypeStat `json:"type_stats"`
	Total     int64      `json:"total"`
}

type ApplicationRequest struct {
	ID            int64             `json:"id"`
	TenantID      int64             `json:"tenant_id"`
	AssetID       int64             `json:"asset_id"`
	AssetName     string            `json:"asset_name"`
	ApplicantID   int64             `json:"applicant_id"`
	Reason        string            `json:"reason"`
	DurationDay   int               `json:"duration_day"`
	Status        ApplicationStatus `json:"status"`
	ReviewNote    string            `json:"review_note"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
	AuthID        *int64            `json:"auth_id,omitempty"`
	AuthStatus    string            `json:"auth_status,omitempty"`
	AuthExpiresAt string            `json:"auth_expires_at,omitempty"`
	AuthRevokedAt string            `json:"auth_revoked_at,omitempty"`
	OpenPath      string            `json:"open_path,omitempty"`
}

type AssetConsumerAccessStatus struct {
	Status   string `json:"status"`
	OpenPath string `json:"open_path,omitempty"`
}

type CreateApplicationRequest struct {
	Reason      string `json:"reason"`
	DurationDay int    `json:"duration_day"`
}

type AssetCatalogTreeNode struct {
	ID       int64                  `json:"id"`
	Name     string                 `json:"name"`
	ParentID *int64                 `json:"parent_id,omitempty"`
	Children []AssetCatalogTreeNode `json:"children,omitempty"`
	Count    int64                  `json:"count"`
}

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

type UpsertRatingRequest struct {
	Score   float32  `json:"score"`
	Comment string   `json:"comment"`
	Tags    []string `json:"tags"`
}

type paginatedApplications struct {
	Total int64                `json:"total"`
	Items []ApplicationRequest `json:"data"`
}

type paginatedRatings struct {
	Total int64        `json:"total"`
	Items []RatingItem `json:"data"`
}

func (c *AssetClient) GetAssets(ctx context.Context, accessToken string, opts AssetQueryOptions) (*AssetListResponse, error) {
	query := url.Values{}
	if opts.CatalogID != nil {
		query.Set("catalog_id", strconv.FormatInt(*opts.CatalogID, 10))
	}
	if opts.Keyword != "" {
		query.Set("keyword", opts.Keyword)
	}
	if opts.TypeID > 0 {
		query.Set("type_id", strconv.FormatInt(opts.TypeID, 10))
	}
	if opts.Page > 0 {
		query.Set("page", strconv.Itoa(opts.Page))
	}
	if opts.PageSize > 0 {
		query.Set("page_size", strconv.Itoa(opts.PageSize))
	}
	var result AssetListResponse
	if err := c.do(ctx, accessToken, http.MethodGet, "/api/v1/asset/consumer/assets", query, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *AssetClient) GetAssetStats(ctx context.Context, accessToken string) (*AssetStatsResponse, error) {
	var result AssetStatsResponse
	if err := c.do(ctx, accessToken, http.MethodGet, "/api/v1/asset/consumer/assets/stats", nil, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *AssetClient) GetAssetDetail(ctx context.Context, accessToken string, assetID int64) (*AssetDetail, error) {
	var result AssetDetail
	path := fmt.Sprintf("/api/v1/asset/consumer/assets/%d", assetID)
	if err := c.do(ctx, accessToken, http.MethodGet, path, nil, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *AssetClient) GetCatalogs(ctx context.Context, accessToken string) ([]AssetCatalogTreeNode, error) {
	var result []AssetCatalogTreeNode
	if err := c.do(ctx, accessToken, http.MethodGet, "/api/v1/asset/consumer/catalogs", nil, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *AssetClient) CreateApplication(ctx context.Context, accessToken string, assetID int64, request CreateApplicationRequest) (*ApplicationRequest, error) {
	var result ApplicationRequest
	path := fmt.Sprintf("/api/v1/asset/consumer/assets/%d/applications", assetID)
	if err := c.do(ctx, accessToken, http.MethodPost, path, nil, request, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *AssetClient) GetApplications(ctx context.Context, accessToken string) ([]ApplicationRequest, error) {
	query := url.Values{"page_size": {"100"}}
	var result paginatedApplications
	if err := c.do(ctx, accessToken, http.MethodGet, "/api/v1/asset/consumer/applications", query, nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *AssetClient) GetApplyStatus(ctx context.Context, accessToken string, assetID int64) (*AssetConsumerAccessStatus, error) {
	var result AssetConsumerAccessStatus
	path := fmt.Sprintf("/api/v1/asset/consumer/assets/%d/application-status", assetID)
	if err := c.do(ctx, accessToken, http.MethodGet, path, nil, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *AssetClient) GetRatings(ctx context.Context, accessToken string, assetID int64) ([]RatingItem, int64, error) {
	query := url.Values{"page_size": {"50"}}
	var result paginatedRatings
	path := fmt.Sprintf("/api/v1/asset/consumer/assets/%d/ratings", assetID)
	if err := c.do(ctx, accessToken, http.MethodGet, path, query, nil, &result); err != nil {
		return nil, 0, err
	}
	return result.Items, result.Total, nil
}

func (c *AssetClient) UpsertRating(ctx context.Context, accessToken string, assetID int64, request UpsertRatingRequest) (*RatingItem, error) {
	var result RatingItem
	path := fmt.Sprintf("/api/v1/asset/consumer/assets/%d/ratings", assetID)
	if err := c.do(ctx, accessToken, http.MethodPost, path, nil, request, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *AssetClient) do(
	ctx context.Context,
	accessToken, method, path string,
	query url.Values,
	payload, result any,
) error {
	if c == nil || c.httpClient == nil || c.baseURL == "" {
		return errors.New("Asset client is not configured")
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return errors.New("Asset request requires a user access token")
	}

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode Asset request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create Asset request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if query != nil {
		request.URL.RawQuery = query.Encode()
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send Asset request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return &AssetAPIError{
			Method: method, Path: pathWithoutQuery(path), StatusCode: response.StatusCode,
		}
	}
	if result == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return fmt.Errorf("decode Asset response: %w", err)
	}
	return nil
}
