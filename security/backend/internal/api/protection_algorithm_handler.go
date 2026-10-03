package api

import (
	"net/http"

	"github.com/addp/common/dataprotection"
	"github.com/gin-gonic/gin"
)

// ListProtectionAlgorithms godoc
// @Summary 查看内置脱敏算法 | List built-in protection algorithms
// @Description 返回版本、适用字段类型、参数名和输出类型，不允许上传可执行代码 | Returns versions, supported field types, parameter names and output types; executable uploads are not supported
// @Tags ProtectionAlgorithms
// @Produce json
// @Success 200 {array} dataprotection.Algorithm
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_baseline.read"]
// @Router /protection-algorithms [get]
// @Security BearerAuth
func ListProtectionAlgorithms(c *gin.Context) { c.JSON(http.StatusOK, dataprotection.ListAlgorithms()) }
