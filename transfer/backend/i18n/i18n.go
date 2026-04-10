package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
