package format

import (
	"bytes"
	"unicode/utf8"
)

// LooksLikeTextContent reports whether a content prefix is likely UTF-8 text.
//
// It is intentionally lightweight: callers still decide whether text should be
// interpreted as document/text, CSV, JSON, XML, or a format-specific dialect.
func LooksLikeTextContent(peek []byte) bool {
	peek = bytes.TrimPrefix(peek, []byte{0xEF, 0xBB, 0xBF})
	if len(peek) == 0 {
		return false
	}
	if bytes.IndexByte(peek, 0) >= 0 {
		return false
	}
	if !utf8.Valid(peek) {
		return false
	}

	controlCount := 0
	runeCount := 0
	for len(peek) > 0 {
		r, size := utf8.DecodeRune(peek)
		if r == utf8.RuneError && size == 1 {
			return false
		}
		runeCount++
		if r < 0x20 {
			switch r {
			case '\t', '\n', '\r', '\f':
			default:
				controlCount++
			}
		}
		peek = peek[size:]
	}
	return runeCount > 0 && controlCount*100 <= runeCount
}
