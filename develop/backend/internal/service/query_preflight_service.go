package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonquery "github.com/addp/common/query"
	"github.com/addp/develop/backend/internal/config"
)

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
