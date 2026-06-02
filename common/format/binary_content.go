package format

import (
	"context"
	"fmt"
	"io"
)

const DefaultBinaryContentReadLimit int64 = 16 * 1024

// BinaryContent 是 unknown 非文本内容的中立读取结果。
//
// Bytes 不参与 JSON 序列化，避免格式层结果被误当成长期元数据写入 attributes。
// 上层模块如需展示，应自行投影为下载提示、hex 片段或其他专用 DTO。
type BinaryContent struct {
	Bytes     []byte                 `json:"-"`
	Truncated bool                   `json:"truncated,omitempty"`
	SizeBytes *int64                 `json:"size_bytes,omitempty"`
	MIMEType  string                 `json:"mime_type,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type defaultBinaryContentReader struct{}

func NewBinaryContentReader() BinaryContentReader {
	return defaultBinaryContentReader{}
}

func ReadBinaryContent(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (*BinaryContent, error) {
	return NewBinaryContentReader().ReadBinaryContent(ctx, input, limit, options)
}

func (defaultBinaryContentReader) Format() FormatType {
	return FormatUnknown
}

func (defaultBinaryContentReader) ReadBinaryContent(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (*BinaryContent, error) {
	if input == nil {
		return nil, fmt.Errorf("binary content input cannot be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = DefaultBinaryContentReadLimit
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	result := &BinaryContent{
		Bytes:     data,
		Truncated: truncated,
	}
	if options != nil && options.ExtraParams != nil {
		if size, ok := int64ExtraParam(options.ExtraParams, "size_bytes"); ok {
			result.SizeBytes = &size
		}
		if mimeType, ok := stringExtraParam(options.ExtraParams, "mime_type"); ok {
			result.MIMEType = mimeType
		}
	}
	return result, nil
}

func int64ExtraParam(params map[string]interface{}, key string) (int64, bool) {
	value, ok := params[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case int32:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case float32:
		return int64(typed), true
	default:
		return 0, false
	}
}

func stringExtraParam(params map[string]interface{}, key string) (string, bool) {
	value, ok := params[key]
	if !ok {
		return "", false
	}
	typed, ok := value.(string)
	return typed, ok && typed != ""
}
