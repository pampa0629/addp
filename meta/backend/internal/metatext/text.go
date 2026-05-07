package metatext

const (
	DocumentPreviewRuneLimit = 400
	DocumentContentRuneLimit = 20000
)

// TruncateRunes 截断字符串到指定 rune 数量。
func TruncateRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// PreviewText 生成文本预览（截断到指定长度）。
func PreviewText(text string, limit int) string {
	return TruncateRunes(text, limit)
}
