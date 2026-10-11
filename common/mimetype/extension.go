// Package mimetype provides MIME inference without inventing content encoding facts.
package mimetype

import (
	"mime"
	"strings"
)

// ByExtension returns the base MIME type registered for an extension (including its dot).
// The registry's parameters are defaults, not facts about the file's bytes.
// An unknown extension returns an empty string, as in mime.TypeByExtension.
func ByExtension(extension string) string {
	value, _, _ := strings.Cut(mime.TypeByExtension(extension), ";")
	return strings.TrimSpace(value)
}
