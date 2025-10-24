package api

import (
	"net/http"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/gin-gonic/gin"
)

// TransformHandler 转换器处理器
type TransformHandler struct{}

// NewTransformHandler 创建转换器处理器
func NewTransformHandler() *TransformHandler {
	return &TransformHandler{}
}

// ListTransforms 列出所有可用的转换器
// GET /api/transforms
func (h *TransformHandler) ListTransforms(c *gin.Context) {
	transforms := pipeline.ListAllTransforms()

	c.JSON(http.StatusOK, gin.H{
		"total":      len(transforms),
		"transforms": transforms,
	})
}

// GetTransformCapability 获取转换器能力描述
// GET /api/transforms/:name
func (h *TransformHandler) GetTransformCapability(c *gin.Context) {
	name := c.Param("name")

	cap, exists := pipeline.GetTransformCapability(name)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "transform not found",
			"name":  name,
		})
		return
	}

	c.JSON(http.StatusOK, cap)
}

// ValidateTransformConfigRequest 验证转换器配置请求
type ValidateTransformConfigRequest struct {
	Config map[string]interface{} `json:"config" binding:"required"`
}

// ValidateTransformConfigResponse 验证转换器配置响应
type ValidateTransformConfigResponse struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

// ValidateTransformConfig 验证转换器配置
// POST /api/transforms/:name/validate
func (h *TransformHandler) ValidateTransformConfig(c *gin.Context) {
	name := c.Param("name")

	var req ValidateTransformConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"valid": false,
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	// 检查转换器是否存在
	if !pipeline.HasTransformRegistered(name) {
		c.JSON(http.StatusNotFound, gin.H{
			"valid": false,
			"error": "transform not found: " + name,
		})
		return
	}

	// 尝试创建转换器实例以验证配置
	_, err := pipeline.NewTransformByName(name, req.Config)
	if err != nil {
		c.JSON(http.StatusOK, ValidateTransformConfigResponse{
			Valid: false,
			Error: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, ValidateTransformConfigResponse{
		Valid: true,
	})
}

// TestTransformRequest 测试转换器请求
type TestTransformRequest struct {
	Config map[string]interface{}   `json:"config" binding:"required"`
	Sample []map[string]interface{} `json:"sample" binding:"required"` // 样本数据
}

// TestTransformResponse 测试转换器响应
type TestTransformResponse struct {
	Success bool                     `json:"success"`
	Error   string                   `json:"error,omitempty"`
	Input   []map[string]interface{} `json:"input"`
	Output  []map[string]interface{} `json:"output,omitempty"`
}

// TestTransform 测试转换器（使用样本数据）
// POST /api/transforms/:name/test
func (h *TransformHandler) TestTransform(c *gin.Context) {
	name := c.Param("name")

	var req TestTransformRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "invalid request body: " + err.Error(),
		})
		return
	}

	// 创建转换器实例
	transform, err := pipeline.NewTransformByName(name, req.Config)
	if err != nil {
		c.JSON(http.StatusBadRequest, TestTransformResponse{
			Success: false,
			Error:   "failed to create transform: " + err.Error(),
			Input:   req.Sample,
		})
		return
	}

	// 创建测试批次
	batch := &pipeline.DataBatch{
		Rows: req.Sample,
	}

	// 应用转换
	result, err := transform.Apply(c.Request.Context(), batch)
	if err != nil {
		c.JSON(http.StatusOK, TestTransformResponse{
			Success: false,
			Error:   "transform failed: " + err.Error(),
			Input:   req.Sample,
		})
		return
	}

	c.JSON(http.StatusOK, TestTransformResponse{
		Success: true,
		Input:   req.Sample,
		Output:  result.Rows,
	})
}

// GetTransformStats 获取转换器统计信息
// GET /api/transforms/stats
func (h *TransformHandler) GetTransformStats(c *gin.Context) {
	transforms := pipeline.ListAllTransforms()

	// 按类型分组统计
	typeCount := make(map[string]int)
	for _, t := range transforms {
		for _, supportedType := range t.SupportedTypes {
			typeCount[supportedType]++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_transforms":    len(transforms),
		"supported_types":     typeCount,
		"available_transforms": extractTransformNames(transforms),
	})
}

// extractTransformNames 提取转换器名称列表
func extractTransformNames(transforms []pipeline.TransformCapability) []string {
	names := make([]string, len(transforms))
	for i, t := range transforms {
		names[i] = t.Name
	}
	return names
}
