package preview

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
)

type ContainerChildPreviewProvider struct {
	content *objectcontent.ObjectContentRegistry
}

func NewContainerChildPreviewProvider(content *objectcontent.ObjectContentRegistry) PreviewProvider {
	return &ContainerChildPreviewProvider{content: content}
}

func (p *ContainerChildPreviewProvider) Name() string {
	return "builtin:container-child"
}

func (p *ContainerChildPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	if req == nil || strings.TrimSpace(req.ChildName) == "" {
		return nil, fmt.Errorf("container child preview requires child_name")
	}
	contentCtx, err := (&FileTablePreviewProvider{}).contentContextForPreview(req)
	if err != nil {
		return nil, err
	}
	parentFormat := normalizeFileTableFormat(stringAttribute(req.Attributes, "format"))
	child, err := resolvePreviewContainerChild(ctx, contentCtx.reader, contentCtx.path, parentFormat, req)
	if err != nil {
		return nil, err
	}
	if nestedChildPath := strings.Trim(strings.TrimSpace(req.NestedChildPath), "/"); nestedChildPath != "" {
		if !strings.EqualFold(strings.TrimSpace(child.DataType), format.FormatDataTypeContainer) {
			return nil, fmt.Errorf("nested_child_path requires container child %s", req.ChildName)
		}
		resolved, err := resolveNestedPreviewContainerChild(ctx, child, nestedChildPath, req)
		if err != nil {
			return nil, err
		}
		child = resolved
	}
	if refPath := strings.Trim(strings.TrimSpace(req.RefPath), "/"); refPath != "" {
		refChild := childInfoForRefPath(child, refPath)
		resolved, err := resolvePreviewContainerChild(ctx, contentCtx.reader, contentCtx.path, parentFormat, previewRequestForRef(req, refChild))
		if err != nil {
			return nil, err
		}
		child = resolved
	}
	switch strings.ToLower(strings.TrimSpace(child.DataType)) {
	case format.FormatDataTypeTable:
		return p.previewTableChild(ctx, req, contentCtx.bucket, child)
	case format.FormatDataTypeContainer:
		return p.previewContainerChild(ctx, req, contentCtx.bucket, child)
	case format.FormatDataTypeDocument, format.FormatDataTypeMedia, format.FormatDataTypeFile, "":
		return p.previewObjectChild(ctx, req, contentCtx.bucket, child)
	default:
		return p.previewObjectChild(ctx, req, contentCtx.bucket, child)
	}
}

func resolveNestedPreviewContainerChild(ctx context.Context, parent *format.ContainerChildResource, nestedPath string, req *PreviewRequest) (*format.ContainerChildResource, error) {
	nestedPath = strings.Trim(strings.TrimSpace(nestedPath), "/")
	if nestedPath == "" {
		return parent, nil
	}
	childInfo := childInfoForNestedContainerPath(nestedPath)
	resolved, directErr := resolveOpenablePreviewContainerChild(ctx, parent.Reader, parent.Ref, parent.Format, previewRequestForRef(req, childInfo))
	if directErr == nil {
		return resolved, nil
	}
	for _, index := range nestedPathSplitIndexes(nestedPath) {
		prefix := strings.Trim(nestedPath[:index], "/")
		rest := strings.Trim(nestedPath[index+1:], "/")
		if prefix == "" || rest == "" {
			continue
		}
		prefixInfo := childInfoForNestedContainerPath(prefix)
		prefixChild, err := resolveOpenablePreviewContainerChild(ctx, parent.Reader, parent.Ref, parent.Format, previewRequestForRef(req, prefixInfo))
		if err != nil || !isContainerChildResource(prefixChild) {
			continue
		}
		return resolveNestedPreviewContainerChild(ctx, prefixChild, rest, req)
	}
	return nil, directErr
}

func resolveOpenablePreviewContainerChild(ctx context.Context, parent contentio.Reader, parentRef contentio.Ref, parentFormat format.FormatType, req *PreviewRequest) (*format.ContainerChildResource, error) {
	child, err := resolvePreviewContainerChildFromResource(ctx, parent, parentRef, parentFormat, req)
	if err != nil {
		return nil, err
	}
	reader, err := child.Open(ctx)
	if err != nil {
		return nil, err
	}
	reader.Close()
	return child, nil
}

func nestedPathSplitIndexes(path string) []int {
	indexes := []int{}
	for index, char := range path {
		if char == '/' {
			indexes = append(indexes, index)
		}
	}
	for left, right := 0, len(indexes)-1; left < right; left, right = left+1, right-1 {
		indexes[left], indexes[right] = indexes[right], indexes[left]
	}
	return indexes
}

func isContainerChildResource(child *format.ContainerChildResource) bool {
	if child == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(child.DataType), format.FormatDataTypeContainer) {
		return true
	}
	descriptor, ok := format.GetFormatDescriptor(child.Format)
	return ok && descriptor.DataType == format.FormatDataTypeContainer
}

func childInfoForNestedContainerPath(refPath string) map[string]interface{} {
	name := strings.Trim(refPath, "/")
	preview := previewHintForRefDescriptor(nil, name)
	result := map[string]interface{}{
		"name":         name,
		"key":          name,
		"path":         name,
		"child_kind":   "file",
		"data_type":    preview.DataType,
		"format":       string(preview.Format),
		"layout":       "single",
		"content_type": previewContentType(preview.Format, name),
	}
	if result["content_type"] == "application/octet-stream" {
		delete(result, "content_type")
	}
	return result
}

func childInfoForRefPath(parent *format.ContainerChildResource, refPath string) map[string]interface{} {
	name := strings.Trim(refPath, "/")
	descriptor := refDescriptorForPath(parent, name)
	preview := previewHintForRefDescriptor(descriptor, name)
	result := map[string]interface{}{
		"name":         name,
		"key":          name,
		"path":         name,
		"child_kind":   "file",
		"data_type":    preview.DataType,
		"format":       string(preview.Format),
		"layout":       "single",
		"content_type": previewContentType(preview.Format, name),
	}
	if descriptor != nil {
		result["role"] = descriptor.Role
		result["label"] = descriptor.Label
		if descriptor.Extension != "" {
			result["extension"] = descriptor.Extension
		}
		result["preview_material"] = preview.Material
		result["preview_renderer"] = preview.Renderer
		result["previewable"] = preview.Previewable
		result["ref_preview"] = true
	}
	if result["content_type"] == "application/octet-stream" {
		delete(result, "content_type")
	}
	return result
}

func previewContentType(formatType format.FormatType, name string) string {
	if mime := format.GuessContentType(name, nil); strings.TrimSpace(mime) != "" && mime != "application/octet-stream" {
		return mime
	}
	return format.FormatToMIME(formatType)
}

func refDescriptorForPath(parent *format.ContainerChildResource, refPath string) *format.RefDescriptor {
	if parent == nil || len(parent.Refs) == 0 {
		return nil
	}
	descriptors := format.DescribeRefs(parent.Format, parent.Refs)
	for _, descriptor := range descriptors {
		if refPathMatches(descriptor.Path, refPath) {
			current := descriptor
			return &current
		}
	}
	return nil
}

func refPathMatches(candidate, target string) bool {
	candidate = strings.Trim(strings.TrimSpace(candidate), "/")
	target = strings.Trim(strings.TrimSpace(target), "/")
	if candidate == "" || target == "" {
		return false
	}
	if strings.EqualFold(candidate, target) {
		return true
	}
	if strings.HasSuffix(strings.ToLower(candidate), "/"+strings.ToLower(target)) {
		return true
	}
	return strings.EqualFold(resourceBase(candidate), resourceBase(target))
}

func resourceBase(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

func previewHintForRefDescriptor(descriptor *format.RefDescriptor, path string) previewHint {
	if descriptor == nil {
		return inferPreviewHint(previewHintInput{
			Name: path,
			Path: path,
		})
	}
	hint := inferPreviewHint(previewHintInput{
		Name:     descriptor.Path,
		Path:     descriptor.Path,
		Format:   descriptor.Format,
		DataType: descriptor.DataType,
	})
	return hint
}

func previewRequestForRef(req *PreviewRequest, child map[string]interface{}) *PreviewRequest {
	next := *req
	next.ChildName = strings.Trim(strings.TrimSpace(commonJSON.InterfaceString(child["path"])), "/")
	if next.ChildName == "" {
		next.ChildName = strings.TrimSpace(commonJSON.InterfaceString(child["name"]))
	}
	next.RefPath = ""
	next.NestedChildPath = ""
	next.Attributes = map[string]interface{}{}
	for key, value := range req.Attributes {
		if key == "type_info" {
			continue
		}
		next.Attributes[key] = value
	}
	typeInfo := map[string]interface{}{}
	next.Attributes["type_info"] = typeInfo
	containerInfo := map[string]interface{}{}
	typeInfo["container"] = containerInfo
	containerInfo["children"] = []interface{}{child}
	return &next
}

func (p *ContainerChildPreviewProvider) previewTableChild(ctx context.Context, req *PreviewRequest, bucket string, child *format.ContainerChildResource) (*models.TablePreview, error) {
	opts := child.ParentOptions
	if opts == nil {
		opts = format.ChildTableParseOptions(req.ChildName, containerChildForRequest(req.Attributes, req.ChildName))
	}
	if len(child.Refs) > 0 {
		infoProvider, err := format.GetMultiTableInfoProvider(child.Format)
		if err != nil {
			return p.previewObjectChild(ctx, req, bucket, child)
		}
		sampleReader, err := format.GetMultiTableSampleReader(child.Format)
		if err != nil {
			return nil, fmt.Errorf("no multi table sample reader for child format %s: %w", child.Format, err)
		}
		return (&FileTablePreviewProvider{}).previewRefs(ctx, child.Reader, child.Refs, bucket, child.Format, infoProvider, sampleReader, opts, req)
	}
	infoProvider, _ := format.GetTableInfoProvider(child.Format)
	sampleReader, err := format.GetTableSampleReader(child.Format)
	if err != nil {
		return p.previewObjectChild(ctx, req, bucket, child)
	}
	return (&FileTablePreviewProvider{}).previewStreamable(ctx, child.Reader, bucket, child.Ref.Path, child.Format, infoProvider, sampleReader, opts, req)
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
	previewJSON := objectcontent.BuildContainerPreviewFromInfo(info, string(child.Format))
	metadata := objectcontent.BuildContainerMetadata(info, &objectcontent.ObjectContentRequest{
		Bucket:     bucket,
		Name:       child.Name,
		Format:     string(child.Format),
		Attributes: req.Attributes,
	}, child.Format)
	return p.objectPreview(req, bucket, child, objectcontent.DecoratePreviewContent(&models.ObjectPreviewContent{
		Kind:      models.ObjectPreviewKindContainer,
		JSON:      previewJSON,
		Metadata:  metadata,
		Truncated: objectcontent.ContainerInfoTruncated(info),
	})), nil
}

func (p *ContainerChildPreviewProvider) previewObjectChild(ctx context.Context, req *PreviewRequest, bucket string, child *format.ContainerChildResource) (*models.TablePreview, error) {
	contentReq := &objectcontent.ObjectContentRequest{
		Bucket:      bucket,
		Path:        "",
		Name:        child.Name,
		Format:      string(child.Format),
		Extension:   defaultExtension(child.Name),
		ContentType: contentTypeForChild(child),
		Attributes:  childObjectAttributes(child),
	}
	if child.Properties != nil {
		if previewFormat := strings.TrimSpace(interfaceStringForContainerChild(child.Properties["preview_format"])); previewFormat != "" {
			contentReq.Format = previewFormat
		}
		if contentType := strings.TrimSpace(interfaceStringForContainerChild(child.Properties["content_type"])); contentType != "" {
			contentReq.ContentType = contentType
		}
	}
	if p.content != nil {
		handler := p.content.Resolve(contentReq)
		if handler != nil {
			if streamHandler, ok := handler.(objectcontent.StreamableContentHandler); ok {
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
	return objectcontent.InferContentType(child.Name, "")
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
		if previewMaterial := strings.TrimSpace(interfaceStringForContainerChild(child.Properties["preview_material"])); previewMaterial != "" {
			attrs["preview_material"] = previewMaterial
		}
		if previewRenderer := strings.TrimSpace(interfaceStringForContainerChild(child.Properties["preview_renderer"])); previewRenderer != "" {
			attrs["frontend_renderer"] = previewRenderer
		}
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
