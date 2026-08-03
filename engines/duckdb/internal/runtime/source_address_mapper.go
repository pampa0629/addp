package runtime

import (
	"net"
	"net/url"
	"strings"

	commonmodels "github.com/addp/common/models"
)

func mapSourceEngineLoopbackAddress(engine commonmodels.Engine, replacementHost string) commonmodels.Engine {
	replacementHost = strings.TrimSpace(replacementHost)
	if replacementHost == "" {
		return engine
	}

	connectionInfo := make(commonmodels.ConnectionInfo, len(engine.ConnectionInfo))
	for key, value := range engine.ConnectionInfo {
		connectionInfo[key] = value
	}
	engine.ConnectionInfo = connectionInfo

	switch strings.ToLower(strings.TrimSpace(engine.EngineType)) {
	case "postgresql", "mysql":
		if host, ok := connectionInfo["host"].(string); ok && isLoopbackHost(host) {
			connectionInfo["host"] = replacementHost
		}
	case "minio", "s3":
		if endpoint, ok := connectionInfo["endpoint"].(string); ok {
			connectionInfo["endpoint"] = mapLoopbackEndpoint(endpoint, replacementHost)
		}
	}
	return engine
}

func mapLoopbackEndpoint(endpoint, replacementHost string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return endpoint
	}
	if strings.Contains(endpoint, "://") {
		parsed, err := url.Parse(endpoint)
		if err != nil || !isLoopbackHost(parsed.Hostname()) {
			return endpoint
		}
		port := parsed.Port()
		parsed.Host = replacementHost
		if port != "" {
			parsed.Host = net.JoinHostPort(replacementHost, port)
		}
		return parsed.String()
	}

	host, port, err := net.SplitHostPort(endpoint)
	if err == nil {
		if isLoopbackHost(host) {
			return net.JoinHostPort(replacementHost, port)
		}
		return endpoint
	}
	if isLoopbackHost(endpoint) {
		return replacementHost
	}
	return endpoint
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "localhost.") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
