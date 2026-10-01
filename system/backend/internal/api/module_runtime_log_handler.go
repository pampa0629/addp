package api

import (
	"context"
	"errors"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net"
	"net/http"
	"strconv"
	"time"
)

// GetModuleRuntimeLogs godoc
// @Summary 查看指定模块实例运行日志 | Read runtime logs of a module instance
// @Description 有界查询已登记实例的日志；采集完整性未知，离线实例也可读取 | Bounded logs for a registered instance, including offline instances; collection completeness is unknown
// @Tags 平台模块管理 | Platform Module Management
// @Produce json
// @Security BearerAuth
// @Param module_name path string true "模块名 | Module name"
// @Param instance_id path string true "进程实例 ID | Process instance ID"
// @Param from query string true "UTC RFC3339 起始时间（含） | Inclusive UTC RFC3339 start"
// @Param to query string true "UTC RFC3339 结束时间（不含） | Exclusive UTC RFC3339 end"
// @Param level query string false "日志级别 | Log level" Enums(debug,info,warn,error,unknown)
// @Param keyword query string false "字面量关键字，最多 128 字符 | Literal keyword, at most 128 characters"
// @Param limit query int false "返回上限，默认 200，最多 1000 | Limit, default 200, maximum 1000"
// @Success 200 {object} service.RuntimeLogResult
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Failure 502 {object} models.ErrorResponse
// @Failure 503 {object} models.ErrorResponse
// @Failure 504 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.module_log.read"]
// @Router /platform/modules/{module_name}/instances/{instance_id}/logs [get]
func (h *ModuleRegistryHandler) GetModuleRuntimeLogs(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	q := service.RuntimeLogQuery{Level: c.Query("level"), Keyword: c.Query("keyword"), Limit: 200}
	allowed := map[string]bool{"from": true, "to": true, "level": true, "keyword": true, "limit": true}
	for key, values := range c.Request.URL.Query() {
		if !allowed[key] || len(values) != 1 {
			runtimeLogError(c, 400, sysi18n.MsgRuntimeLogInvalid)
			return
		}
	}
	var err error
	q.From, err = time.Parse(time.RFC3339Nano, c.Query("from"))
	if err != nil {
		runtimeLogError(c, 400, sysi18n.MsgRuntimeLogInvalid)
		return
	}
	q.To, err = time.Parse(time.RFC3339Nano, c.Query("to"))
	if err != nil {
		runtimeLogError(c, 400, sysi18n.MsgRuntimeLogInvalid)
		return
	}
	if c.Query("limit") != "" {
		q.Limit, err = strconv.Atoi(c.Query("limit"))
		if err != nil {
			runtimeLogError(c, 400, sysi18n.MsgRuntimeLogInvalid)
			return
		}
	}
	if q.Validate() != nil {
		runtimeLogError(c, 400, sysi18n.MsgRuntimeLogInvalid)
		return
	}
	module, err := h.service.GetModule(c.Param("module_name"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			runtimeLogError(c, 404, sysi18n.MsgModuleRuntimeInstanceMissing)
		} else {
			runtimeLogError(c, 500, sysi18n.MsgRuntimeLogUnavailable)
		}
		return
	}
	found := false
	for _, instance := range module.Instances {
		if instance.InstanceID == c.Param("instance_id") {
			found = true
			break
		}
	}
	if !found {
		runtimeLogError(c, 404, sysi18n.MsgModuleRuntimeInstanceMissing)
		return
	}
	result, err := h.runtimeLogs.Query(c.Request.Context(), module.ModuleName, c.Param("instance_id"), q)
	if err != nil {
		status := 502
		key := sysi18n.MsgRuntimeLogUnavailable
		if errors.Is(err, service.ErrRuntimeLogsDisabled) {
			status = 503
			key = sysi18n.MsgRuntimeLogDisabled
		}
		if errors.Is(err, service.ErrRuntimeLogsBusy) {
			status = 503
		}
		var timeout net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
			status = 504
		}
		runtimeLogError(c, status, key)
		return
	}
	c.JSON(http.StatusOK, result)
}
func runtimeLogError(c *gin.Context, status int, key string) {
	c.JSON(status, models.ErrorResponse{Error: commoni18n.T(c, key)})
}
