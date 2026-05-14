package service

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
	"github.com/addp/manager/internal/models"
)

type ContainerChildPreviewProvider struct {
	content *ObjectContentRegistry
}

func NewContainerChildPreviewProvider(content *ObjectContentRegistry) PreviewProvider {
	return &ContainerChildPreviewProvider{content: content}
}

func (p *ContainerChildPreviewProvider) Name() string {
	return "builtin:container-child"
}

func (p *ContainerChildPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	if req == nil || strings.TrimSpace(req.ChildName) == "" {
		return nil, fmt.Errorf("container child preview requires child_name")
	}
	resourceCtx, err := (&FileTablePreviewProvider{}).resourceContextForPreview(req)
	if err != nil {
		return nil, err
	}
	parentFormat := normalizeFileTableFormat(stringAttribute(req.Attributes, "format"))
	child, err := resolvePreviewContainerChild(ctx, resourceCtx.reader, resourceCtx.path, parentFormat, req)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(child.DataType)) {
	case format.FormatDataTypeTable:
		return p.previewTableChild(ctx, req, resourceCtx.bucket, child)
	case format.FormatDataTypeContainer:
		return p.previewContainerChild(ctx, req, resourceCtx.bucket, child)
	case format.FormatDataTypeDocument, format.FormatDataTypeMedia, format.FormatDataTypeFile, "":
		return p.previewObjectChild(ctx, req, resourceCtx.bucket, child)
	default:
		return p.previewObjectChild(ctx, req, resourceCtx.bucket, child)
	}
}

func (p *ContainerChildPreviewProvider) previewTableChild(ctx context.Context, req *PreviewRequest, bucket string, child *format.ContainerChildResource) (*models.TablePreview, error) {
	provider, err := format.GetTableProvider(child.Format)
	if err != nil {
		return p.previewObjectChild(ctx, req, bucket, child)
	}
	opts := child.ParentOptions
	if opts == nil {
		opts = format.ChildTableParseOptions(req.ChildName, containerChildForRequest(req.Attributes, req.ChildName))
	}
	if componentProvider, ok := provider.(format.ComponentTableProvider); ok && len(child.Components) > 0 {
		return (&FileTablePreviewProvider{}).previewComponents(ctx, resource.NewStaticComponentReader(child.Reader, child.Components), bucket, child.Format, componentProvider, opts, req)
	}
	return (&FileTablePreviewProvider{}).previewStreamable(ctx, child.Reader, bucket, child.Ref.Path, child.Format, provider, opts, req)
}

func (p *ContainerChildPreviewProvider) previewContainerChild(ctx context.Context, req *PreviewRequest, bucket string, child *format.ContainerChildResource) (*models.TablePreview, error) {
	provider, err := format.GetContainerInfoProvider(child.Format)
	if err != nil {
		return p.previewObjectChild(ctx, req, bucket, child)
	}
	reader, err := child.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	info, err := provider.DescribeContainer(ctx, reader, format.ContainerParseOptions(0, 0))
	if err != nil {
		return nil, err
	}
	previewJSON := buildContainerPreviewFromContainerInfo(info, string(child.Format))
	metadata := buildContainerMetadataMap(info, &ObjectContentRequest{
		Bucket:     bucket,
		Name:       child.Name,
		Format:     string(child.Format),
		Attributes: req.Attributes,
	}, child.Format)
	return p.objectPreview(req, bucket, child, &models.ObjectPreviewContent{
		Kind:      models.ObjectPreviewKindContainer,
		JSON:      previewJSON,
		Metadata:  metadata,
		Truncated: containerInfoTruncated(info),
	}), nil
}

func (p *ContainerChildPreviewProvider) previewObjectChild(ctx context.Context, req *PreviewRequest, bucket string, child *format.ContainerChildResource) (*models.TablePreview, error) {
	contentReq := &ObjectContentRequest{
		Bucket:      bucket,
		Path:        "",
		Name:        child.Name,
		Format:      string(child.Format),
		Extension:   defaultExtension(child.Name),
		ContentType: contentTypeForChild(child),
		Attributes:  childObjectAttributes(child),
	}
	if p.content != nil {
		handler := p.content.Resolve(contentReq)
		if handler != nil {
			if streamHandler, ok := handler.(StreamableContentHandler); ok {
				content, truncated, err := streamHandler.HandleStream(ctx, contentReq, func() (io.ReadCloser, error) {
					return child.Open(ctx)
				})
				if err != nil {
					return nil, err
				}
				if content != nil {
					if truncated || content.Truncated {
						content.Truncated = true
					}
					return p.objectPreview(req, bucket, child, content), nil
				}
			}
			content, truncated, err := handler.Handle(ctx, contentReq, func(limit int64) ([]byte, bool, error) {
				reader, err := child.Open(ctx)
				if err != nil {
					return nil, false, err
				}
				defer reader.Close()
				if limit <= 0 {
					limit = maxTextPreviewBytes
				}
				return readObjectWithLimit(reader, limit)
			})
			if err != nil {
				return nil, err
			}
			if content != nil {
				if truncated || content.Truncated {
					content.Truncated = true
				}
				return p.objectPreview(req, bucket, child, content), nil
			}
		}
	}
	return p.objectPreview(req, bucket, child, &models.ObjectPreviewContent{
		Kind:     models.ObjectPreviewKindUnsupported,
		Text:     "暂不支持该容器子对象的在线预览，请下载后查看。",
		Metadata: map[string]interface{}{"format": child.Format, "data_type": child.DataType},
	}), nil
}

func (p *ContainerChildPreviewProvider) objectPreview(req *PreviewRequest, bucket string, child *format.ContainerChildResource, content *models.ObjectPreviewContent) *models.TablePreview {
	return &models.TablePreview{
		Mode:     PreviewModeObject,
		Page:     1,
		PageSize: 1,
		Columns:  []string{},
		Rows:     []map[string]interface{}{},
		Object: &models.ObjectPreview{
			Bucket:      bucket,
			Path:        child.Ref.Path,
			ObjectKey:   child.Ref.Path,
			NodeType:    "object",
			ContentType: contentTypeForChild(child),
			Attributes:  childObjectAttributes(child),
			Content:     content,
			EngineID:    req.Engine.ID,
		},
		GeometryColumns: []string{},
	}
}

func contentTypeForChild(child *format.ContainerChildResource) string {
	if child == nil {
		return "application/octet-stream"
	}
	if contentType := strings.TrimSpace(interfaceStringForContainerChild(child.Properties["content_type"])); contentType != "" {
		return contentType
	}
	if mime := format.FormatToMIME(child.Format); mime != "" {
		return mime
	}
	return inferContentType(child.Name, "")
}

func childObjectAttributes(child *format.ContainerChildResource) map[string]interface{} {
	attrs := map[string]interface{}{
		"item": map[string]interface{}{
			"data_type": child.DataType,
			"format":    string(child.Format),
		},
	}
	if len(child.Properties) > 0 {
		attrs["container_child"] = child.Properties
	}
	return attrs
}

func interfaceStringForContainerChild(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}
