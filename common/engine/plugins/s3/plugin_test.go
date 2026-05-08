package s3

import (
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestParseConnInfoDefaultsSSLToTrue(t *testing.T) {
	p := &S3Plugin{}
	_, _, _, useSSL, err := p.parseConnInfo(plugin.ConnectionInfo{
		"endpoint":   "s3.amazonaws.com",
		"access_key": "ak",
		"secret_key": "sk",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !useSSL {
		t.Fatal("expected use_ssl to default to true")
	}
}

func TestParseConnInfoRespectsExplicitFalseSSL(t *testing.T) {
	p := &S3Plugin{}
	_, _, _, useSSL, err := p.parseConnInfo(plugin.ConnectionInfo{
		"endpoint":   "localhost:9000",
		"access_key": "ak",
		"secret_key": "sk",
		"use_ssl":    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if useSSL {
		t.Fatal("expected explicit use_ssl=false to be respected")
	}
}
