package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	commonJSON "github.com/addp/common/jsonmap"
)

// MetaClient 用于调用Meta模块的API
type MetaClient struct {
	baseURL string
	client  *http.Client
	token   string
}

// NewMetaClient 创建Meta客户端
func NewMetaClient(baseURL, token string) *MetaClient {
	return &MetaClient{
		baseURL: baseURL,
		token:   token,
		client: &http.Client{
			Timeout: 60 * time.Second, // 元数据提取可能需要较长时间
		},
	}
}

// ExtractObjectMetadataRequest 提取元数据请求
type ExtractObjectMetadataRequest struct {
	EngineID   uint
	ObjectKey  string
	ObjectData io.Reader
}

// ExtractObjectMetadataResponse Meta返回的元数据提取响应
type ExtractObjectMetadataResponse struct {
	Data struct {
		BasicInfo   map[string]interface{} `json:"BasicInfo"`
		CustomAttrs map[string]interface{} `json:"CustomAttrs"`
		SchemaInfo  interface{}            `json:"SchemaInfo"`
	} `json:"data"`
	Message string `json:"message"`
}

// ExtractObjectMetadata 调用Meta模块提取对象元数据
func (c *MetaClient) ExtractObjectMetadata(req *ExtractObjectMetadataRequest) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/meta/metadata/extract?engine_id=%d&object_key=%s",
		c.baseURL, req.EngineID, url.QueryEscape(req.ObjectKey))

	httpReq, err := http.NewRequest("POST", url, req.ObjectData)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call Meta API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Meta API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result ExtractObjectMetadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// 合并BasicInfo和CustomAttrs
	merged := make(map[string]interface{})
	merged["basic_info"] = result.Data.BasicInfo
	merged["custom_attrs"] = result.Data.CustomAttrs
	if result.Data.SchemaInfo != nil {
		merged["schema_info"] = result.Data.SchemaInfo
	}

	return merged, nil
}

func (c *MetaClient) BuildObjectContentIndex(req *ExtractObjectMetadataRequest) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/api/meta/metadata/content-index?engine_id=%d&object_key=%s",
		c.baseURL, req.EngineID, url.QueryEscape(req.ObjectKey))

	httpReq, err := http.NewRequest("POST", endpoint, req.ObjectData)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call Meta API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Meta API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Attributes map[string]interface{} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result.Data.Attributes, nil
}

// GetObjectMetadata 获取已存储的对象元数据
func (c *MetaClient) GetObjectMetadata(engineID uint, objectKey string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/meta/metadata/object?engine_id=%d&object_key=%s",
		c.baseURL, engineID, url.QueryEscape(objectKey))

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call Meta API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // 元数据不存在
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Meta API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Attributes map[string]interface{} `json:"attributes"`
		} `json:"data"`
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// 检查标准提取能力中的 extracted_metadata
	if extracted, ok := commonJSON.Value(result.Data.Attributes, "capabilities.extraction", "extracted_metadata").(map[string]interface{}); ok {
		return extracted, nil
	}

	return nil, nil
}

// TryExtractMetadata 尝试提取元数据：先查询已有的，如果没有则提取新的
func (c *MetaClient) TryExtractMetadata(engineID uint, objectKey string, objectDataProvider func() (io.Reader, error)) (map[string]interface{}, error) {
	// 1. 先查询已有的元数据
	existing, err := c.GetObjectMetadata(engineID, objectKey)
	if err != nil {
		// 查询失败时继续尝试提取，避免旧元数据缺失阻断预览。
		_ = err
	}

	if existing != nil {
		// 已有元数据，直接返回
		return existing, nil
	}

	// 2. 没有元数据，需要提取
	if objectDataProvider == nil {
		return nil, fmt.Errorf("no object data provider for extraction")
	}

	objectData, err := objectDataProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to get object data: %w", err)
	}

	// 3. 调用Meta提取
	extracted, err := c.ExtractObjectMetadata(&ExtractObjectMetadataRequest{
		EngineID:   engineID,
		ObjectKey:  objectKey,
		ObjectData: objectData,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to extract metadata: %w", err)
	}

	return extracted, nil
}

// CachedObjectReader 实现可重复读取的Reader
type CachedObjectReader struct {
	data []byte
	pos  int
}

// NewCachedObjectReader 创建缓存的对象读取器
func NewCachedObjectReader(data []byte) *CachedObjectReader {
	return &CachedObjectReader{
		data: data,
		pos:  0,
	}
}

func (r *CachedObjectReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *CachedObjectReader) Reset() {
	r.pos = 0
}

func (r *CachedObjectReader) Bytes() []byte {
	return r.data
}

func (r *CachedObjectReader) Reader() io.Reader {
	return bytes.NewReader(r.data)
}
