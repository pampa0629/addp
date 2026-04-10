package i18n

import (
	"embed"
	"sync"

	"github.com/gin-gonic/gin"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/text/language"
)

//go:embed locales/*.toml
var commonLocaleFS embed.FS

// 通用消息 key 常量（common 模块使用）
const (
	MsgInvalidID    = "err.invalid_id"
	MsgUnauthorized = "err.unauthorized"
	MsgInvalidParams = "err.invalid_params"
)

// Bundle 是全局翻译 Bundle，各模块可通过 RegisterBundle 注册自己的翻译文件。
var (
	globalBundle *goi18n.Bundle
	bundleOnce   sync.Once
)

func getBundle() *goi18n.Bundle {
	bundleOnce.Do(func() {
		globalBundle = goi18n.NewBundle(language.Chinese)
		globalBundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)
		loadFS(globalBundle, commonLocaleFS, "locales")
	})
	return globalBundle
}

// RegisterBundle 供各模块在启动时注册自己的翻译文件。
// fs 是模块的 embed.FS，dir 是 TOML 文件所在目录（如 "locales"）。
func RegisterBundle(fs embed.FS, dir string) {
	b := getBundle()
	loadFS(b, fs, dir)
}

func loadFS(b *goi18n.Bundle, fs embed.FS, dir string) {
	for _, lang := range []string{LangZhCN, LangEn} {
		data, err := fs.ReadFile(dir + "/" + lang + ".toml")
		if err != nil {
			continue
		}
		b.ParseMessageFileBytes(data, lang+".toml")
	}
}

// T 根据 gin context 中的语言偏好翻译消息 ID。
// 若 key 不存在则 fallback 到 zh-cn，再不存在则返回 key 本身。
func T(c *gin.Context, messageID string) string {
	lang := GetLang(c)
	localizer := goi18n.NewLocalizer(getBundle(), lang)
	msg, err := localizer.Localize(&goi18n.LocalizeConfig{MessageID: messageID})
	if err != nil {
		localizer = goi18n.NewLocalizer(getBundle(), LangZhCN)
		msg, err = localizer.Localize(&goi18n.LocalizeConfig{MessageID: messageID})
		if err != nil {
			return messageID
		}
	}
	return msg
}

// TWithDetail 翻译消息 ID 并追加动态详情（如错误原因）。
func TWithDetail(c *gin.Context, messageID, detail string) string {
	return T(c, messageID) + ": " + detail
}
