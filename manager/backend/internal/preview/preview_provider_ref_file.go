package preview

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
)

type RefFilePreviewProvider struct {
	content *objectcontent.ObjectContentRegistry
}

func NewRefFilePreviewProvider(content *objectcontent.ObjectContentRegistry) PreviewProvider {
	return &RefFilePreviewProvider{content: content}
}

func (p *RefFilePreviewProvider) Name() string {
	return "builtin:ref-file"
}

func (p *RefFilePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	if req == nil || strings.TrimSpace(req.RefPath) == "" {
		return nil, fmt.Errorf("ref file preview requires ref_path")
	}
	contentCtx, err := (&FileTablePreviewProvider{}).contentContextForPreview(req)
	if err != nil {
		return nil, err
	}
	formatType := formatTypeFromMetaAttributes(req.Attributes)
	refs := refsForPreview(contentCtx.path, formatType, req.Attributes)
	ref, ok := refForPreviewPath(formatType, refs, req.RefPath)
	if !ok {
		return nil, fmt.Errorf("ref %s not found", req.RefPath)
	}
	previewRefs := refPreviewDescriptors(formatType, refs)
	descriptor := refDescriptorForRef(formatType, ref)
	preview := previewHintForRefDescriptor(descriptor, ref.Ref.Path)
	refName := contentio.BaseName(ref.Ref)
	contentReq := &objectcontent.ObjectContentRequest{
		Bucket:      contentCtx.bucket,
		Path:        "",
		Name:        refName,
		Format:      string(preview.Format),
		Extension:   defaultExtension(ref.Ref.Path),
		ContentType: previewContentType(preview.Format, refName),
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": preview.DataType,
				"format":    string(preview.Format),
			},
		},
	}
	if contentReq.ContentType == "application/octet-stream" {
		contentReq.ContentType = ""
	}

	if p.content != nil {
		handler := p.content.Resolve(contentReq)
		if handler != nil {
			if streamHandler, ok := handler.(objectcontent.StreamableContentHandler); ok {
				content, truncated, err := streamHandler.HandleStream(ctx, contentReq, func() (io.ReadCloser, error) {
					return contentCtx.reader.Open(ctx, ref.Ref)
				})
				if err != nil {
					return nil, err
				}
				if content != nil {
					if truncated || content.Truncated {
						content.Truncated = true
					}
					return p.objectPreview(req, contentCtx.bucket, ref, preview, previewRefs, content), nil
				}
			}
			content, truncated, err := handler.Handle(ctx, contentReq, func(limit int64) ([]byte, bool, error) {
				reader, err := contentCtx.reader.Open(ctx, ref.Ref)
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
				return p.objectPreview(req, contentCtx.bucket, ref, preview, previewRefs, content), nil
			}
		}
	}

	return p.objectPreview(req, contentCtx.bucket, ref, preview, previewRefs, &models.ObjectPreviewContent{
		Kind:     models.ObjectPreviewKindUnsupported,
		Text:     "暂不支持该相关内容的在线预览，请下载后查看。",
		Metadata: map[string]interface{}{"format": preview.Format, "data_type": preview.DataType},
	}), nil
}

func refForPreviewPath(formatType format.FormatType, refs []format.RelatedRef, target string) (format.RelatedRef, bool) {
	target = strings.Trim(strings.TrimSpace(target), "/")
	if target == "" {
		return format.RelatedRef{}, false
	}
	for _, ref := range refs {
		if refPathMatches(ref.Ref.Path, target) {
			return ref, true
		}
	}
	for _, descriptor := range format.DescribeRefs(formatType, refs) {
		if refPathMatches(descriptor.Path, target) {
			for _, ref := range refs {
				if refPathMatches(ref.Ref.Path, descriptor.Path) {
					return ref, true
				}
			}
		}
	}
	return format.RelatedRef{}, false
}

func refDescriptorForRef(formatType format.FormatType, ref format.RelatedRef) *format.RefDescriptor {
	for _, descriptor := range format.DescribeRefs(formatType, []format.RelatedRef{ref}) {
		if refPathMatches(descriptor.Path, ref.Ref.Path) {
			current := descriptor
			return &current
		}
	}
	return nil
}

func (p *RefFilePreviewProvider) objectPreview(req *PreviewRequest, bucket string, ref format.RelatedRef, preview previewHint, previewRefs []map[string]interface{}, content *models.ObjectPreviewContent) *models.TablePreview {
	if content != nil {
		objectcontent.DecoratePreviewContent(content)
		if len(previewRefs) > 0 {
			if content.Metadata == nil {
				content.Metadata = map[string]interface{}{}
			}
			content.Metadata["refs"] = previewRefs
			content.Metadata["layout"] = "multi"
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
			Path:        ref.Ref.Path,
			StorageRef:  storageRefForPreview(req, bucket, ref.Ref.Path),
			NodeType:    "object",
			ContentType: previewContentType(preview.Format, contentio.BaseName(ref.Ref)),
			Attributes: models.JSONMap{
				"item": map[string]interface{}{
					"data_type": preview.DataType,
					"format":    string(preview.Format),
				},
			},
			Content:  content,
			EngineID: req.Engine.ID,
		},
		GeometryColumns: []string{},
	}
}
