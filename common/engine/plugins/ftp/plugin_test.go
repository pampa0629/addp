package ftp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestConnectionDescriptorAndReadOnlyCapabilities(t *testing.T) {
	p := &FTPPlugin{}
	registered, err := plugin.Get("ftp")
	if err != nil || registered.Type() != p.Type() || registered.EngineOrigin() != "general" {
		t.Fatal("FTP builtin registration missing", err)
	}
	if err := p.ConnectionSpec().Validate(); err != nil {
		t.Fatal(err)
	}
	if p.DefaultPort() != 21 || len(p.SensitiveFields()) != 1 || p.SensitiveFields()[0] != "password" {
		t.Fatal("connection facts diverged from descriptor")
	}
	c := p.Capabilities()
	if c.EngineFamily != "file" || c.Storage.Store.StreamWrite || c.Storage.Store.Delete || !c.Storage.Store.RangeRead || !c.Storage.Store.StreamRead {
		t.Fatal("unadmitted capabilities declared")
	}
	if _, ok := any(p).(plugin.ContentWritableProvider); ok {
		t.Fatal("FTP exposes write provider")
	}
	info := plugin.ConnectionInfo{"host": "127.0.0.1", "port": 2121, "user": "reader", "password": "private", "security": "explicit_tls"}
	if p.ValidateConnectionInfo(info) != nil {
		t.Fatal("valid explicit TLS facts rejected")
	}
	for _, field := range []string{"host", "user", "password", "port", "security"} {
		bad := plugin.ConnectionInfo{}
		for k, v := range info {
			bad[k] = v
		}
		delete(bad, field)
		if p.ValidateConnectionInfo(bad) == nil {
			t.Fatalf("missing %s accepted", field)
		}
	}
}

func TestConnectionFactsAreStrictAndAccountRootIsNotConfigurable(t *testing.T) {
	p := &FTPPlugin{}
	if _, exists := p.ConnectionSpec().Field("root_path"); exists {
		t.Fatal("client management root must not be configurable")
	}
	valid := plugin.ConnectionInfo{"host": "localhost", "port": 2121, "user": "reader", "password": "private", "security": "explicit_tls"}
	for _, port := range []any{2121, float64(2121), json.Number("2121")} {
		valid["port"] = port
		if err := p.ValidateConnectionInfo(valid); err != nil {
			t.Fatal(err)
		}
	}
	for _, port := range []any{nil, 0, 65536, 2121.5, "2121junk", "2121", json.Number("2121.5")} {
		valid["port"] = port
		if p.ValidateConnectionInfo(valid) == nil {
			t.Fatalf("invalid port accepted: %v", port)
		}
	}
	valid["port"] = 2121
	for _, field := range []string{"root_path", "skip_verify", "unknown"} {
		valid[field] = "/examples"
		if p.ValidateConnectionInfo(valid) == nil {
			t.Fatalf("undeclared field accepted: %s", field)
		}
		delete(valid, field)
	}
	valid["user"] = 123
	if p.ValidateConnectionInfo(valid) == nil {
		t.Fatal("non-text username accepted")
	}
}

func TestInvalidCatalogRejectedBeforeNetwork(t *testing.T) {
	p := &FTPPlugin{}
	for _, target := range []plugin.EngineCatalogPath{
		{}, {Version: plugin.EngineCatalogPathVersion},
		{Version: plugin.EngineCatalogPathVersion, Segments: []plugin.EngineCatalogSegment{{Term: "root", Kind: "root"}, {Term: "file", Kind: "file", Name: "../escape"}}},
		{Version: plugin.EngineCatalogPathVersion, Segments: []plugin.EngineCatalogSegment{{Term: "root", Kind: "root"}, {Term: "file", Kind: "file", Name: "a"}, {Term: "directory", Kind: "directory", Name: "b"}}},
	} {
		if _, err := p.OpenContent(context.Background(), nil, target, plugin.ReadOptions{}); err == nil {
			t.Fatal("invalid catalog reached content reader")
		}
	}
}
