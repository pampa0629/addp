package modulelifecycle

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/addp/common/buildinfo"
	commonclient "github.com/addp/common/client"
	"github.com/gin-gonic/gin"
)

const (
	CheckReady    = "ready"
	CheckNotReady = "not_ready"
)

type CheckResult struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code,omitempty"`
}

func CancelRuntimeOnFatal(registration *commonclient.ModuleRegistrationLifecycle, cancel context.CancelFunc) {
	if registration == nil || cancel == nil {
		return
	}
	go func() {
		if err, ok := <-registration.Fatal(); ok && err != nil {
			log.Printf("module registration lifecycle failed; stopping process: %v", err)
			cancel()
		}
	}()
}

type CheckFunc func(context.Context) CheckResult

type ReadyResponse struct {
	Status            string                               `json:"status"`
	Module            string                               `json:"module"`
	Role              string                               `json:"role,omitempty"`
	InstanceID        string                               `json:"instance_id,omitempty"`
	RegistrationState commonclient.ModuleRegistrationState `json:"registration_state,omitempty"`
	Checks            []CheckResult                        `json:"checks"`
	BuildID           string                               `json:"build_id"`
	GitCommit         string                               `json:"git_commit"`
	SourceFingerprint string                               `json:"source_fingerprint"`
	BuiltAt           string                               `json:"built_at"`
	StartedAt         string                               `json:"started_at"`
}

// Controller is the unique process-local readiness projection used by HTTP
// backends. System remains the owner of the authoritative module lease.
type Controller struct {
	module string
	role   string

	mu           sync.RWMutex
	registration *commonclient.ModuleRegistrationLifecycle
	checks       []CheckFunc
}

func NewBusiness(module, role string, checks ...CheckFunc) *Controller {
	if strings.TrimSpace(role) == "" {
		role = commonclient.ModuleRuntimeRoleBackend
	}
	if len(checks) == 0 {
		checks = []CheckFunc{StaticCheck("local_dependencies", true, "")}
	}
	return &Controller{module: module, role: role, checks: checks}
}

func NewStandalone(module string, checks ...CheckFunc) *Controller {
	return &Controller{module: module, checks: checks}
}

func StaticCheck(name string, ready bool, errorCode string) CheckFunc {
	return func(context.Context) CheckResult {
		if ready {
			return CheckResult{Name: name, Status: CheckReady}
		}
		return CheckResult{Name: name, Status: CheckNotReady, ErrorCode: errorCode}
	}
}

func (c *Controller) AttachRegistration(registration *commonclient.ModuleRegistrationLifecycle) {
	c.mu.Lock()
	c.registration = registration
	c.mu.Unlock()
}

func (c *Controller) RegisterHealthRoutes(router gin.IRoutes) {
	router.GET("/health/live", c.Live)
	router.GET("/health/ready", c.Ready)
}

func (c *Controller) Live(ginContext *gin.Context) {
	health := buildinfo.Health(c.module)
	health.Status = "live"
	ginContext.JSON(http.StatusOK, health)
}

func (c *Controller) Ready(ginContext *gin.Context) {
	response, ready := c.Readiness(ginContext.Request.Context())
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	ginContext.JSON(status, response)
}

func (c *Controller) Readiness(ctx context.Context) (ReadyResponse, bool) {
	health := buildinfo.Health(c.module)
	response := ReadyResponse{
		Status:            "ready",
		Module:            c.module,
		Checks:            make([]CheckResult, 0, len(c.checks)+1),
		BuildID:           health.BuildID,
		GitCommit:         health.GitCommit,
		SourceFingerprint: health.SourceFingerprint,
		BuiltAt:           health.BuiltAt,
		StartedAt:         health.StartedAt,
	}
	ready := true
	for _, check := range c.checks {
		result := check(ctx)
		if result.Status != CheckReady {
			ready = false
			result.Status = CheckNotReady
		}
		response.Checks = append(response.Checks, result)
	}

	if c.role != "" {
		c.mu.RLock()
		registration := c.registration
		c.mu.RUnlock()
		snapshot := commonclient.ModuleRegistrationSnapshot{
			Role: c.role, State: commonclient.ModuleRegistrationStarting,
		}
		if registration != nil {
			snapshot = registration.Snapshot()
		}
		response.Role = snapshot.Role
		response.InstanceID = snapshot.InstanceID
		response.RegistrationState = snapshot.State
		registrationCheck := CheckResult{Name: "system_registration", Status: CheckReady}
		if snapshot.State != commonclient.ModuleRegistrationRegistered {
			ready = false
			registrationCheck.Status = CheckNotReady
			registrationCheck.ErrorCode = snapshot.ErrorCode
			if registrationCheck.ErrorCode == "" {
				registrationCheck.ErrorCode = "system_registration_unavailable"
			}
		}
		response.Checks = append(response.Checks, registrationCheck)
	}
	if !ready {
		response.Status = "not_ready"
	}
	return response, ready
}

func (c *Controller) RequireReady() gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		if _, ready := c.Readiness(ginContext.Request.Context()); !ready {
			ginContext.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error":      "module is not ready",
				"error_code": "module_not_ready",
			})
			return
		}
		ginContext.Next()
	}
}
