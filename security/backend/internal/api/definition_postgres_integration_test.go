package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/dataprotection"
	authmiddleware "github.com/addp/common/middleware/auth"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// T2 exercises real handlers and PostgreSQL; the trusted identity fixture does
// not replace T4 verification of System authentication or Gateway routing.
func TestDefaultProtectionHTTPAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SECURITY_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("SECURITY_POSTGRES_TEST_DSN is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") ||
		(parsed.Path != "/addp_test" && !strings.Contains(parsed.Path, "disposable")) {
		t.Fatal("default protection HTTP test requires the standard disposable PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() {
		if err := tx.Rollback().Error; err != nil {
			t.Errorf("rollback test schema and fixtures: %v", err)
		}
	}()
	if err := tx.Exec("DROP SCHEMA IF EXISTS security CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(tx); err != nil {
		t.Fatal(err)
	}
	definitions := service.NewDefinitionService(tx)
	handler := NewDefinitionHandler(definitions)
	classification, err := definitions.CreateClassification(models.DefinitionRequest{Code: "personal", Name: "个人信息"}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	initialGrade, err := definitions.CreateGrade(models.DefinitionRequest{Code: "l3", Name: "三级", RiskOrder: 3}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	extraGrade, err := definitions.CreateGrade(models.DefinitionRequest{Code: "l4", Name: "四级", RiskOrder: 4}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	call := func(method string, id int64, body any, tenant string, handle gin.HandlerFunc, status int) []byte {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(method, "/", bytes.NewReader(encoded))
		context.Request.Header.Set("Content-Type", "application/json")
		context.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(id, 10)}}
		actor := authtest.NewTenantUserAuthContext(tenant, "11", []string{
			"security.sensitive_data_type.create", "security.sensitive_data_type.read",
			"security.protection_baseline.create", "security.protection_baseline.read",
			"security.protection_baseline.update", "security.protection_baseline.delete",
		})
		if err := authmiddleware.SetAuthContextForGin(context, actor); err != nil {
			t.Fatal(err)
		}
		handle(context)
		if recorder.Code != status {
			t.Fatalf("%s id=%d status=%d, want %d: %s", method, id, recorder.Code, status, recorder.Body.String())
		}
		return recorder.Body.Bytes()
	}
	list := func() []models.ProtectionBaseline {
		return decodeDefinitionResponse[[]models.ProtectionBaseline](t, call(http.MethodGet, 0, nil, "7", handler.ListBaselines, http.StatusOK))
	}
	get := func(id int64) models.ProtectionBaseline {
		return decodeDefinitionResponse[models.ProtectionBaseline](t, call(http.MethodGet, id, nil, "7", handler.GetBaseline, http.StatusOK))
	}
	command := models.CreateSensitiveDataTypeRequest{
		Code: "email", Name: "电子邮箱", SecurityClassificationID: classification.ID,
		DefaultSecurityGradeID: initialGrade.ID,
		DefaultProtection: &models.DefaultProtectionRequest{Effect: dataprotection.EffectMask,
			Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, KeepPrefix: 2, KeepSuffix: 3},
	}
	invalid := command
	invalid.Code = "invalid_email"
	invalid.DefaultProtection = &models.DefaultProtectionRequest{Effect: dataprotection.EffectMask, Algorithm: "invalid"}
	call(http.MethodPost, 0, invalid, "7", handler.CreateType, http.StatusBadRequest)
	types := decodeDefinitionResponse[[]models.SensitiveDataType](t, call(http.MethodGet, 0, nil, "7", handler.ListTypes, http.StatusOK))
	if len(types) != 0 || len(list()) != 0 {
		t.Fatal("invalid default protection left a partial definition")
	}
	createdType := decodeDefinitionResponse[models.SensitiveDataType](t, call(http.MethodPost, 0, command, "7", handler.CreateType, http.StatusCreated))
	initialRules := list()
	if createdType.ID <= 0 || createdType.Version != 1 || len(initialRules) != 1 {
		t.Fatalf("atomic definition response=%#v, baselines=%#v", createdType, initialRules)
	}
	initial := initialRules[0]
	if initial.SensitiveDataTypeID != createdType.ID || initial.SecurityGradeID != initialGrade.ID || !initial.Enabled || initial.Version != 1 ||
		initial.Effect != dataprotection.EffectMask || initial.Algorithm != dataprotection.AlgorithmKeepPrefixSuffixV2 ||
		initial.KeepPrefix != 2 || initial.KeepSuffix != 3 || initial.InvalidValueEffect != dataprotection.EffectSuppress {
		t.Fatalf("initial baseline=%#v", initial)
	}
	call(http.MethodDelete, initial.ID, models.DeleteProtectionBaselineRequest{Version: initial.Version}, "7", handler.DeleteBaseline, http.StatusConflict)
	if !reflect.DeepEqual(get(initial.ID), initial) {
		t.Fatal("refused deletion modified the initial default protection")
	}
	request := models.ProtectionBaselineRequest{SensitiveDataTypeID: createdType.ID, SecurityGradeID: extraGrade.ID,
		Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, KeepPrefix: 2, KeepSuffix: 4}
	created := decodeDefinitionResponse[models.ProtectionBaseline](t, call(http.MethodPost, 0, request, "7", handler.CreateBaseline, http.StatusCreated))
	if created.ID <= 0 || created.Version != 1 || created.TenantID != 7 || created.CreatedBy != 11 ||
		created.SensitiveDataTypeID != createdType.ID || created.SecurityGradeID != extraGrade.ID ||
		created.Effect != request.Effect || created.Algorithm != request.Algorithm || created.KeepPrefix != 2 || created.KeepSuffix != 4 ||
		created.InvalidValueEffect != dataprotection.EffectSuppress || !created.Enabled || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("create must return the complete canonical rule, including defaulted fields: %#v", created)
	}
	request.Version, request.KeepSuffix = created.Version, 6
	updated := decodeDefinitionResponse[models.ProtectionBaseline](t, call(http.MethodPut, created.ID, request, "7", handler.UpdateBaseline, http.StatusOK))
	if updated.ID != created.ID || updated.Version != 2 || updated.KeepSuffix != 6 || updated.UpdatedBy == nil || *updated.UpdatedBy != 11 {
		t.Fatalf("update must return the new version and saved input: %#v", updated)
	}
	if !reflect.DeepEqual(get(created.ID), updated) {
		t.Fatal("write response differs from the persisted rule")
	}
	for _, operation := range []struct {
		method string
		body   any
		handle gin.HandlerFunc
	}{
		{http.MethodPut, request, handler.UpdateBaseline},
		{http.MethodDelete, models.DeleteProtectionBaselineRequest{Version: created.Version}, handler.DeleteBaseline},
	} {
		response := decodeDefinitionResponse[map[string]string](t, call(operation.method, created.ID, operation.body, "7", operation.handle, http.StatusConflict))
		if response["error_code"] != "resource_version_conflict" || strings.TrimSpace(response["error"]) == "" {
			t.Fatalf("%s conflict response=%#v", operation.method, response)
		}
		call(operation.method, created.ID, operation.body, "8", operation.handle, http.StatusNotFound)
		if !reflect.DeepEqual(get(created.ID), updated) {
			t.Fatalf("%s conflict or cross-tenant request changed the persisted rule", operation.method)
		}
	}
	call(http.MethodGet, created.ID, nil, "8", handler.GetBaseline, http.StatusNotFound)
	otherTenantRules := decodeDefinitionResponse[[]models.ProtectionBaseline](t, call(http.MethodGet, 0, nil, "8", handler.ListBaselines, http.StatusOK))
	if len(otherTenantRules) != 0 {
		t.Fatal("baseline list leaked another tenant's rules")
	}
	// Explicit reload supplies the only version used for the next write.
	latest := get(created.ID)
	request.Version, request.KeepSuffix = latest.Version, 7
	retried := decodeDefinitionResponse[models.ProtectionBaseline](t, call(http.MethodPut, created.ID, request, "7", handler.UpdateBaseline, http.StatusOK))
	if retried.Version != 3 || retried.KeepSuffix != 7 || !reflect.DeepEqual(get(created.ID), retried) {
		t.Fatalf("save after explicit reload=%#v", retried)
	}
	call(http.MethodDelete, created.ID, map[string]any{}, "7", handler.DeleteBaseline, http.StatusBadRequest)
	deletedResponse := decodeDefinitionResponse[map[string]string](t, call(http.MethodDelete, created.ID, models.DeleteProtectionBaselineRequest{Version: retried.Version}, "7", handler.DeleteBaseline, http.StatusOK))
	if strings.TrimSpace(deletedResponse["message"]) == "" {
		t.Fatal("delete response must contain a readable message")
	}
	call(http.MethodGet, created.ID, nil, "7", handler.GetBaseline, http.StatusNotFound)
	if remaining := list(); len(remaining) != 1 || !reflect.DeepEqual(remaining[0], initial) {
		t.Fatalf("delete affected unrelated default protection: %#v", remaining)
	}
}

func decodeDefinitionResponse[T any](t *testing.T, body []byte) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode HTTP response: %v; body=%s", err, body)
	}
	return result
}
