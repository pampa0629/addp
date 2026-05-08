package enginecap

import "github.com/addp/common/engine/plugin"

func StorageFamily(p plugin.EnginePlugin) string {
	if p == nil {
		return ""
	}
	caps := p.Capabilities()
	if caps.Storage == nil {
		return ""
	}
	switch caps.EngineFamily {
	case "object", "file", "tabular", "document", "graph":
		return caps.EngineFamily
	}
	return ""
}
