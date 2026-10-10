package config

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/addp/common/models"
)

const ProcessMetricsDeploymentsEnv = "ADDP_PROCESS_METRICS_DEPLOYMENTS"

var ErrProcessMetricsConfiguration = errors.New("process_metrics_configuration_invalid")

// ProcessMetricsDeployment is a process-start input, not an owner policy.
type ProcessMetricsDeployment struct {
	ModuleName string
	Role       string
	Listen     string
	Endpoint   string
	TLSDir     string
}

// ProcessMetricsDeploymentFor validates the complete deployment input before
// selecting exactly one current module/role. No endpoint or port is inferred.
func ProcessMetricsDeploymentFor(raw, module, role string) (*ProcessMetricsDeployment, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 256<<10 {
		return nil, ErrProcessMetricsConfiguration
	}
	d := json.NewDecoder(strings.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('[') {
		return nil, ErrProcessMetricsConfiguration
	}
	seen, endpoints, listeners := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var selected *ProcessMetricsDeployment
	count := 0
	for d.More() {
		count++
		if count > 1000 {
			return nil, ErrProcessMetricsConfiguration
		}
		token, err := d.Token()
		if err != nil || token != json.Delim('{') {
			return nil, ErrProcessMetricsConfiguration
		}
		fields := map[string]string{}
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok {
				return nil, ErrProcessMetricsConfiguration
			}
			switch name {
			case "module_name", "role", "listen", "endpoint", "tls_dir":
			default:
				return nil, ErrProcessMetricsConfiguration
			}
			if _, exists := fields[name]; exists {
				return nil, ErrProcessMetricsConfiguration
			}
			var value string
			if d.Decode(&value) != nil || value == "" || len(value) > 1024 {
				return nil, ErrProcessMetricsConfiguration
			}
			fields[name] = value
		}
		if token, err = d.Token(); err != nil || token != json.Delim('}') || len(fields) != 5 {
			return nil, ErrProcessMetricsConfiguration
		}
		entry := ProcessMetricsDeployment{ModuleName: fields["module_name"], Role: fields["role"], Listen: fields["listen"], Endpoint: fields["endpoint"], TLSDir: fields["tls_dir"]}
		key := entry.ModuleName + "/" + entry.Role
		if entry.Validate() != nil || seen[key] || endpoints[entry.Endpoint] || listeners[entry.Listen] {
			return nil, ErrProcessMetricsConfiguration
		}
		seen[key], endpoints[entry.Endpoint], listeners[entry.Listen] = true, true, true
		if entry.ModuleName == module && entry.Role == role {
			copy := entry
			selected = &copy
		}
	}
	if token, err := d.Token(); err != nil || token != json.Delim(']') {
		return nil, ErrProcessMetricsConfiguration
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrProcessMetricsConfiguration
	}
	return selected, nil
}

func (d ProcessMetricsDeployment) Validate() error {
	invalid := ErrProcessMetricsConfiguration
	if d.ModuleName == "" || len(d.ModuleName) > 50 {
		return invalid
	}
	for _, r := range d.ModuleName {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return invalid
		}
	}
	switch d.Role {
	case "backend", "worker", "scheduler", "ingress":
	default:
		return invalid
	}
	if (&models.ProcessMetricsDeclaration{SchemaVersion: models.ProcessMetricsSchema, Endpoint: d.Endpoint}).Validate() != nil {
		return invalid
	}
	host, port, err := net.SplitHostPort(d.Listen)
	ip, ipErr := netip.ParseAddr(host)
	p, portErr := strconv.ParseUint(port, 10, 16)
	if err != nil || ipErr != nil || ip.Is4In6() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.Zone() != "" ||
		portErr != nil || p == 0 || net.JoinHostPort(ip.String(), strconv.FormatUint(p, 10)) != d.Listen {
		return invalid
	}
	if !filepath.IsAbs(d.TLSDir) || filepath.Clean(d.TLSDir) != d.TLSDir || len(d.TLSDir) > 1024 {
		return invalid
	}
	return nil
}
