package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ManagerContentClient struct{ tenantHTTPClient }

func NewManagerContentClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *ManagerContentClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &ManagerContentClient{tenantHTTPClient: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
}

func (c *ManagerContentClient) WithTenantID(tenantID uint) *ManagerContentClient {
	if c == nil {
		return nil
	}
	return &ManagerContentClient{tenantHTTPClient: c.tenantHTTPClient.withTenantID(tenantID)}
}

type ManagerContentField struct {
	Name            string `json:"name"`
	DataType        string `json:"data_type,omitempty"`
	ColumnType      string `json:"column_type,omitempty"`
	Comment         string `json:"comment,omitempty"`
	OrdinalPosition int    `json:"ordinal_position,omitempty"`
	IsNullable      bool   `json:"is_nullable,omitempty"`
	IsPrimaryKey    bool   `json:"is_primary_key,omitempty"`
	IsUniqueKey     bool   `json:"is_unique_key,omitempty"`
}

const (
	ManagerContentPayloadTechnicalMetadata = "technical_metadata"
	ManagerContentPayloadExtractedContent  = "extracted_content"
)

type ManagerContentDocument struct {
	DocumentID       string                 `json:"document_id"`
	PayloadKind      string                 `json:"payload_kind" enums:"technical_metadata,extracted_content"`
	ContentHash      string                 `json:"content_hash,omitempty"`
	Locator          string                 `json:"locator,omitempty"`
	EngineID         uint                   `json:"engine_id"`
	EngineName       string                 `json:"engine_name,omitempty"`
	EngineType       string                 `json:"engine_type,omitempty"`
	DataItemType     string                 `json:"data_item_type"`
	Name             string                 `json:"name"`
	FullName         string                 `json:"full_name,omitempty"`
	Description      string                 `json:"description,omitempty"`
	Tags             []string               `json:"tags,omitempty"`
	Schema           string                 `json:"schema,omitempty"`
	TableKind        string                 `json:"table_kind,omitempty"`
	Fields           []ManagerContentField  `json:"fields,omitempty"`
	RowCount         *int64                 `json:"row_count,omitempty"`
	Bucket           string                 `json:"bucket,omitempty"`
	Path             string                 `json:"path,omitempty"`
	SizeBytes        *int64                 `json:"size_bytes,omitempty"`
	ContentType      string                 `json:"content_type,omitempty"`
	DataUpdatedAt    *time.Time             `json:"data_updated_at,omitempty"`
	Content          string                 `json:"content,omitempty"`
	ContentPreview   string                 `json:"content_preview,omitempty"`
	ContentTruncated bool                   `json:"content_truncated,omitempty"`
	DocumentType     string                 `json:"document_type,omitempty"`
	Title            string                 `json:"title,omitempty"`
	Author           string                 `json:"author,omitempty"`
	Keywords         []string               `json:"keywords,omitempty"`
	WordCount        int                    `json:"word_count,omitempty"`
	PageCount        int                    `json:"page_count,omitempty"`
	CreatedDate      *time.Time             `json:"created_date,omitempty"`
	ModifiedDate     *time.Time             `json:"modified_date,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	ProjectionTime   time.Time              `json:"projection_time"`
}

func (d ManagerContentDocument) Validate() error {
	if strings.TrimSpace(d.DocumentID) == "" || d.EngineID == 0 || strings.TrimSpace(d.DataItemType) == "" || strings.TrimSpace(d.Name) == "" {
		return errors.New("Manager content document is incomplete")
	}
	switch d.PayloadKind {
	case ManagerContentPayloadTechnicalMetadata:
		if d.Content != "" || d.ContentPreview != "" || d.ContentTruncated || d.Title != "" || d.Author != "" || len(d.Keywords) > 0 || d.WordCount != 0 || d.PageCount != 0 || d.CreatedDate != nil || d.ModifiedDate != nil || len(d.Metadata) > 0 || len(d.Tags) > 0 {
			return errors.New("Manager technical metadata document contains extracted content")
		}
	case ManagerContentPayloadExtractedContent:
	default:
		return errors.New("Manager content document payload kind is invalid")
	}
	return nil
}

func (c *ManagerContentClient) UpsertDocument(ctx context.Context, document ManagerContentDocument) error {
	document.DocumentID = strings.TrimSpace(document.DocumentID)
	if err := document.Validate(); err != nil {
		return err
	}
	path := "/api/v1/manager/runtime/content-documents/" + url.PathEscape(document.DocumentID)
	if err := c.doJSON(ctx, http.MethodPut, path, document, nil); err != nil {
		return fmt.Errorf("Manager content document upsert: %w", err)
	}
	return nil
}

type ManagerContentDeleteScope struct {
	EngineID     uint
	DataItemType string
	Schema       string
	Bucket       string
	PathPrefix   string
}

func (c *ManagerContentClient) DeleteDocuments(ctx context.Context, scope ManagerContentDeleteScope) error {
	if scope.EngineID == 0 {
		return errors.New("Manager content document deletion requires an engine ID")
	}
	query := url.Values{}
	query.Set("engine_id", strconv.FormatUint(uint64(scope.EngineID), 10))
	if value := strings.TrimSpace(scope.DataItemType); value != "" {
		query.Set("data_item_type", value)
	}
	if value := strings.TrimSpace(scope.Schema); value != "" {
		query.Set("schema", value)
	}
	if value := strings.TrimSpace(scope.Bucket); value != "" {
		query.Set("bucket", value)
	}
	if value := strings.TrimSpace(scope.PathPrefix); value != "" {
		query.Set("path_prefix", value)
	}
	path := "/api/v1/manager/runtime/content-documents?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("Manager content document deletion: %w", err)
	}
	return nil
}
