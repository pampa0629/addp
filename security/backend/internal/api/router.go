package api

import (
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/modulelifecycle"
	_ "github.com/addp/security/docs"
	securityauthorization "github.com/addp/security/internal/authorization"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
	swaggerfiles "github.com/swaggo/files"
	ginswagger "github.com/swaggo/gin-swagger"
)

func getTenantID(c *gin.Context) int64 { return int64(commonauth.GetTenantID(c)) }
func getUserID(c *gin.Context) int64   { return int64(commonauth.GetUserID(c)) }

func SetupRouter(svc *service.DefinitionService, enrollments *service.EnrollmentService, discoveries *service.DiscoveryService, assessments *service.AssessmentService, policies *service.PolicyService, exemptions *service.ExemptionService, systemURL string, lifecycle *modulelifecycle.Controller) *gin.Engine {
	router := gin.Default()
	router.GET("/swagger/*any", ginswagger.WrapHandler(swaggerfiles.Handler))
	lifecycle.RegisterHealthRoutes(router)
	router.Use(lifecycle.RequireReady(), commoni18n.I18nMiddleware())
	h := NewDefinitionHandler(svc)
	definitionProfileHandler := NewDefinitionProfileHandler(svc)
	detectorHandler := NewDetectorHandler(svc)
	enrollmentHandler := NewEnrollmentHandler(enrollments)
	findingHandler := NewFindingHandler(discoveries)
	assessmentHandler := NewAssessmentHandler(assessments)
	policyHandler := NewPolicyHandler(policies)
	exemptionHandler := NewExemptionHandler(exemptions)
	api := router.Group("/api/v1/security")
	api.Use(commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: systemURL}), commonauth.MustNewContextGuard("tenant"))
	permission := func(key string) gin.HandlerFunc { return commonauth.MustNewPermissionGuard(key) }

	api.GET("/definition-profiles",
		permission(securityauthorization.PermissionSecurityClassificationRead),
		permission(securityauthorization.PermissionSecurityGradeRead),
		definitionProfileHandler.List,
	)
	api.POST("/definition-profile-applications",
		permission(securityauthorization.PermissionSecurityClassificationCreate),
		permission(securityauthorization.PermissionSecurityGradeCreate),
		definitionProfileHandler.Apply,
	)

	classifications := api.Group("/classifications")
	classifications.GET("", permission(securityauthorization.PermissionSecurityClassificationRead), h.ListClassifications)
	classifications.GET("/:id", permission(securityauthorization.PermissionSecurityClassificationRead), h.GetClassification)
	classifications.POST("", permission(securityauthorization.PermissionSecurityClassificationCreate), h.CreateClassification)
	classifications.PUT("/:id", permission(securityauthorization.PermissionSecurityClassificationUpdate), h.UpdateClassification)
	classifications.DELETE("/:id", permission(securityauthorization.PermissionSecurityClassificationDelete), h.DeleteClassification)

	grades := api.Group("/grades")
	grades.GET("", permission(securityauthorization.PermissionSecurityGradeRead), h.ListGrades)
	grades.GET("/:id", permission(securityauthorization.PermissionSecurityGradeRead), h.GetGrade)
	grades.POST("", permission(securityauthorization.PermissionSecurityGradeCreate), h.CreateGrade)
	grades.PUT("/:id", permission(securityauthorization.PermissionSecurityGradeUpdate), h.UpdateGrade)
	grades.DELETE("/:id", permission(securityauthorization.PermissionSecurityGradeDelete), h.DeleteGrade)

	types := api.Group("/sensitive-data-types")
	types.GET("", permission(securityauthorization.PermissionSecuritySensitiveDataTypeRead), h.ListTypes)
	types.GET("/:id", permission(securityauthorization.PermissionSecuritySensitiveDataTypeRead), h.GetType)
	types.POST("", permission(securityauthorization.PermissionSecuritySensitiveDataTypeCreate), h.CreateType)
	types.PUT("/:id", permission(securityauthorization.PermissionSecuritySensitiveDataTypeUpdate), h.UpdateType)
	types.DELETE("/:id", permission(securityauthorization.PermissionSecuritySensitiveDataTypeDelete), h.DeleteType)

	api.GET("/detector-capabilities", permission(securityauthorization.PermissionSecurityDetectorRead), detectorHandler.ListCapabilities)
	detectors := api.Group("/detectors")
	detectors.GET("", permission(securityauthorization.PermissionSecurityDetectorRead), detectorHandler.List)
	detectors.GET("/:id", permission(securityauthorization.PermissionSecurityDetectorRead), detectorHandler.Get)
	detectors.POST("", permission(securityauthorization.PermissionSecurityDetectorCreate), detectorHandler.Create)
	detectors.PUT("/:id", permission(securityauthorization.PermissionSecurityDetectorUpdate), detectorHandler.Update)
	detectors.DELETE("/:id", permission(securityauthorization.PermissionSecurityDetectorDelete), detectorHandler.Delete)

	baselines := api.Group("/protection-baselines")
	baselines.GET("", permission(securityauthorization.PermissionSecurityProtectionBaselineRead), h.ListBaselines)
	baselines.GET("/:id", permission(securityauthorization.PermissionSecurityProtectionBaselineRead), h.GetBaseline)
	baselines.POST("", permission(securityauthorization.PermissionSecurityProtectionBaselineCreate), h.CreateBaseline)
	baselines.PUT("/:id", permission(securityauthorization.PermissionSecurityProtectionBaselineUpdate), h.UpdateBaseline)
	baselines.DELETE("/:id", permission(securityauthorization.PermissionSecurityProtectionBaselineDelete), h.DeleteBaseline)

	protectionEnrollments := api.Group("/protection-enrollments")
	protectionEnrollments.GET("", permission(securityauthorization.PermissionSecurityEnrollmentRead), enrollmentHandler.List)
	protectionEnrollments.POST("", permission(securityauthorization.PermissionSecurityEnrollmentCreate), enrollmentHandler.Create)
	protectionEnrollments.GET("/:id", permission(securityauthorization.PermissionSecurityEnrollmentRead), enrollmentHandler.Get)
	protectionEnrollments.POST("/:id/re-enrollments", permission(securityauthorization.PermissionSecurityEnrollmentCreate), enrollmentHandler.ReEnroll)
	protectionEnrollments.GET("/:id/components", permission(securityauthorization.PermissionSecurityEnrollmentRead), permission(securityauthorization.PermissionSecurityAssessmentRead), assessmentHandler.ListComponents)
	protectionEnrollments.POST("/:id/releases", permission(securityauthorization.PermissionSecurityEnrollmentUpdate), enrollmentHandler.Release)
	protectionEnrollments.POST("/:id/discovery-executions", permission(securityauthorization.PermissionSecurityEnrollmentUpdate), enrollmentHandler.CreateDiscoveryExecution)

	findings := api.Group("/findings")
	findings.GET("", permission(securityauthorization.PermissionSecurityFindingRead), findingHandler.List)
	findings.GET("/:id", permission(securityauthorization.PermissionSecurityFindingRead), findingHandler.Get)
	findings.POST("/:id/reviews", permission(securityauthorization.PermissionSecurityFindingUpdate), assessmentHandler.ReviewFinding)
	api.GET("/discovery-quality", permission(securityauthorization.PermissionSecurityFindingRead), findingHandler.Quality)

	assessmentsAPI := api.Group("/assessments")
	assessmentsAPI.GET("", permission(securityauthorization.PermissionSecurityAssessmentRead), assessmentHandler.List)
	assessmentsAPI.POST("", permission(securityauthorization.PermissionSecurityAssessmentCreate), assessmentHandler.CreateManual)
	assessmentsAPI.GET("/:id", permission(securityauthorization.PermissionSecurityAssessmentRead), assessmentHandler.Get)
	assessmentsAPI.POST("/:id/revisions", permission(securityauthorization.PermissionSecurityAssessmentUpdate), assessmentHandler.Revise)
	assessmentsAPI.DELETE("/:id", permission(securityauthorization.PermissionSecurityAssessmentUpdate), assessmentHandler.Revoke)

	policiesAPI := api.Group("/protection-policies")
	policiesAPI.GET("", permission(securityauthorization.PermissionSecurityPolicyRead), policyHandler.List)
	policiesAPI.POST("", permission(securityauthorization.PermissionSecurityPolicyCreate), policyHandler.Create)
	policiesAPI.GET("/:id", permission(securityauthorization.PermissionSecurityPolicyRead), policyHandler.Get)
	policiesAPI.PUT("/:id", permission(securityauthorization.PermissionSecurityPolicyUpdate), policyHandler.Update)
	policiesAPI.DELETE("/:id", permission(securityauthorization.PermissionSecurityPolicyDelete), policyHandler.Revoke)

	exemptionsAPI := api.Group("/protection-exemptions")
	exemptionsAPI.GET("", permission(securityauthorization.PermissionSecurityProtectionExemptionRead), exemptionHandler.List)
	exemptionsAPI.POST("", permission(securityauthorization.PermissionSecurityProtectionExemptionCreate), exemptionHandler.Create)
	exemptionsAPI.GET("/:id", permission(securityauthorization.PermissionSecurityProtectionExemptionRead), exemptionHandler.Get)
	exemptionsAPI.PUT("/:id", permission(securityauthorization.PermissionSecurityProtectionExemptionUpdate), exemptionHandler.Renew)
	exemptionsAPI.DELETE("/:id", permission(securityauthorization.PermissionSecurityProtectionExemptionDelete), exemptionHandler.Revoke)

	runtime := api.Group("/runtime")
	runtime.Use(commonauth.MustNewServiceClientGuard("addp-manager", "addp-transfer", "addp-develop", "addp-service"))
	runtime.GET("/protection-projections/changes", permission(securityauthorization.PermissionSecurityProtectionProjectionRead), enrollmentHandler.ListChanges)
	runtime.POST("/protection-projection-acknowledgements", permission(securityauthorization.PermissionSecurityProtectionProjectionUpdate), enrollmentHandler.Acknowledge)
	return router
}
