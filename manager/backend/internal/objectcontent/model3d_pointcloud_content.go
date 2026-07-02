package objectcontent

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/models"
)

const (
	pointCloudPreviewPointLimit = 5000
	lasPreviewHeaderMinSize     = 227
)

type model3DContentHandler struct {
	baseContentHandler
}

func (h *model3DContentHandler) Matches(req *ObjectContentRequest) bool {
	if req == nil || !h.baseContentHandler.Matches(req) {
		return false
	}
	if format.NormalizeFormat(req.Format) != format.FormatPLY {
		return true
	}
	return strings.EqualFold(commonJSON.String(req.Attributes, "item", "data_type"), string(datatype.Model3D))
}

func (h *model3DContentHandler) Handle(_ context.Context, req *ObjectContentRequest, _ ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	metadata := buildPreviewMetadata(req, 0)
	if req != nil && strings.TrimSpace(req.PreviewURL) != "" {
		content := &models.ObjectPreviewContent{
			Kind:            models.ObjectPreviewKindModel3D,
			URL:             strings.TrimSpace(req.PreviewURL),
			PreviewMaterial: models.PreviewMaterialURL,
			Metadata:        metadata,
		}
		setFrontendRenderer(content, model3DFrontendRenderer(req))
		return decoratePreviewContent(content), false, nil
	}
	return decoratePreviewContent(&models.ObjectPreviewContent{
		Kind:     models.ObjectPreviewKindUnsupported,
		Metadata: metadata,
	}), false, nil
}

func model3DFrontendRenderer(req *ObjectContentRequest) string {
	if req != nil && format.NormalizeFormat(req.Format) == format.Format3DTiles {
		return string(format.Format3DTiles)
	}
	return models.ObjectPreviewKindModel3D
}

type pointCloudContentHandler struct {
	baseContentHandler
}

type gaussianSplatContentHandler struct {
	baseContentHandler
}

func (h *gaussianSplatContentHandler) Matches(req *ObjectContentRequest) bool {
	if req == nil || !h.baseContentHandler.Matches(req) {
		return false
	}
	return strings.EqualFold(commonJSON.String(req.Attributes, "item", "data_type"), string(datatype.GaussianSplat))
}

func (h *gaussianSplatContentHandler) Handle(_ context.Context, req *ObjectContentRequest, _ ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	metadata := buildPreviewMetadata(req, 0)
	metadata["preview_artifact_status"] = "ready"
	metadata["preview_artifact_task_type"] = ""
	previewURL := ""
	if req != nil {
		previewURL = strings.TrimSpace(req.PreviewURL)
	}
	if previewURL == "" {
		metadata["preview_artifact_status"] = "preview_url_missing"
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind:             models.ObjectPreviewKindGaussianSplat,
			PreviewMaterial:  models.PreviewMaterialUnsupported,
			FrontendRenderer: models.ObjectPreviewKindGaussianSplat,
			Metadata:         metadata,
		}), false, nil
	}
	content := &models.ObjectPreviewContent{
		Kind:             models.ObjectPreviewKindGaussianSplat,
		PreviewMaterial:  models.PreviewMaterialURL,
		FrontendRenderer: models.ObjectPreviewKindGaussianSplat,
		URL:              previewURL,
		Metadata:         metadata,
	}
	return decoratePreviewContent(content), false, nil
}

func (h *pointCloudContentHandler) HandleStream(ctx context.Context, req *ObjectContentRequest, streamer ObjectStreamProvider) (*models.ObjectPreviewContent, bool, error) {
	if streamer == nil {
		return h.Handle(ctx, req, nil)
	}
	tmpFile, err := os.CreateTemp("", "point-cloud-preview-*.las")
	if err != nil {
		return nil, false, fmt.Errorf("创建点云预览临时文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	reader, err := streamer()
	if err != nil {
		return nil, false, fmt.Errorf("获取点云对象流失败: %w", err)
	}
	defer reader.Close()

	if _, err := io.Copy(tmpFile, reader); err != nil {
		return nil, false, fmt.Errorf("写入点云预览临时文件失败: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return nil, false, fmt.Errorf("关闭点云预览临时文件失败: %w", err)
	}

	file, err := os.Open(tmpPath)
	if err != nil {
		return nil, false, fmt.Errorf("打开点云预览临时文件失败: %w", err)
	}
	defer file.Close()
	return h.previewLAS(ctx, req, file)
}

func (h *pointCloudContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	if fetcher == nil {
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind:     models.ObjectPreviewKindUnsupported,
			Metadata: buildPreviewMetadata(req, 0),
		}), false, nil
	}
	data, truncated, err := fetcher(64 * 1024 * 1024)
	if err != nil {
		return nil, false, err
	}
	content, _, err := h.previewLAS(ctx, req, newBytesReadSeeker(data))
	if err != nil {
		return nil, false, err
	}
	if truncated && content != nil {
		content.Truncated = true
	}
	return content, truncated, nil
}

func (h *pointCloudContentHandler) previewLAS(ctx context.Context, req *ObjectContentRequest, reader io.ReadSeeker) (*models.ObjectPreviewContent, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}
	header, err := readLASPreviewHeader(reader)
	if err != nil {
		return nil, false, err
	}
	points, err := sampleLASPreviewPoints(ctx, reader, header, pointCloudPreviewPointLimit)
	if err != nil {
		return nil, false, err
	}
	metadata := buildPreviewMetadata(req, 0)
	metadata["format"] = string(format.FormatLAS)
	metadata["point_count"] = header.PointCount
	metadata["sample_count"] = len(points)
	metadata["point_format"] = header.PointFormat
	metadata["point_record_length"] = header.PointRecordLength
	metadata["bounds_3d"] = header.Bounds3D()

	content := &models.ObjectPreviewContent{
		Kind:             models.ObjectPreviewKindPointCloud,
		PreviewMaterial:  models.PreviewMaterialJSON,
		FrontendRenderer: models.ObjectPreviewKindPointCloud,
		JSON: map[string]interface{}{
			"format":       string(format.FormatLAS),
			"point_count":  header.PointCount,
			"sample_count": len(points),
			"bounds_3d":    header.Bounds3D(),
			"points":       points,
		},
		Metadata:  metadata,
		Truncated: header.PointCount > int64(len(points)),
	}
	return decoratePreviewContent(content), content.Truncated, nil
}

type bytesReadSeeker struct {
	data []byte
	pos  int64
}

func newBytesReadSeeker(data []byte) *bytesReadSeeker {
	return &bytesReadSeeker{data: data}
}

func (r *bytesReadSeeker) Read(p []byte) (int, error) {
	if r.pos >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += int64(n)
	return n, nil
}

func (r *bytesReadSeeker) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = r.pos + offset
	case io.SeekEnd:
		next = int64(len(r.data)) + offset
	default:
		return 0, fmt.Errorf("invalid seek whence %d", whence)
	}
	if next < 0 {
		return 0, fmt.Errorf("negative seek offset %d", next)
	}
	r.pos = next
	return r.pos, nil
}

type lasPreviewHeader struct {
	PointDataOffset   int64
	PointFormat       uint8
	PointRecordLength int
	PointCount        int64
	ScaleX            float64
	ScaleY            float64
	ScaleZ            float64
	OffsetX           float64
	OffsetY           float64
	OffsetZ           float64
	MinX              float64
	MinY              float64
	MinZ              float64
	MaxX              float64
	MaxY              float64
	MaxZ              float64
}

func (h lasPreviewHeader) Bounds3D() map[string]interface{} {
	return map[string]interface{}{
		"min_x": h.MinX,
		"min_y": h.MinY,
		"min_z": h.MinZ,
		"max_x": h.MaxX,
		"max_y": h.MaxY,
		"max_z": h.MaxZ,
	}
}

func readLASPreviewHeader(reader io.ReadSeeker) (lasPreviewHeader, error) {
	header := make([]byte, 375)
	n, err := io.ReadFull(reader, header)
	if err != nil && err != io.ErrUnexpectedEOF {
		return lasPreviewHeader{}, err
	}
	if n < lasPreviewHeaderMinSize {
		return lasPreviewHeader{}, fmt.Errorf("LAS header too small: %d bytes", n)
	}
	header = header[:n]
	if string(header[:4]) != "LASF" {
		return lasPreviewHeader{}, fmt.Errorf("invalid LAS signature")
	}
	pointFormat := header[104] & 0x3f
	pointRecordLength := int(binary.LittleEndian.Uint16(header[105:107]))
	pointCount := int64(binary.LittleEndian.Uint32(header[107:111]))
	if len(header) >= 255 {
		if extended := int64(binary.LittleEndian.Uint64(header[247:255])); extended > 0 {
			pointCount = extended
		}
	}
	if pointRecordLength <= 0 {
		return lasPreviewHeader{}, fmt.Errorf("invalid LAS point record length")
	}
	return lasPreviewHeader{
		PointDataOffset:   int64(binary.LittleEndian.Uint32(header[96:100])),
		PointFormat:       pointFormat,
		PointRecordLength: pointRecordLength,
		PointCount:        pointCount,
		ScaleX:            math.Float64frombits(binary.LittleEndian.Uint64(header[131:139])),
		ScaleY:            math.Float64frombits(binary.LittleEndian.Uint64(header[139:147])),
		ScaleZ:            math.Float64frombits(binary.LittleEndian.Uint64(header[147:155])),
		OffsetX:           math.Float64frombits(binary.LittleEndian.Uint64(header[155:163])),
		OffsetY:           math.Float64frombits(binary.LittleEndian.Uint64(header[163:171])),
		OffsetZ:           math.Float64frombits(binary.LittleEndian.Uint64(header[171:179])),
		MaxX:              math.Float64frombits(binary.LittleEndian.Uint64(header[179:187])),
		MinX:              math.Float64frombits(binary.LittleEndian.Uint64(header[187:195])),
		MaxY:              math.Float64frombits(binary.LittleEndian.Uint64(header[195:203])),
		MinY:              math.Float64frombits(binary.LittleEndian.Uint64(header[203:211])),
		MaxZ:              math.Float64frombits(binary.LittleEndian.Uint64(header[211:219])),
		MinZ:              math.Float64frombits(binary.LittleEndian.Uint64(header[219:227])),
	}, nil
}

func sampleLASPreviewPoints(ctx context.Context, reader io.ReadSeeker, header lasPreviewHeader, limit int) ([]map[string]interface{}, error) {
	if header.PointCount <= 0 || limit <= 0 {
		return []map[string]interface{}{}, nil
	}
	count := int(header.PointCount)
	if count > limit {
		count = limit
	}
	stride := int64(1)
	if header.PointCount > int64(count) {
		stride = header.PointCount / int64(count)
		if stride <= 0 {
			stride = 1
		}
	}
	record := make([]byte, header.PointRecordLength)
	points := make([]map[string]interface{}, 0, count)
	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		pointIndex := int64(i) * stride
		offset := header.PointDataOffset + pointIndex*int64(header.PointRecordLength)
		if _, err := reader.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
		n, err := io.ReadFull(reader, record)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}
		if n < 14 {
			break
		}
		x := float64(int32(binary.LittleEndian.Uint32(record[0:4])))*header.ScaleX + header.OffsetX
		y := float64(int32(binary.LittleEndian.Uint32(record[4:8])))*header.ScaleY + header.OffsetY
		z := float64(int32(binary.LittleEndian.Uint32(record[8:12])))*header.ScaleZ + header.OffsetZ
		point := map[string]interface{}{
			"x": x,
			"y": y,
			"z": z,
		}
		if n >= 14 {
			point["intensity"] = int(binary.LittleEndian.Uint16(record[12:14]))
		}
		points = append(points, point)
	}
	return points, nil
}
