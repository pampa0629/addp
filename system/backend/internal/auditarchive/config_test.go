package auditarchive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveConfigKeepsCollectorAndReaderCredentialsSeparate(t *testing.T) {
	c := Config{Mode: "auditor", SourceID: testSourceID, Endpoint: "https://audit.example", Region: "us-east-1", Bucket: "audit", AccessKey: "fixture", SecretKey: "fixture-secret", RetentionDays: 365, Actor: testActor, Root: Ref{rootKey(testSourceID), "fixture-version", digest([]byte("fixture"))}}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "private.json")
	data, _ := json.Marshal(c)
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadConfig(path); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadConfig(path); e == nil {
		t.Fatal("world-readable credentials accepted")
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.Endpoint = "http://audit.example" },
		func(c *Config) { c.PostgresDSN = "postgres://source" },
		func(c *Config) {
			c.Mode = "collector"
			c.Actor = ""
			c.Slot = "slot"
			c.Publication = "pub"
			c.PostgresDSN = "postgres://fixture@localhost/test?sslmode=disable"
		},
		func(c *Config) { c.Endpoint = "https://user:secret@audit.example" },
		func(c *Config) { c.RetentionDays = 0 },
	} {
		bad := c
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("unsafe deployment input accepted")
		}
	}
}

func TestDecoderRefusesMutationAndUncommittedPayload(t *testing.T) {
	for _, b := range [][]byte{nil, {'U'}, {'D'}, {'T'}, {'I', 0}, {'C', 0}, {'R', 0}, {'B', 0}} {
		d := decoder{}
		if c, e := d.parse(b); e == nil || c != nil {
			t.Fatal("accepted malformed or nonappend message")
		}
	}
}
