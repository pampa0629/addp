package api

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/logger"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
)

const (
	ksplatHeaderSizeBytes       = 4096
	ksplatExpectedContentType   = "application/vnd.gaussian-ksplat"
	ksplatCurrentVersionMajor   = 0
	ksplatCurrentVersionMinor   = 1
	ksplatMaxReasonableSections = 1_000_000
)

type GaussianSplatKSplatHandler struct {
	repo          *repository.GaussianSplatKSplatRepository
	minioClient   *minio.Client
	defaultBucket string
}

func NewGaussianSplatKSplatHandler(repo *repository.GaussianSplatKSplatRepository, minioClient *minio.Client, defaultBucket string) *GaussianSplatKSplatHandler {
	return &GaussianSplatKSplatHandler{repo: repo, minioClient: minioClient, defaultBucket: defaultBucket}
}

type GaussianSplatKSplatInspectResponse struct {
	ID                  uint                              `json:"id"`
	Status              string                            `json:"status"`
	FileName            string                            `json:"file_name,omitempty"`
	StorageRef          string                            `json:"storage_ref,omitempty"`
	ObjectBucket        string                            `json:"object_bucket,omitempty"`
	ObjectName          string                            `json:"object_name,omitempty"`
	ExpectedContentType string                            `json:"expected_content_type"`
	ObjectContentType   string                            `json:"object_content_type,omitempty"`
	ObjectSizeBytes     int64                             `json:"object_size_bytes"`
	RecordedSizeBytes   int64                             `json:"recorded_size_bytes,omitempty"`
	BytesInspected      int                               `json:"bytes_inspected"`
	HeaderSignatureHex  string                            `json:"header_signature_hex,omitempty"`
	Header              *GaussianSplatKSplatHeader        `json:"header,omitempty"`
	Checks              []GaussianSplatKSplatCheck        `json:"checks"`
	Summary             GaussianSplatKSplatInspectSummary `json:"summary"`
}

type GaussianSplatKSplatHeader struct {
	VersionMajor               uint8     `json:"version_major"`
	VersionMinor               uint8     `json:"version_minor"`
	MaxSectionCount            uint32    `json:"max_section_count"`
	SectionCount               uint32    `json:"section_count"`
	MaxSplatCount              uint32    `json:"max_splat_count"`
	SplatCount                 uint32    `json:"splat_count"`
	CompressionLevel           uint16    `json:"compression_level"`
	SceneCenter                []float32 `json:"scene_center"`
	MinSphericalHarmonicsCoeff float32   `json:"min_spherical_harmonics_coeff,omitempty"`
	MaxSphericalHarmonicsCoeff float32   `json:"max_spherical_harmonics_coeff,omitempty"`
}

type GaussianSplatKSplatCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type GaussianSplatKSplatInspectSummary struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// InspectGaussianSplatKSplat 检查 Manager 3DGS - KSplat 快显内容。
// @Summary 检查 3DGS - KSplat 快显 | Inspect 3DGS - KSplat quick view result
// @Description 只读取 Manager infra MinIO 中 KSplat artifact 的对象元信息和头部，返回轻量健康检查事实，不下载完整内容。| Read object metadata and the KSplat header only, returning lightweight health facts without downloading the full artifact.
// @Tags Manager
// @Produce json
// @Param id path int true "gaussian splat KSplat ID"
// @Success 200 {object} GaussianSplatKSplatInspectResponse "KSplat 快显检查结果 | KSplat inspection result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "KSplat 不存在或未就绪 | KSplat not found or not ready"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /gaussian_splat_ksplat/{id}/inspect [get]
// @Security BearerAuth
func (h *GaussianSplatKSplatHandler) InspectGaussianSplatKSplat(c *gin.Context) {
	if h == nil || h.repo == nil || h.minioClient == nil {
		commonAPI.InternalServerError(c, "gaussian splat KSplat service is not available")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		commonAPI.BadRequestError(c, "invalid gaussian splat KSplat id")
		return
	}
	tenantID := uint(1)
	if ctxTenantID := tenantIDFromContext(c); ctxTenantID != nil && *ctxTenantID > 0 {
		tenantID = *ctxTenantID
	}
	result, err := h.repo.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	if result == nil || result.Status != models.GaussianSplatKSplatStatusReady {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "gaussian splat KSplat is not ready")
		return
	}
	bucket, objectName, err := rastercogref.ObjectLocation(result.StorageRef, h.defaultBucket)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	objInfo, err := h.minioClient.StatObject(c.Request.Context(), bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "gaussian splat KSplat object not found")
		return
	}
	header, inspected, signature, readErr := h.readKSplatHeader(c, bucket, objectName, objInfo.Size)
	response := inspectGaussianSplatKSplatResult(result, bucket, objectName, objInfo, header, inspected, signature, readErr)
	c.JSON(http.StatusOK, response)
}

// GetGaussianSplatKSplatContent 返回 Manager 3DGS - KSplat 快显内容。
// @Summary 读取 3DGS - KSplat 快显 | Read 3DGS - KSplat quick view result
// @Description 按 gaussian splat KSplat id 返回 Manager infra MinIO 中的 KSplat 内容，支持 HTTP Range。该接口只读取 Manager 拥有生命周期且状态为 ready 的 KSplat 快显结果。 | Return KSplat content from Manager infra MinIO by result id with HTTP Range support. Only ready Manager-owned results are readable.
// @Tags Manager
// @Produce octet-stream
// @Param id path int true "gaussian splat KSplat ID"
// @Success 200 "KSplat 内容流 | KSplat content stream"
// @Success 206 "部分 KSplat 内容流 | Partial KSplat content stream"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "KSplat 不存在或未就绪 | KSplat not found or not ready"
// @Failure 416 {object} map[string]interface{} "Range 不可满足 | Range not satisfiable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /gaussian_splat_ksplat/{id}/content [get]
// @Security BearerAuth
func (h *GaussianSplatKSplatHandler) GetGaussianSplatKSplatContent(c *gin.Context) {
	if h == nil || h.repo == nil || h.minioClient == nil {
		commonAPI.InternalServerError(c, "gaussian splat KSplat service is not available")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		commonAPI.BadRequestError(c, "invalid gaussian splat KSplat id")
		return
	}
	tenantID := uint(1)
	if ctxTenantID := tenantIDFromContext(c); ctxTenantID != nil && *ctxTenantID > 0 {
		tenantID = *ctxTenantID
	}
	result, err := h.repo.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	if result == nil || result.Status != models.GaussianSplatKSplatStatusReady {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "gaussian splat KSplat is not ready")
		return
	}
	bucket, objectName, err := rastercogref.ObjectLocation(result.StorageRef, h.defaultBucket)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	objInfo, err := h.minioClient.StatObject(c.Request.Context(), bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "gaussian splat KSplat object not found")
		return
	}
	getOpts := minio.GetObjectOptions{}
	rangeHeader := c.GetHeader("Range")
	contentLength := objInfo.Size
	contentRange := ""
	statusCode := http.StatusOK
	if rangeHeader != "" {
		start, end, err := parseGaussianSplatHTTPRange(rangeHeader, objInfo.Size)
		if err != nil {
			if errors.Is(err, errGaussianSplatKSplatInvalidRange) {
				commonAPI.ErrorResponse(c, http.StatusRequestedRangeNotSatisfiable, err.Error())
				return
			}
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		if err := getOpts.SetRange(start, end); err != nil {
			commonAPI.ErrorResponse(c, http.StatusRequestedRangeNotSatisfiable, err.Error())
			return
		}
		contentLength = end - start + 1
		contentRange = "bytes " + strconv.FormatInt(start, 10) + "-" + strconv.FormatInt(end, 10) + "/" + strconv.FormatInt(objInfo.Size, 10)
		statusCode = http.StatusPartialContent
	}
	obj, err := h.minioClient.GetObject(c.Request.Context(), bucket, objectName, getOpts)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	defer obj.Close()

	contentType := "application/vnd.gaussian-ksplat"
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Disposition", storageStreamContentDisposition(result.FileName, contentType))
	c.Header("Content-Length", strconv.FormatInt(contentLength, 10))
	if contentRange != "" {
		c.Header("Content-Range", contentRange)
	}
	c.Status(statusCode)
	if _, err := io.Copy(c.Writer, obj); err != nil {
		logger.L().Error("gaussian splat KSplat stream failed", "error", err, "result_id", result.ID)
	}
}

func (h *GaussianSplatKSplatHandler) readKSplatHeader(c *gin.Context, bucket, objectName string, objectSize int64) (*GaussianSplatKSplatHeader, int, string, error) {
	if objectSize <= 0 {
		return nil, 0, "", errors.New("KSplat object is empty")
	}
	readEnd := int64(ksplatHeaderSizeBytes - 1)
	if objectSize-1 < readEnd {
		readEnd = objectSize - 1
	}
	getOpts := minio.GetObjectOptions{}
	if err := getOpts.SetRange(0, readEnd); err != nil {
		return nil, 0, "", err
	}
	obj, err := h.minioClient.GetObject(c.Request.Context(), bucket, objectName, getOpts)
	if err != nil {
		return nil, 0, "", err
	}
	defer obj.Close()
	headerBytes, err := io.ReadAll(io.LimitReader(obj, ksplatHeaderSizeBytes))
	if err != nil {
		return nil, len(headerBytes), "", err
	}
	signatureSize := len(headerBytes)
	if signatureSize > 16 {
		signatureSize = 16
	}
	signature := ""
	if signatureSize > 0 {
		signature = hex.EncodeToString(headerBytes[:signatureSize])
	}
	header, err := parseKSplatHeader(headerBytes)
	return header, len(headerBytes), signature, err
}

func inspectGaussianSplatKSplatResult(result *models.GaussianSplatKSplat, bucket, objectName string, objInfo minio.ObjectInfo, header *GaussianSplatKSplatHeader, inspected int, signature string, headerErr error) GaussianSplatKSplatInspectResponse {
	checks := []GaussianSplatKSplatCheck{
		inspectCheck("object_exists", objInfo.Size > 0, "KSplat object is readable", "KSplat object is empty"),
		inspectWarningCheck("file_extension", strings.HasSuffix(strings.ToLower(result.FileName), ".ksplat"), "file extension is .ksplat", "file extension is not .ksplat"),
		inspectWarningCheck("size_matches", result.SizeBytes <= 0 || result.SizeBytes == objInfo.Size, "recorded size matches object size", "recorded size differs from object size"),
		inspectWarningCheck("content_type", objInfo.ContentType == "" || strings.EqualFold(objInfo.ContentType, ksplatExpectedContentType), "content type is compatible", "content type differs from KSplat"),
		inspectCheck("header_readable", headerErr == nil && inspected >= ksplatHeaderSizeBytes, "KSplat header is readable", headerReadErrorMessage(headerErr, inspected)),
	}
	if header != nil {
		checks = append(checks,
			inspectCheck("header_version", header.VersionMajor == ksplatCurrentVersionMajor && header.VersionMinor <= ksplatCurrentVersionMinor, "KSplat header version is supported", "KSplat header version is not supported"),
			inspectCheck("section_count", header.MaxSectionCount > 0 && header.SectionCount <= header.MaxSectionCount && header.MaxSectionCount <= ksplatMaxReasonableSections, "section count is plausible", "section count is not plausible"),
			inspectCheck("splat_count", header.SplatCount > 0 && header.SplatCount <= header.MaxSplatCount, "splat count is plausible", "splat count is empty or exceeds max splat count"),
			inspectCheck("compression_level", header.CompressionLevel <= 2, "compression level is supported", "compression level is not supported"),
			inspectCheck("minimum_size", objInfo.Size >= minimumKSplatObjectSize(header), "object size covers declared header layout", "object size is smaller than declared header layout"),
		)
	}

	summary := summarizeKSplatInspection(checks)
	return GaussianSplatKSplatInspectResponse{
		ID:                  result.ID,
		Status:              result.Status,
		FileName:            result.FileName,
		StorageRef:          result.StorageRef,
		ObjectBucket:        bucket,
		ObjectName:          objectName,
		ExpectedContentType: ksplatExpectedContentType,
		ObjectContentType:   objInfo.ContentType,
		ObjectSizeBytes:     objInfo.Size,
		RecordedSizeBytes:   result.SizeBytes,
		BytesInspected:      inspected,
		HeaderSignatureHex:  signature,
		Header:              header,
		Checks:              checks,
		Summary:             summary,
	}
}

func parseKSplatHeader(headerBytes []byte) (*GaussianSplatKSplatHeader, error) {
	if len(headerBytes) < ksplatHeaderSizeBytes {
		return nil, errors.New("KSplat header is incomplete")
	}
	header := &GaussianSplatKSplatHeader{
		VersionMajor:               headerBytes[0],
		VersionMinor:               headerBytes[1],
		MaxSectionCount:            binary.LittleEndian.Uint32(headerBytes[4:8]),
		SectionCount:               binary.LittleEndian.Uint32(headerBytes[8:12]),
		MaxSplatCount:              binary.LittleEndian.Uint32(headerBytes[12:16]),
		SplatCount:                 binary.LittleEndian.Uint32(headerBytes[16:20]),
		CompressionLevel:           binary.LittleEndian.Uint16(headerBytes[20:22]),
		SceneCenter:                []float32{littleEndianFloat32(headerBytes[24:28]), littleEndianFloat32(headerBytes[28:32]), littleEndianFloat32(headerBytes[32:36])},
		MinSphericalHarmonicsCoeff: littleEndianFloat32(headerBytes[36:40]),
		MaxSphericalHarmonicsCoeff: littleEndianFloat32(headerBytes[40:44]),
	}
	return header, nil
}

func littleEndianFloat32(raw []byte) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(raw))
}

func inspectCheck(name string, ok bool, okMessage, failMessage string) GaussianSplatKSplatCheck {
	if ok {
		return GaussianSplatKSplatCheck{Name: name, Status: "ok", Message: okMessage}
	}
	return GaussianSplatKSplatCheck{Name: name, Status: "failed", Message: failMessage}
}

func inspectWarningCheck(name string, ok bool, okMessage, warnMessage string) GaussianSplatKSplatCheck {
	if ok {
		return GaussianSplatKSplatCheck{Name: name, Status: "ok", Message: okMessage}
	}
	return GaussianSplatKSplatCheck{Name: name, Status: "warning", Message: warnMessage}
}

func headerReadErrorMessage(err error, inspected int) string {
	if err != nil {
		return err.Error()
	}
	return "KSplat header is incomplete: inspected " + strconv.Itoa(inspected) + " bytes"
}

func minimumKSplatObjectSize(header *GaussianSplatKSplatHeader) int64 {
	if header == nil || header.MaxSectionCount == 0 {
		return ksplatHeaderSizeBytes
	}
	return int64(ksplatHeaderSizeBytes) + int64(header.MaxSectionCount)*1024
}

func summarizeKSplatInspection(checks []GaussianSplatKSplatCheck) GaussianSplatKSplatInspectSummary {
	hasWarning := false
	warningMessage := ""
	for _, check := range checks {
		if check.Status == "failed" {
			return GaussianSplatKSplatInspectSummary{Status: "failed", Message: check.Message}
		}
		if check.Status == "warning" && !hasWarning {
			hasWarning = true
			warningMessage = check.Message
		}
	}
	if hasWarning {
		return GaussianSplatKSplatInspectSummary{Status: "warning", Message: warningMessage}
	}
	return GaussianSplatKSplatInspectSummary{Status: "ok", Message: "KSplat quick view artifact is readable"}
}

var errGaussianSplatKSplatInvalidRange = errors.New("invalid range")

func parseGaussianSplatHTTPRange(header string, size int64) (int64, int64, error) {
	if size <= 0 {
		return 0, 0, errGaussianSplatKSplatInvalidRange
	}
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, errGaussianSplatKSplatInvalidRange
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes="))
	if strings.Contains(spec, ",") {
		return 0, 0, errGaussianSplatKSplatInvalidRange
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, errGaussianSplatKSplatInvalidRange
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, errGaussianSplatKSplatInvalidRange
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, nil
	}
	start, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, errGaussianSplatKSplatInvalidRange
	}
	end := size - 1
	if strings.TrimSpace(parts[1]) != "" {
		end, err = strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil || end < start {
			return 0, 0, errGaussianSplatKSplatInvalidRange
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, nil
}
