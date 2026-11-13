package api

import (
	"errors"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/system/internal/engineaccess"
	"github.com/gin-gonic/gin"
	"net/http"
)

// CheckManagerPGPreview godoc
// @Summary 核验 Manager PostgreSQL 预览范围 | Check Manager PostgreSQL preview coverage
// @Description 仅 addp-manager Tenant Service 可提交同次正式 DataItem 与完整 Provider ReadSet，System 在最终只读快照核验原用户、功能权限及当前 Grant；不重复查询 Meta，不签发访问权或缓存凭据。原用户可为普通 User 或严格限定 manager/data.preview 委托。 | Only addp-manager Tenant Service may submit same-request formal DataItem facts and the complete Provider ReadSet. System rechecks the original user, function permissions and current Grants in the final read-only snapshot without querying Meta or issuing authority. The original credential may be an ordinary User or a narrowly qualified manager/data.preview delegate.
// @Tags 源数据读取检查 | Source Data Read Checks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body client.ManagerPGReadCheckRequest true "原用户凭据与完整 PG 读取事实，不记录或持久化凭据 | Original user credential and complete PG read facts; never log or persist the credential"
// @Success 200 {object} engineaccess.SourceReadCheck "不可缓存的当次观察时刻 | Non-cacheable observation time"
// @Failure 400,401,403,404,500,503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.manager_pg_read_check.execute"]
// @Router /engine-access/read-checks/manager-pg-preview [post]
func (h *EngineAccessSourceReadCheckHandler) CheckManagerPGPreview(c *gin.Context) {
	h.checkManagerPG(c, true)
}

// CheckManagerPGProfileResult godoc
// @Summary 核验 Manager PostgreSQL 剖析结果范围 | Check Manager PostgreSQL profile-result coverage
// @Description 仅 addp-manager Tenant Service；使用实际历史结果冻结的完整 ReadSet 和同次正式 Meta 身份。最终重新核验原普通 User 及当前授权，不接受预览委托，不查询 Meta，不授予后台采样或 Grant 签发权限。 | Only addp-manager Tenant Service with the complete frozen historical result ReadSet and same-request formal Meta identity. Finally rechecks the original ordinary User and current authorization; no preview delegation, Meta query, background sampling authority or Grant issuance.
// @Tags 源数据读取检查 | Source Data Read Checks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body client.ManagerPGReadCheckRequest true "原用户凭据与完整历史 PG 读取事实 | Original user credential and complete historical PG read facts"
// @Success 200 {object} engineaccess.SourceReadCheck
// @Failure 400,401,403,404,500,503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.manager_pg_read_check.execute"]
// @Router /engine-access/read-checks/manager-pg-profile-result [post]
func (h *EngineAccessSourceReadCheckHandler) CheckManagerPGProfileResult(c *gin.Context) {
	h.checkManagerPG(c, false)
}

func (h *EngineAccessSourceReadCheckHandler) checkManagerPG(c *gin.Context, preview bool) {
	var request client.ManagerPGReadCheckRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
	if c.Request.URL.RawQuery != "" || commonapi.BindOptionalJSONStrict(c, &request) != nil || request.Validate() != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	_, tenant, _, err := iamTenantActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var result *engineaccess.SourceReadCheck
	result, err = h.service.CheckManagerPGRead(c.Request.Context(), int64(tenant), request, preview)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if result == nil || result.ObservedAt.IsZero() {
		respondIAMError(c, errors.New("incomplete source read observation"))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}
