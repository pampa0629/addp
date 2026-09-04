package preview

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
	"github.com/addp/common/format/rastermosaic"
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
	rasterLeafMetadataByPath := map[string]map[string]interface{}{}
	if rasterRefs, rasterMetadata, err := rasterMosaicLeafRefsForPreview(ctx, contentCtx.reader, contentCtx.path, formatType, req.Attributes); err != nil {
		return nil, err
	} else if len(rasterRefs) > 0 {
		refs = rasterRefs
		rasterLeafMetadataByPath = rasterMetadata
	}
	ref, ok := refForPreviewPath(formatType, refs, req.RefPath)
	if !ok {
		return nil, fmt.Errorf("ref %s not found", req.RefPath)
	}
	rasterLeafMetadata := rasterMosaicLeafMetadataForRef(rasterLeafMetadataByPath, ref.Ref.Path)
	previewRefs := refPreviewDescriptors(formatType, refs)
	descriptor := refDescriptorForRef(formatType, ref)
	preview := previewHintForRefDescriptor(descriptor, ref.Ref.Path)
	refName := contentio.BaseName(ref.Ref)
	storageRef := storageRefForPreview(req, contentCtx.bucket, ref.Ref.Path)
	contentReq := &objectcontent.ObjectContentRequest{
		Bucket:      contentCtx.bucket,
		Path:        "",
		Name:        refName,
		Format:      string(preview.Format),
		Extension:   defaultExtension(ref.Ref.Path),
		ContentType: previewContentType(preview.Format, refName),
		PreviewURL:  buildStorageStreamURL(req.Locator, previewRequestEngineID(req), storageRef),
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
					applyRasterMosaicLeafPreviewMetadata(content, rasterLeafMetadata)
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
				applyRasterMosaicLeafPreviewMetadata(content, rasterLeafMetadata)
				return p.objectPreview(req, contentCtx.bucket, ref, preview, previewRefs, content), nil
			}
		}
	}

	content := &models.ObjectPreviewContent{
		Kind:     models.ObjectPreviewKindUnsupported,
		Text:     "暂不支持该相关内容的在线预览，请下载后查看。",
		Metadata: map[string]interface{}{"format": preview.Format, "data_type": preview.DataType},
	}
	applyRasterMosaicLeafPreviewMetadata(content, rasterLeafMetadata)
	return p.objectPreview(req, contentCtx.bucket, ref, preview, previewRefs, content), nil
}

func rasterMosaicLeafRefsForPreview(ctx context.Context, reader contentio.Reader, datasetRoot string, formatType format.FormatType, attrs map[string]interface{}) ([]format.RelatedRef, map[string]map[string]interface{}, error) {
	if formatType != format.FormatRasterMosaic {
		return nil, nil, nil
	}
	if reader == nil {
		return nil, nil, fmt.Errorf("raster mosaic preview requires content reader")
	}
	info := formatInfoRasterMosaic(attrs)
	indexRef := strings.Trim(strings.TrimSpace(interfaceString(info["index_ref"])), "/")
	if indexRef == "" {
		indexRef = rastermosaic.SourceIndexRef
	}
	indexPath := joinPreviewPath(datasetRoot, indexRef)
	rc, err := reader.Open(ctx, contentio.NewRef(indexPath, contentio.RoleManifest))
	if err != nil {
		return nil, nil, fmt.Errorf("open raster mosaic source index: %w", err)
	}
	defer rc.Close()
	index, err := rastermosaic.DecodeSourceIndex(rc, 16<<20)
	if err != nil {
		return nil, nil, fmt.Errorf("decode raster mosaic source index: %w", err)
	}
	refs := make([]format.RelatedRef, 0, len(index.Leaves))
	metadataByPath := make(map[string]map[string]interface{}, len(index.Leaves))
	for _, leaf := range index.Leaves {
		leafRef := strings.Trim(strings.TrimSpace(interfaceString(leaf["leaf_ref"])), "/")
		if leafRef == "" {
			leafRef = strings.Trim(strings.TrimSpace(interfaceString(leaf["path"])), "/")
		}
		if leafRef == "" {
			continue
		}
		role := strings.TrimSpace(interfaceString(leaf["id"]))
		if role == "" {
			role = "leaf"
		}
		refPath := joinPreviewPath(datasetRoot, leafRef)
		refs = append(refs, format.NewRelatedRef(contentio.NewRef(refPath, role), true, false))
		if metadata := rasterMosaicLeafPreviewMetadata(leaf); len(metadata) > 0 {
			metadataByPath[refPath] = metadata
		}
	}
	return refs, metadataByPath, nil
}

func rasterMosaicLeafMetadataForRef(metadataByPath map[string]map[string]interface{}, refPath string) map[string]interface{} {
	if len(metadataByPath) == 0 {
		return nil
	}
	for pathValue, metadata := range metadataByPath {
		if refPathMatches(pathValue, refPath) {
			return metadata
		}
	}
	return nil
}

func rasterMosaicLeafPreviewMetadata(leaf map[string]interface{}) map[string]interface{} {
	if len(leaf) == 0 {
		return nil
	}
	metadata := map[string]interface{}{}
	extent := floatSlice(leaf["extent"])
	if len(extent) == 4 {
		metadata["extent"] = extent
	}
	if sourceCRS := strings.TrimSpace(interfaceString(leaf["source_crs"])); sourceCRS != "" {
		metadata["source_crs"] = sourceCRS
		if srid := sridFromCRS(sourceCRS); srid > 0 {
			metadata["extent_srid"] = srid
			metadata["srid"] = srid
		}
	} else if extentLooksGeographic(extent) {
		metadata["source_crs"] = "EPSG:4326"
		metadata["extent_srid"] = 4326
		metadata["srid"] = 4326
	}
	copyIfPresent(metadata, leaf, "width")
	copyIfPresent(metadata, leaf, "height")
	copyIfPresent(metadata, leaf, "band_count")
	copyIfPresent(metadata, leaf, "dtype")
	return metadata
}

func applyRasterMosaicLeafPreviewMetadata(content *models.ObjectPreviewContent, metadata map[string]interface{}) {
	if content == nil || len(metadata) == 0 {
		return
	}
	if content.Metadata == nil {
		content.Metadata = map[string]interface{}{}
	}
	for key, value := range metadata {
		if _, exists := content.Metadata[key]; !exists {
			content.Metadata[key] = value
		}
	}
}

func floatSlice(value interface{}) []float64 {
	raw, ok := value.([]interface{})
	if !ok {
		if values, ok := value.([]float64); ok {
			return values
		}
		return nil
	}
	result := make([]float64, 0, len(raw))
	for _, item := range raw {
		var v float64
		if _, err := fmt.Sscan(fmt.Sprint(item), &v); err != nil {
			return nil
		}
		result = append(result, v)
	}
	return result
}

func extentLooksGeographic(extent []float64) bool {
	if len(extent) != 4 {
		return false
	}
	minX, minY, maxX, maxY := extent[0], extent[1], extent[2], extent[3]
	return minX >= -180 && minX <= 180 &&
		maxX >= -180 && maxX <= 180 &&
		minY >= -90 && minY <= 90 &&
		maxY >= -90 && maxY <= 90 &&
		maxX > minX &&
		maxY > minY
}

func sridFromCRS(crs string) int {
	crs = strings.TrimSpace(strings.ToUpper(crs))
	if !strings.HasPrefix(crs, "EPSG:") {
		return 0
	}
	var srid int
	if _, err := fmt.Sscan(strings.TrimPrefix(crs, "EPSG:"), &srid); err != nil {
		return 0
	}
	return srid
}

func copyIfPresent(target, source map[string]interface{}, key string) {
	if value, ok := source[key]; ok && value != nil {
		target[key] = value
	}
}

func formatInfoRasterMosaic(attrs map[string]interface{}) map[string]interface{} {
	formatInfo, ok := attrs["format_info"].(map[string]interface{})
	if !ok {
		return nil
	}
	info, _ := formatInfo["raster_mosaic"].(map[string]interface{})
	return info
}

func interfaceString(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func joinPreviewPath(root, child string) string {
	root = strings.Trim(root, "/")
	child = strings.Trim(child, "/")
	if root == "" {
		return child
	}
	if child == "" {
		return root
	}
	return path.Join(root, child)
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
