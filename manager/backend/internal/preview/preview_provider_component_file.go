package preview

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
)

type ComponentFilePreviewProvider struct {
	content *objectcontent.ObjectContentRegistry
}

func NewComponentFilePreviewProvider(content *objectcontent.ObjectContentRegistry) PreviewProvider {
	return &ComponentFilePreviewProvider{content: content}
}

func (p *ComponentFilePreviewProvider) Name() string {
	return "builtin:component-file"
}

func (p *ComponentFilePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	if req == nil || strings.TrimSpace(req.ComponentPath) == "" {
		return nil, fmt.Errorf("component file preview requires component_path")
	}
	resourceCtx, err := (&FileTablePreviewProvider{}).resourceContextForPreview(req)
	if err != nil {
		return nil, err
	}
	formatType := normalizeFileTableFormat(stringAttribute(req.Attributes, "format"))
	components := componentReaderForPreview(resourceCtx.reader, resourceCtx.path, formatType, req.Attributes).Components()
	component, ok := componentForPreviewPath(formatType, components, req.ComponentPath)
	if !ok {
		return nil, fmt.Errorf("component %s not found", req.ComponentPath)
	}
	componentDescriptors := componentPreviewDescriptors(formatType, components)
	descriptor := componentDescriptorForComponent(formatType, component)
	preview := previewHintForComponentDescriptor(descriptor, component.Path)
	contentReq := &objectcontent.ObjectContentRequest{
		Bucket:      resourceCtx.bucket,
		Path:        "",
		Name:        component.Name,
		Format:      string(preview.Format),
		Extension:   defaultExtension(component.Path),
		ContentType: previewContentType(preview.Format, component.Name),
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": preview.DataType,
				"format":    string(preview.Format),
			},
		},
	}
	if descriptor != nil {
		contentReq.Attributes["component"] = map[string]interface{}{
			"path":     descriptor.Path,
			"role":     descriptor.Role,
			"label":    descriptor.Label,
			"required": descriptor.Required,
			"primary":  descriptor.Primary,
		}
		if descriptor.PreviewMaterial != "" {
			contentReq.Attributes["preview_material"] = descriptor.PreviewMaterial
		}
		if descriptor.PreviewRenderer != "" {
			contentReq.Attributes["frontend_renderer"] = descriptor.PreviewRenderer
		}
	}
	if len(componentDescriptors) > 0 {
		contentReq.Attributes["components"] = componentDescriptors
	}
	if contentReq.ContentType == "application/octet-stream" {
		contentReq.ContentType = ""
	}

	if p.content != nil {
		handler := p.content.Resolve(contentReq)
		if handler != nil {
			if streamHandler, ok := handler.(objectcontent.StreamableContentHandler); ok {
				content, truncated, err := streamHandler.HandleStream(ctx, contentReq, func() (io.ReadCloser, error) {
					return resourceCtx.reader.Open(ctx, component.ResourceRef)
				})
				if err != nil {
					return nil, err
				}
				if content != nil {
					if truncated || content.Truncated {
						content.Truncated = true
					}
					return p.objectPreview(req, resourceCtx.bucket, component, preview, componentDescriptors, content), nil
				}
			}
			content, truncated, err := handler.Handle(ctx, contentReq, func(limit int64) ([]byte, bool, error) {
				reader, err := resourceCtx.reader.Open(ctx, component.ResourceRef)
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
				return p.objectPreview(req, resourceCtx.bucket, component, preview, componentDescriptors, content), nil
			}
		}
	}

	return p.objectPreview(req, resourceCtx.bucket, component, preview, componentDescriptors, &models.ObjectPreviewContent{
		Kind:     models.ObjectPreviewKindUnsupported,
		Text:     "暂不支持该组件文件的在线预览，请下载后查看。",
		Metadata: map[string]interface{}{"format": preview.Format, "data_type": preview.DataType},
	}), nil
}

func componentForPreviewPath(formatType format.FormatType, components []resource.ComponentRef, target string) (resource.ComponentRef, bool) {
	target = strings.Trim(strings.TrimSpace(target), "/")
	if target == "" {
		return resource.ComponentRef{}, false
	}
	for _, component := range components {
		if componentPathMatches(component.Path, target) {
			return component, true
		}
	}
	for _, descriptor := range format.DescribeComponents(formatType, components) {
		if componentPathMatches(descriptor.Path, target) {
			for _, component := range components {
				if componentPathMatches(component.Path, descriptor.Path) {
					return component, true
				}
			}
		}
	}
	return resource.ComponentRef{}, false
}

func componentDescriptorForComponent(formatType format.FormatType, component resource.ComponentRef) *format.ComponentDescriptor {
	for _, descriptor := range format.DescribeComponents(formatType, []resource.ComponentRef{component}) {
		if componentPathMatches(descriptor.Path, component.Path) {
			current := descriptor
			return &current
		}
	}
	return nil
}

func (p *ComponentFilePreviewProvider) objectPreview(req *PreviewRequest, bucket string, component resource.ComponentRef, preview format.PreviewHint, components []map[string]interface{}, content *models.ObjectPreviewContent) *models.TablePreview {
	if content != nil {
		objectcontent.DecoratePreviewContent(content)
		if len(components) > 0 {
			if content.Metadata == nil {
				content.Metadata = map[string]interface{}{}
			}
			content.Metadata["components"] = components
			content.Metadata["organization"] = "multi"
		}
	}
	return &models.TablePreview{
		Mode:     PreviewModeObject,
		Page:     1,
		PageSize: 1,
		Columns:  []string{},
		Rows:     []map[string]interface{}{},
		Object: &models.ObjectPreview{
			Bucket:      bucket,
			Path:        component.Path,
			ObjectKey:   component.Path,
			NodeType:    "object",
			ContentType: previewContentType(preview.Format, component.Name),
			Attributes: models.JSONMap{
				"item": map[string]interface{}{
					"data_type": preview.DataType,
					"format":    string(preview.Format),
				},
				"components": components,
			},
			Content:  content,
			EngineID: req.Engine.ID,
		},
		GeometryColumns: []string{},
	}
}
