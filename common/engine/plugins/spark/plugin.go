// Package spark registers a Standalone compute target, not a SQL storage server.
package spark

import (
	"context"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
)

type Plugin struct{}

func init()                               { plugin.Register(&Plugin{}) }
func (*Plugin) Type() string              { return "spark" }
func (*Plugin) DisplayName() string       { return "Apache Spark" }
func (*Plugin) RegistrationGroup() string { return plugin.EngineRegistrationGroupCompute }
func (*Plugin) EngineOrigin() string      { return "general" }
func (*Plugin) ConnectionSpec() plugin.ConnectionSpec {
	return plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "host", LabelKey: "storageEngine.host", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "host.docker.internal", Placeholder: "host.docker.internal"},
		plugin.ConnectionFieldSpec{Key: "port", LabelKey: "storageEngine.sparkMasterPort", Input: plugin.ConnectionFieldNumber, Required: true, Identity: true, Default: 7077, Min: plugin.Int(1), Max: plugin.Int(65535)},
		plugin.ConnectionFieldSpec{Key: "web_ui_url", LabelKey: "storageEngine.sparkMasterWebUI", Input: plugin.ConnectionFieldText, Required: true, Placeholder: "http://localhost:18088", HintKey: "storageEngine.hints.spark"},
	)
}
func (p *Plugin) DefaultPort() int                   { return p.ConnectionSpec().DefaultPortValue() }
func (p *Plugin) RequiredFields() []string           { return p.ConnectionSpec().RequiredFields() }
func (p *Plugin) SensitiveFields() []string          { return p.ConnectionSpec().SensitiveFields() }
func (p *Plugin) ConnectionIdentityFields() []string { return p.ConnectionSpec().IdentityFields() }
func (p *Plugin) Capabilities() plugin.EngineCapabilities {
	// Query is enabled only after the shared Runtime implements the complete protocol.
	return plugin.EngineCapabilities{SchemaVersion: plugin.CapabilitiesSchemaVersion, EngineType: p.Type(), EngineFamily: "compute", Compute: &plugin.ComputeCapabilities{}}
}

func (p *Plugin) ValidateConnectionInfo(info plugin.ConnectionInfo) error {
	for key := range info {
		if _, ok := p.ConnectionSpec().Field(key); !ok {
			return commoni18n.NewMessageError("engine.spark.configuration")
		}
	}
	if _, err := masterEndpoint(info); err != nil {
		return err
	}
	_, err := webEndpoint(info)
	return err
}

func masterEndpoint(info plugin.ConnectionInfo) (string, error) {
	host, ok := info["host"].(string)
	if !ok || host == "" || host != strings.TrimSpace(host) || strings.ContainsAny(host, "/@?#\\ \t\n\r") {
		return "", commoni18n.NewMessageError("engine.spark.configuration")
	}
	// Accept bare IPv6, never a URL or a host with an embedded port.
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", commoni18n.NewMessageError("engine.spark.configuration")
	}
	port := 0
	switch value := info["port"].(type) {
	case int:
		port = value
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < 1 || value > 65535 {
			return "", commoni18n.NewMessageError("engine.spark.configuration")
		}
		port = int(value)
	default:
		return "", commoni18n.NewMessageError("engine.spark.configuration")
	}
	if port < 1 || port > 65535 {
		return "", commoni18n.NewMessageError("engine.spark.configuration")
	}
	return net.JoinHostPort(plugin.NormalizeHost(host), strconv.Itoa(port)), nil
}

func webEndpoint(info plugin.ConnectionInfo) (*url.URL, error) {
	raw, ok := info["web_ui_url"].(string)
	if !ok || raw == "" || raw != strings.TrimSpace(raw) {
		return nil, commoni18n.NewMessageError("engine.spark.configuration")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, commoni18n.NewMessageError("engine.spark.configuration")
	}
	if u.Port() != "" {
		n, err := strconv.Atoi(u.Port())
		if err != nil || n < 1 || n > 65535 {
			return nil, commoni18n.NewMessageError("engine.spark.configuration")
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return nil, commoni18n.NewMessageError("engine.spark.configuration")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/json/"
	u.RawPath = ""
	return u, nil
}

func (p *Plugin) TestConnection(ctx context.Context, info plugin.ConnectionInfo) error {
	if err := p.ValidateConnectionInfo(info); err != nil {
		return err
	}
	endpoint, _ := masterEndpoint(info)
	web, _ := webEndpoint(info)
	return probeMaster(ctx, endpoint, web.String())
}
