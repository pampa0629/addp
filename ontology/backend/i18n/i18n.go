package i18n

import (
	"embed"
	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var locales embed.FS

const (
	Invalid           = "ontology.invalid"
	Conflict          = "ontology.conflict"
	NotFound          = "ontology.not_found"
	Failed            = "ontology.failed"
	AdmissionFailed   = "ontology.admission_failed"
	NotActive         = "ontology.not_active"
	ActivationChanged = "ontology.activation_changed"
	ResultTooLarge    = "ontology.result_too_large"
)

func init() { commoni18n.RegisterBundle(locales, "locales") }
