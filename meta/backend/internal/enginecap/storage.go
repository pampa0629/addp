package enginecap

import (
	"strings"

	"github.com/addp/common/engine/plugin"
)

func StorageFamily(p plugin.EnginePlugin) string {
	if p == nil {
		return ""
	}
	caps := p.Capabilities()
	if caps.Storage == nil {
		return ""
	}
	for _, family := range caps.Storage.Families {
		normalized := strings.ToLower(family)
		switch normalized {
		case "object", "file", "tabular", "document", "graph":
			return normalized
		}
	}
	return ""
}
