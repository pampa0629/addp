package runtimeconn

import (
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

type ExportOptions struct {
	IncludeRawConnectionInfo bool
}

func BuildNotebookConnection(engine *models.Engine, opts ExportOptions) (map[string]interface{}, error) {
	if engine == nil {
		return nil, fmt.Errorf("engine is required")
	}

	p, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}

	connInfo := plugin.ConnectionInfo(engine.ConnectionInfo)
	result := map[string]interface{}{
		"type":        p.Type(),
		"engine_type": p.Type(),
		"engine_id":   engine.ID,
		"name":        engine.Name,
	}

	if opts.IncludeRawConnectionInfo {
		result["connection_info"] = copyConnectionInfo(engine.ConnectionInfo)
	}

	caps := p.Capabilities()
	switch caps.EngineFamily {
	case "tabular":
		addDriverConnectionFields(result, connInfo, p)
	case "object":
		addObjectConnectionFields(result, connInfo)
	case "file":
		addFileConnectionFields(result, connInfo)
	case "dynamic_schema", "graph":
		addDSNConnectionFields(result, connInfo, p)
	default:
		if !addDSNConnectionFields(result, connInfo, p) {
			return nil, fmt.Errorf("engine %s does not expose a notebook runtime connection description", engine.EngineType)
		}
	}

	return result, nil
}

func addDriverConnectionFields(result map[string]interface{}, connInfo plugin.ConnectionInfo, p plugin.EnginePlugin) {
	parts := plugin.ParseDriverConnInfo(connInfo, p.DefaultPort(), "")
	result["host"] = parts.Host
	result["port"] = parts.Port
	result["database"] = parts.Database
	result["user"] = parts.User
	result["password"] = parts.Password
	addDSNConnectionFields(result, connInfo, p)
}

func addObjectConnectionFields(result map[string]interface{}, connInfo plugin.ConnectionInfo) {
	result["endpoint"] = plugin.GetString(connInfo, "endpoint")
	result["access_key"] = plugin.GetString(connInfo, "access_key")
	result["secret_key"] = plugin.GetString(connInfo, "secret_key")
	if bucket := plugin.GetString(connInfo, "bucket"); bucket != "" {
		result["bucket"] = bucket
	}
	result["secure"] = plugin.GetBool(connInfo, "secure") || strings.EqualFold(plugin.GetString(connInfo, "secure"), "true")
	if region := plugin.GetString(connInfo, "region"); region != "" {
		result["region"] = region
	}
}

func addFileConnectionFields(result map[string]interface{}, connInfo plugin.ConnectionInfo) {
	for _, key := range []string{"server", "export_path", "root_path", "mount_path", "base_path"} {
		if value := plugin.GetString(connInfo, key); value != "" {
			result[key] = value
		}
	}
}

func addDSNConnectionFields(result map[string]interface{}, connInfo plugin.ConnectionInfo, p plugin.EnginePlugin) bool {
	dsnProvider, ok := p.(plugin.DSNProvider)
	if !ok {
		return false
	}
	dsn, err := dsnProvider.BuildDSN(connInfo)
	if err != nil || dsn == "" {
		return false
	}
	result["connection_string"] = dsn
	return true
}

func copyConnectionInfo(connInfo models.ConnectionInfo) map[string]interface{} {
	copied := make(map[string]interface{}, len(connInfo))
	for key, value := range connInfo {
		copied[key] = value
	}
	return copied
}
