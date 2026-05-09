package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/common/format/codecs/shapefile"
	"github.com/addp/common/logger"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/addp/manager/internal/models"
)

const (
	defaultShapefilePreviewFeatures = 200
	maxShapefilePreviewBytes        = 50 * 1024 * 1024 // 50MB：超过此大小跳过全量下载
)

type shapefileContentHandler struct {
	baseContentHandler
	maxFeatures int
}

func (h *shapefileContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	// Shapefile 预览依赖多文件协同处理，普通 Handle 无法工作，这里给出提示
	return &models.ObjectPreviewContent{
		Kind: "shapefile",
		Text: "Shapefile 预览需要完整的 .shp/.shx/.dbf 文件组合，当前处理器未启用流式模式。",
	}, false, nil
}

func (h *shapefileContentHandler) HandleCompositeStream(ctx context.Context, req *ObjectContentRequest, baseStreamer ObjectStreamProvider, siblingProvider ObjectSiblingStreamProvider) (*models.ObjectPreviewContent, bool, error) {
	// 大文件保护：超过阈值时跳过全量下载，仅返回元数据提示
	if req.Size > maxShapefilePreviewBytes {
		sizeMB := float64(req.Size) / (1024 * 1024)
		return &models.ObjectPreviewContent{
			Kind: "shapefile",
			Text: fmt.Sprintf("Shapefile 文件较大（%.1f MB），已跳过全量下载。如需预览请下载后在本地查看。", sizeMB),
			Metadata: map[string]interface{}{
				"size_bytes":  req.Size,
				"size_mb":     sizeMB,
				"skipped":     true,
				"skip_reason": "file_too_large",
			},
		}, false, nil
	}

	tmpDir, err := os.MkdirTemp("", "shapefile-preview-*")
	if err != nil {
		return nil, false, fmt.Errorf("创建 shapefile 临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// 从 req.Name 获取文件名（不含扩展名）
	rawExt := filepath.Ext(req.Name)
	baseName := strings.TrimSuffix(req.Name, rawExt)
	if baseName == "" {
		baseName = "shapefile"
	}

	shpPath := filepath.Join(tmpDir, baseName+".shp")
	if _, err := downloadObjectToFile(baseStreamer, shpPath); err != nil {
		return nil, false, fmt.Errorf("下载 shapefile 主文件失败: %w", err)
	}

	// 构建完整路径用于下载同级文件
	fullPath := req.Path + req.Name
	requiredExts := []string{".shx", ".dbf"}
	missing := make([]string, 0, len(requiredExts))
	for _, ext := range requiredExts {
		target := filepath.Join(tmpDir, baseName+ext)
		if err := downloadSiblingToFile(fullPath, ext, siblingProvider, target); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				missing = append(missing, ext)
				continue
			}
			return nil, false, fmt.Errorf("下载 shapefile 依赖文件失败 (%s): %w", ext, err)
		}
	}

	if len(missing) > 0 {
		return &models.ObjectPreviewContent{
			Kind: "shapefile",
			Text: fmt.Sprintf("Shapefile 缺少必要的配套文件: %s。请确认 .shp/.shx/.dbf 均已上传。", strings.Join(missing, ", ")),
			Metadata: map[string]interface{}{
				"missing_parts": missing,
			},
		}, false, nil
	}

	prjText, _ := downloadSiblingText(fullPath, ".prj", siblingProvider)
	cpgText, _ := downloadSiblingText(fullPath, ".cpg", siblingProvider)

	// 使用 common/format/codecs/shapefile 的 Reader
	reader, err := shapefile.Open(shpPath)
	if err != nil {
		return nil, false, fmt.Errorf("打开 shapefile 失败: %w", err)
	}
	defer reader.Close()

	// 使用 GetSchema() 获取字段信息
	schema := reader.GetSchema()
	fieldsMeta := make([]map[string]interface{}, 0, len(schema))
	for _, field := range schema {
		fieldsMeta = append(fieldsMeta, map[string]interface{}{
			"name":      field.Name,
			"type":      field.Type,
			"raw_type":  field.RawType,
			"size":      field.Size,
			"precision": field.Precision,
		})
	}

	totalFeatures := reader.AttributeCount()
	bbox := reader.BBox()
	maxPreview := h.maxFeatures
	if maxPreview <= 0 {
		maxPreview = defaultShapefilePreviewFeatures
	}

	// 使用 ReadAllFeatures() 读取所有特征
	features := make([]map[string]interface{}, 0, maxPreview)
	allFeatures, err := reader.ReadAllFeatures(maxPreview)
	if err != nil {
		return nil, false, fmt.Errorf("读取 shapefile 数据失败: %w", err)
	}

	for _, feature := range allFeatures {
		// 使用 common/format/codecs/shapefile 的 ShapeToGeoJSON 转换几何为 GeoJSON
		geometry, err := shapefile.ShapeToGeoJSON(feature.Geometry)
		if err != nil {
			logger.L().Warn("Shapefile 预览: 几何转换失败", "path", req.Path+req.Name, "error", err)
			continue
		}

		// 使用已经解析好的属性
		features = append(features, map[string]interface{}{
			"type":       "Feature",
			"geometry":   geometry,
			"properties": feature.Properties,
		})
	}

	if err := reader.Err(); err != nil {
		return nil, false, fmt.Errorf("读取 shapefile 数据失败: %w", err)
	}

	truncated := totalFeatures > len(features)
	metadata := map[string]interface{}{
		"geometry_type":         shapefile.MapShapeType(reader.GeometryType),
		"feature_count":         totalFeatures,
		"preview_feature_count": len(features),
		"fields":                fieldsMeta,
		"bbox":                  []float64{bbox.MinX, bbox.MinY, bbox.MaxX, bbox.MaxY},
		"required_parts":        []string{".shp", ".shx", ".dbf"},
		"optional_parts":        []string{".prj", ".cpg"},
	}
	if prjText != "" {
		metadata["projection_wkt"] = prjText
	}
	if cpgText != "" {
		metadata["code_page"] = cpgText
	}

	geojson := map[string]interface{}{
		"type":     "FeatureCollection",
		"features": features,
	}

	sourceSRID := commonSpatial.ParseSRID(strings.TrimSpace(prjText))
	if sourceSRID > 0 {
		metadata["source_srid"] = sourceSRID
		metadata["spatial_ref_sys"] = fmt.Sprintf("EPSG:%d", sourceSRID)
	}

	transformResult, transformErr := commonSpatial.TransformGeoJSONToWGS84(ctx, geojson, sourceSRID, strings.TrimSpace(prjText))
	if transformErr != nil {
		logger.L().Warn("Shapefile 预览: 坐标转换失败", "path", req.Path+req.Name, "error", transformErr)
		metadata["transform_status"] = string(commonSpatial.TransformStatusUnsupportedCRS)
		metadata["transform_error"] = transformErr.Error()
	} else if transformResult != nil {
		metadata["transform_status"] = string(transformResult.Status)
		if transformResult.Engine != "" {
			metadata["transform_engine"] = transformResult.Engine
		}
		if transformResult.Message != "" {
			metadata["transform_message"] = transformResult.Message
		}
		if bbox := transformResult.BoundingBox; len(bbox) == 4 {
			metadata["render_bbox"] = bbox
		}
		metadata["render_srid"] = commonSpatial.SRIDWGS84

		switch transformResult.Status {
		case commonSpatial.TransformStatusNoop, commonSpatial.TransformStatusTransformed:
			if transformResult.GeoJSON != nil {
				geojson = toGeoJSONMap(transformResult.GeoJSON)
			}
		}
	}

	content := &models.ObjectPreviewContent{
		Kind:      "shapefile",
		GeoJSON:   geojson,
		Metadata:  metadata,
		Truncated: truncated,
	}

	return content, truncated, nil
}

func toGeoJSONMap(value interface{}) map[string]interface{} {
	result, ok := value.(map[string]interface{})
	if ok {
		return result
	}
	return nil
}

func downloadObjectToFile(opener func() (io.ReadCloser, error), target string) (int64, error) {
	if opener == nil {
		return 0, fmt.Errorf("对象读取器未就绪")
	}
	reader, err := opener()
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	file, err := os.Create(target)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	written, err := io.Copy(file, reader)
	if err != nil {
		return 0, err
	}
	return written, nil
}

func downloadSiblingToFile(basePath, ext string, provider ObjectSiblingStreamProvider, target string) error {
	if provider == nil {
		return fs.ErrNotExist
	}

	candidates := candidateSiblingPaths(basePath, ext)
	for _, key := range candidates {
		reader, err := provider(key)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}

		file, err := os.Create(target)
		if err != nil {
			reader.Close()
			return err
		}

		_, copyErr := io.Copy(file, reader)
		closeErr := reader.Close()
		fileErr := file.Close()

		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if fileErr != nil {
			return fileErr
		}
		return nil
	}
	return fs.ErrNotExist
}

func downloadSiblingText(basePath, ext string, provider ObjectSiblingStreamProvider) (string, error) {
	if provider == nil {
		return "", fs.ErrNotExist
	}
	candidates := candidateSiblingPaths(basePath, ext)
	for _, key := range candidates {
		reader, err := provider(key)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return "", err
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return strings.TrimSpace(string(data)), nil
	}
	return "", fs.ErrNotExist
}

func candidateSiblingPaths(basePath, ext string) []string {
	cleanExt := ensureLeadingDot(ext)
	base := strings.TrimSuffix(basePath, filepath.Ext(basePath))

	candidates := []string{
		base + cleanExt,
		base + strings.ToLower(cleanExt),
		base + strings.ToUpper(cleanExt),
	}

	seen := make(map[string]struct{}, len(candidates))
	unique := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, exists := seen[c]; exists {
			continue
		}
		seen[c] = struct{}{}
		unique = append(unique, c)
	}
	return unique
}

func ensureLeadingDot(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		return "." + ext
	}
	return ext
}
