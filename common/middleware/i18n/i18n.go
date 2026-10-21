package i18n

import (
	"context"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// LangZhCN 简体中文
	LangZhCN = "zh-cn"
	// LangEn 英文
	LangEn = "en"
	// DefaultLang 默认语言
	DefaultLang = LangZhCN
)

type languageKey struct{}

// IsSupportedLanguage reports whether lang is a canonical execution language.
func IsSupportedLanguage(lang string) bool {
	return lang == LangZhCN || lang == LangEn
}

// WithLanguage binds an already normalized language without accepting header syntax.
func WithLanguage(ctx context.Context, lang string) (context.Context, error) {
	if ctx == nil || !IsSupportedLanguage(lang) {
		return nil, fmt.Errorf("canonical display language is required")
	}
	return context.WithValue(ctx, languageKey{}, lang), nil
}

// Language reads the standard request context; a request without a preference uses Chinese.
func Language(ctx context.Context) string {
	if ctx != nil {
		if lang, ok := ctx.Value(languageKey{}).(string); ok {
			return lang
		}
	}
	return DefaultLang
}

// I18nMiddleware 从请求的 Accept-Language 头提取语言偏好，注入标准 context。
//
// 用法（在各模块 router 中注册）：
//
//	r.Use(i18n.I18nMiddleware())
//
// 在 handler 中获取语言：
//
//	lang := i18n.GetLang(c)
func I18nMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		lang := extractLang(c.GetHeader("Accept-Language"))
		ctx, _ := WithLanguage(c.Request.Context(), lang)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// GetLang reads the same language fact as services using the request context.
func GetLang(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return DefaultLang
	}
	return Language(c.Request.Context())
}

// extractLang 解析 Accept-Language 请求头，返回 ADDP 支持的语言代码。
//
// Accept-Language 示例：
//   - "zh-CN,zh;q=0.9" → "zh-cn"
//   - "en-US,en;q=0.9" → "en"
//   - "fr-FR,fr;q=0.9" → "zh-cn"（不支持的语言降级为默认值）
func extractLang(header string) string {
	if header == "" {
		return DefaultLang
	}

	// 按 , 分割多个语言项（例如 "zh-CN,zh;q=0.9,en;q=0.8"）
	parts := strings.Split(header, ",")
	for _, part := range parts {
		// 去掉 quality factor（分号后的部分），只取语言标签
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if normalized := normalizeLang(tag); normalized != "" {
			return normalized
		}
	}

	return DefaultLang
}

// normalizeLang 将 BCP 47 语言标签转为 ADDP 支持的语言代码。
// 不支持的语言返回空字符串。
func normalizeLang(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	switch {
	case strings.HasPrefix(tag, "zh"):
		return LangZhCN
	case strings.HasPrefix(tag, "en"):
		return LangEn
	}
	return ""
}
