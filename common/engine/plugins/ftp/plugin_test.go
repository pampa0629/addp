package ftp

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestConnectionDescriptorAndReadOnlyCapabilities(t *testing.T) {
	p := &FTPPlugin{}
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
	info := plugin.ConnectionInfo{"host": "127.0.0.1", "port": 2121, "user": "reader", "password": "private", "root_path": "/examples", "security": "explicit_tls"}
	if p.ValidateConnectionInfo(info) != nil {
		t.Fatal("valid explicit TLS facts rejected")
	}
	for _, field := range []string{"host", "user", "password", "root_path", "security"} {
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
