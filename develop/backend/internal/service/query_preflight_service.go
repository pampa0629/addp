package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resourcetree"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
)

// AnalyzePreparedQuery returns only Provider-owned or owner-compiler-owned
// diagnostics. It intentionally uses the masked runtime descriptor and never
// fetches execution credentials or reads business data.
func (s *SQLEngineService) AnalyzePreparedQuery(
	ctx context.Context,
	tenantID, engineID uint,
	language, query, targetLocator string,
	parameters map[string]interface{},
	queryParameters []models.QueryParameterDefinition,
	readOnly bool,
) (*plugin.QueryAnalysis, error) {
	if s == nil || s.systemService == nil || tenantID == 0 || engineID == 0 {
		return nil, fmt.Errorf("查询分析服务未正确初始化")
	}
	descriptor, err := s.systemService.WithTenantID(tenantID).GetEngineRuntimeDescriptor(ctx, engineID)
	if err != nil {
		return nil, fmt.Errorf("获取查询引擎描述失败: %w", err)
	}
	language = strings.ToLower(strings.TrimSpace(language))
	bindings, hasRelationParameters, bindingErr := relationParameterBindings(queryParameters)
	if bindingErr != nil {
		return invalidPreparedQueryAnalysis(language, bindingErr)
	}
	if hasRelationParameters {
		structuralAnalysis, structuralErr := analyzeRelationParameterQuery(descriptor.EngineType, language, query, queryParameters)
		if structuralErr != nil || queryAnalysisHasErrors(structuralAnalysis) {
			return structuralAnalysis, structuralErr
		}
		complete := true
		for _, binding := range bindings {
			if _, overridden := parameters[binding.Name]; overridden || binding.DefaultLocator != "" {
				continue
			}
			complete = false
			break
		}
		if !complete {
			return structuralAnalysis, nil
		}
		content := map[string]interface{}{
			"query": query, "query_type": language, "query_parameters": queryParameters,
		}
		_, runtimeParameters, effectiveInputs, resolveErr := resolveQueryPreviewParameters(content, parameters)
		if resolveErr != nil {
			return invalidPreparedQueryAnalysis(language, resolveErr)
		}
		compiled, compileErr := compileRelationPreviewQuery(&models.DevTask{
			DevType:         commonExecution.TaskTypeQuery,
			Content:         models.DevTaskContent(content),
			ExecutionConfig: models.DevTaskContent{"engine_id": engineID},
		}, effectiveInputs, descriptor.EngineType)
		if compileErr != nil {
			return invalidPreparedQueryAnalysis(language, compileErr)
		}
		query, _ = compiled.Content["query"].(string)
		parameters = runtimeParameters
	}

	registered, err := plugin.Get(descriptor.EngineType)
	if err != nil {
		return nil, err
	}
	provider, ordinary := registered.(plugin.QueryRuntimeProvider)
	if !ordinary {
		if federated, ok := registered.(plugin.FederatedQueryRuntimeProvider); ok && containsQueryLanguage(federated.QueryLanguages(), language) {
			analysis, analyzeErr := federated.AnalyzeFederatedQuery(ctx, plugin.FederatedQueryRequest{
				Query: query, Language: language,
				Options: plugin.QueryOptions{EngineID: engineID, EngineType: descriptor.EngineType, ReadOnly: readOnly, Parameters: parameters},
			})
			if analyzeErr != nil {
				return invalidPreparedQueryAnalysis(language, analyzeErr)
			}
			return analysis, nil
		}
		return nil, fmt.Errorf("引擎 %s 不支持普通查询分析", descriptor.EngineType)
	}
	if !containsQueryLanguage(provider.QueryLanguages(), language) {
		return invalidPreparedQueryAnalysis(language, fmt.Errorf("引擎不支持查询语言: %s", language))
	}

	var targetPath *plugin.EngineCatalogPath
	if strings.TrimSpace(targetLocator) != "" {
		locator, parseErr := resourcetree.ParseURI(strings.TrimSpace(targetLocator))
		if parseErr != nil || locator.EngineID != engineID {
			if parseErr == nil {
				parseErr = fmt.Errorf("资源定位符引擎 ID 不匹配")
			}
			return invalidPreparedQueryAnalysis(language, parseErr)
		}
		model, modelErr := dbbridge.EngineCatalogModel(descriptor.EngineType)
		if modelErr != nil {
			return nil, modelErr
		}
		path, pathErr := resourcetree.EngineCatalogPathFromLocator(model, locator)
		if pathErr != nil {
			return invalidPreparedQueryAnalysis(language, pathErr)
		}
		targetPath = &path
	}
	prepared, prepareErr := provider.PrepareQuery(ctx, nil, plugin.QueryRequest{
		EngineID: engineID, Language: language, Query: query, TargetPath: targetPath,
		Options: plugin.QueryOptions{
			EngineID: engineID, EngineType: descriptor.EngineType, ReadOnly: readOnly,
			Parameters: parameters,
		},
	})
	if prepareErr != nil {
		return invalidPreparedQueryAnalysis(language, prepareErr)
	}
	analysis, analysisErr := prepared.Analysis(ctx)
	if analysisErr != nil {
		return nil, analysisErr
	}
	return analysis, nil
}

func analyzeRelationParameterQuery(engineType, language, query string, queryParameters []models.QueryParameterDefinition) (*plugin.QueryAnalysis, error) {
	if language != "sql" || !commonquery.ForDialect(engineType).IsPostgreSQL() {
		return invalidPreparedQueryAnalysis(language, fmt.Errorf("relation 查询参数仅支持 PostgreSQL SQL"))
	}
	bindings, _, err := relationParameterBindings(queryParameters)
	if err != nil {
		return invalidPreparedQueryAnalysis(language, err)
	}
	if err := validateRelationResultSource(query, bindings); err != nil {
		return invalidPreparedQueryAnalysis(language, err)
	}
	return plugin.NewQueryAnalysis(language, plugin.QuerySchemaCoverageUnknown)
}

func queryAnalysisHasErrors(analysis *plugin.QueryAnalysis) bool {
	if analysis == nil {
		return false
	}
	for _, diagnostic := range analysis.Diagnostics {
		if diagnostic.Severity == plugin.QueryDiagnosticSeverityError {
			return true
		}
	}
	return false
}

func invalidPreparedQueryAnalysis(language string, cause error) (*plugin.QueryAnalysis, error) {
	if strings.TrimSpace(language) == "" {
		language = "sql"
	}
	detail := ""
	if cause != nil {
		detail = cause.Error()
	}
	return plugin.NewQueryAnalysis(language, plugin.QuerySchemaCoverageUnknown, plugin.QueryDiagnostic{
		Code: "query_invalid", Severity: plugin.QueryDiagnosticSeverityError, Phase: plugin.QueryDiagnosticPhaseSyntax,
		Parameters: map[string]string{"detail": detail},
	})
}

func containsQueryLanguage(languages []string, target string) bool {
	for _, language := range languages {
		if strings.EqualFold(strings.TrimSpace(language), target) {
			return true
		}
	}
	return false
}

type QueryPreflightResult struct {
	Effect                   string
	Statement                string
	ClassificationConfidence string
	TargetObjects            []string
	Warnings                 []string
	Fingerprint              string
	RiskLevel                string
	RequiresConfirmation     bool
	ConfirmationToken        string
	ConfirmationExpiresAt    time.Time
}

type QueryConfirmationError struct {
	Message string
}

func (e *QueryConfirmationError) Error() string {
	if e == nil || e.Message == "" {
		return "查询确认凭证无效"
	}
	return e.Message
}

// AnalyzeQuery performs the same conservative analysis used by SQL execution
// authorization. MQL and Cypher are currently provider-enforced read-only
// languages and therefore do not enter the SQL classifier.
func AnalyzeQuery(queryType, query string) (*QueryPreflightResult, error) {
	language := strings.ToLower(strings.TrimSpace(queryType))
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("查询语句不能为空")
	}
	if language == "" {
		language = "sql"
	}
	if language != "sql" && language != "mql" && language != "cypher" {
		return nil, fmt.Errorf("不支持的查询语言: %s", language)
	}
	if language != "sql" {
		digest := sha256.Sum256([]byte(strings.TrimSpace(query)))
		return &QueryPreflightResult{
			Effect:                   string(SQLExecutionEffectRead),
			Statement:                strings.ToUpper(language),
			ClassificationConfidence: "provider_read_only",
			Fingerprint:              fmt.Sprintf("%x", digest[:]),
			RiskLevel:                "low",
		}, nil
	}

	analysis, err := commonquery.Analyze(query)
	if err != nil {
		return nil, err
	}
	risk := "low"
	if analysis.Effect == commonquery.Write {
		risk = "medium"
	}
	if analysis.Effect == commonquery.DDL || analysis.Effect == commonquery.ExternalEffect {
		risk = "high"
	}
	return &QueryPreflightResult{
		Effect:                   string(analysis.Effect),
		Statement:                analysis.Statement,
		ClassificationConfidence: analysis.ClassificationConfidence,
		TargetObjects:            append([]string(nil), analysis.TargetObjects...),
		Warnings:                 append([]string(nil), analysis.Warnings...),
		Fingerprint:              analysis.Fingerprint,
		RiskLevel:                risk,
		RequiresConfirmation:     analysis.RequiresConfirmation,
	}, nil
}

// IssueQueryConfirmationToken returns a short-lived, request-bound token. It
// is intentionally stateless; the signed payload is revalidated at execution.
func IssueQueryConfirmationToken(cfg *config.Config, tenantID, userID, engineID uint, targetLocator, fingerprint, effect string, now time.Time) (string, time.Time, error) {
	if cfg == nil || len(cfg.EncryptionKey) == 0 || tenantID == 0 || userID == 0 || engineID == 0 || fingerprint == "" || effect == "" {
		return "", time.Time{}, fmt.Errorf("查询确认凭证参数无效")
	}
	expiresAt := now.UTC().Add(5 * time.Minute)
	locatorDigest := sha256.Sum256([]byte(strings.TrimSpace(targetLocator)))
	payload := strings.Join([]string{
		"v1", strconv.FormatUint(uint64(tenantID), 10), strconv.FormatUint(uint64(userID), 10),
		strconv.FormatUint(uint64(engineID), 10), effect, fingerprint,
		fmt.Sprintf("%x", locatorDigest[:]), strconv.FormatInt(expiresAt.Unix(), 10),
	}, "|")
	signature := signQueryConfirmation(cfg.EncryptionKey, payload)
	token := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(signature)
	return token, expiresAt, nil
}

func VerifyQueryConfirmationToken(cfg *config.Config, token string, tenantID, userID, engineID uint, targetLocator, fingerprint, effect string, now time.Time) error {
	if cfg == nil || len(cfg.EncryptionKey) == 0 || strings.TrimSpace(token) == "" {
		return fmt.Errorf("查询确认凭证缺失")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return fmt.Errorf("查询确认凭证格式无效")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("查询确认凭证格式无效")
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("查询确认凭证格式无效")
	}
	payload := string(payloadBytes)
	expectedSignature := signQueryConfirmation(cfg.EncryptionKey, payload)
	if !hmac.Equal(providedSignature, expectedSignature) {
		return fmt.Errorf("查询确认凭证签名无效")
	}
	fields := strings.Split(payload, "|")
	if len(fields) != 8 || fields[0] != "v1" || fields[1] != strconv.FormatUint(uint64(tenantID), 10) ||
		fields[2] != strconv.FormatUint(uint64(userID), 10) || fields[3] != strconv.FormatUint(uint64(engineID), 10) ||
		fields[4] != effect || fields[5] != fingerprint {
		return fmt.Errorf("查询确认凭证与当前请求不匹配")
	}
	locatorDigest := sha256.Sum256([]byte(strings.TrimSpace(targetLocator)))
	if fields[6] != fmt.Sprintf("%x", locatorDigest[:]) {
		return fmt.Errorf("查询确认凭证与当前资源不匹配")
	}
	expiresUnix, err := strconv.ParseInt(fields[7], 10, 64)
	if err != nil || !now.UTC().Before(time.Unix(expiresUnix, 0)) {
		return fmt.Errorf("查询确认凭证已过期")
	}
	return nil
}

func (s *SQLEngineService) IssueQueryConfirmationToken(tenantID, userID, engineID uint, targetLocator, fingerprint, effect string) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, fmt.Errorf("查询确认服务未正确初始化")
	}
	return IssueQueryConfirmationToken(s.cfg, tenantID, userID, engineID, targetLocator, fingerprint, effect, time.Now())
}

func (s *SQLEngineService) VerifyQueryConfirmationToken(token string, tenantID, userID, engineID uint, targetLocator, fingerprint, effect string) error {
	if s == nil {
		return fmt.Errorf("查询确认服务未正确初始化")
	}
	return VerifyQueryConfirmationToken(s.cfg, token, tenantID, userID, engineID, targetLocator, fingerprint, effect, time.Now())
}

func signQueryConfirmation(key []byte, payload string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("develop.query.confirmation|" + payload))
	return mac.Sum(nil)
}
