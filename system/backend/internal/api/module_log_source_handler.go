package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/runtimelog"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

// ReportLogSources godoc
// @Summary 上报受控运行日志来源 | Report controlled runtime log sources
// @Description 来源元数据不裁决业务启动结果；仅绑定节点的观察服务可调用；v2 必须提供有界扫描问题 | Source metadata does not determine startup outcome; bound observer service only; v2 requires bounded scan issues
// @Tags 运行时注册 | Runtime Registry
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body runtimelog.SourceReport true "有界来源报告 | Bounded source report"
// @Success 200 {object} object{accepted=bool}
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Failure 503 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.module_log_source.create"]
// @Router /runtime/module-log-source-observations [post]
func (h *ModuleRegistryHandler) ReportLogSources(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	a, ok := sharedauth.AuthContextFromGin(c)
	if !ok || a.Token.Type != "service_access_token" || a.Delegation != nil {
		runtimeLogError(c, 403, sysi18n.MsgRuntimeLogSourceForbidden)
		return
	}
	node := strings.TrimSpace(os.Getenv("ADDP_HOST_NODE_NAME"))
	if node == "" {
		runtimeLogError(c, 503, sysi18n.MsgRuntimeLogUnavailable)
		return
	}
	var report runtimelog.SourceReport
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20))
	d.DisallowUnknownFields()
	if d.Decode(&report) != nil || d.Decode(new(any)) != io.EOF {
		runtimeLogError(c, 400, sysi18n.MsgRuntimeLogSourceInvalid)
		return
	}
	retention, err := h.runtimeLogs.Retention()
	if err != nil {
		runtimeLogError(c, 503, sysi18n.MsgRuntimeLogUnavailable)
		return
	}
	err = h.service.SaveLogSources(c.Request.Context(), report, node, retention, time.Now().UTC())
	if err != nil {
		status, key := 500, sysi18n.MsgRuntimeLogUnavailable
		if errors.Is(err, service.ErrRuntimeLogsInvalid) {
			status, key = 400, sysi18n.MsgRuntimeLogSourceInvalid
		}
		if errors.Is(err, repository.ErrLogSourceConflict) {
			status, key = 409, sysi18n.MsgRuntimeLogSourceConflict
		}
		if errors.Is(err, repository.ErrLogSourceCapacity) {
			status = 503
		}
		runtimeLogError(c, status, key)
		return
	}
	c.JSON(200, gin.H{"accepted": true})
}

// ListLogSources godoc
// @Summary 查询未登记实例的运行日志来源 | List runtime log sources without a current registration
// @Description 未登记不代表启动失败；发现状态 unknown 不代表空目录；discovery_issues 区分观测超时和扫描问题 | Unregistered does not mean startup failure; unknown discovery does not mean an empty catalog; discovery_issues separates stale reports from scan issues
// @Tags 平台模块管理 | Platform Module Management
// @Produce json
// @Security BearerAuth
// @Param module_name query string false "模块名 | Module name"
// @Param node_name query string false "节点名精确匹配 | Exact node name"
// @Param role query string false "角色 | Role" Enums(backend,worker,scheduler,ingress)
// @Param time_from query string false "首次采集起始时间（含） | Inclusive first capture time"
// @Param time_to query string false "首次采集结束时间（不含） | Exclusive first capture time"
// @Param page query int false "页码 | Page number" default(1)
// @Param page_size query int false "每页条数 | Page size" default(20) maximum(100)
// @Success 200 {object} models.ModuleLogSourcePage
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.module_log.read"]
// @Router /platform/module-log-sources [get]
func (h *ModuleRegistryHandler) ListLogSources(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	allowed := map[string]bool{"module_name": true, "node_name": true, "role": true, "time_from": true, "time_to": true, "page": true, "page_size": true}
	for key, values := range c.Request.URL.Query() {
		if !allowed[key] || len(values) != 1 {
			runtimeLogError(c, 400, sysi18n.MsgRuntimeLogSourceInvalid)
			return
		}
	}
	f := models.ModuleLogSourceFilter{Module: c.Query("module_name"), Node: c.Query("node_name"), Role: c.Query("role"), Page: 1, PageSize: 20}
	var err error
	for key, value := range map[string]*int{"page": &f.Page, "page_size": &f.PageSize} {
		if c.Query(key) != "" {
			*value, err = strconv.Atoi(c.Query(key))
			if err != nil {
				runtimeLogError(c, 400, sysi18n.MsgRuntimeLogSourceInvalid)
				return
			}
		}
	}
	for key, value := range map[string]*time.Time{"time_from": &f.From, "time_to": &f.To} {
		if c.Query(key) != "" {
			*value, err = time.Parse(time.RFC3339Nano, c.Query(key))
			if err != nil {
				runtimeLogError(c, 400, sysi18n.MsgRuntimeLogSourceInvalid)
				return
			}
		}
	}
	result, err := h.service.ListLogSources(c.Request.Context(), f, strings.TrimSpace(os.Getenv("ADDP_HOST_NODE_NAME")), time.Now().UTC())
	if err != nil {
		status, key := 500, sysi18n.MsgRuntimeLogUnavailable
		if errors.Is(err, service.ErrRuntimeLogsInvalid) {
			status, key = 400, sysi18n.MsgRuntimeLogSourceInvalid
		}
		runtimeLogError(c, status, key)
		return
	}
	c.JSON(200, result)
}
