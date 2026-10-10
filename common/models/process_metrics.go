package models

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const ProcessMetricsSchema = "addp.process-metrics/v1"

// ProcessMetricsDeclaration is a deployment declaration, never a user endpoint.
type ProcessMetricsDeclaration struct {
	SchemaVersion string `json:"schema_version"`
	Endpoint      string `json:"endpoint"`
}

func (d *ProcessMetricsDeclaration) Validate() error {
	if d == nil {
		return nil
	}
	u, err := url.Parse(d.Endpoint)
	if err != nil || d.SchemaVersion != ProcessMetricsSchema || len(d.Endpoint) > 512 ||
		strings.ContainsAny(d.Endpoint, "%@?#\\\r\n\t ") || u.Scheme != "https" || u.Hostname() == "" ||
		u.User != nil || u.Path != "/metrics" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return errors.New("invalid process metrics declaration")
	}
	p, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil || p == 0 || strconv.FormatUint(p, 10) != u.Port() {
		return errors.New("invalid process metrics declaration")
	}
	host := u.Hostname()
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Is4In6() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.Zone() != "" || ip.String() != host {
			return errors.New("invalid process metrics declaration")
		}
	} else {
		if len(host) > 253 {
			return errors.New("invalid process metrics declaration")
		}
		for _, label := range strings.Split(host, ".") {
			if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return errors.New("invalid process metrics declaration")
			}
			for _, r := range label {
				if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
					return errors.New("invalid process metrics declaration")
				}
			}
		}
		if strings.Trim(host, "0123456789.") == "" {
			return errors.New("invalid process metrics declaration")
		}
	}
	if u.Host != net.JoinHostPort(host, u.Port()) {
		return errors.New("invalid process metrics declaration")
	}
	return nil
}
