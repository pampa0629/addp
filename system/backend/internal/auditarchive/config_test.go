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
		func(c *Config) { c.Bucket = "*" },
		func(c *Config) { c.Bucket = "system/audit" },
		func(c *Config) { c.Root.Key = "sources/" + testSourceID + "/root.json" },
	} {
		bad := c
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("unsafe deployment input accepted")
		}
	}
}

func TestArchiveNamespaceNeverFallsBackToTenantOrBucketRoot(t *testing.T) {
	prefix, err := sourcePrefix(testSourceID)
	if err != nil || prefix != "audit/sources/"+testSourceID+"/" {
		t.Fatal("archive escaped the platform audit namespace", prefix, err)
	}
	if rootKey(testSourceID) != prefix+"root.json" || checkpointKey(testSourceID, 7) != prefix+"checkpoints/00000000000000000007.json" || partKey(testSourceID, 7, 2) != prefix+"data/00000000000000000007/000002.json" {
		t.Fatal("archive object keys use different namespaces")
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

func TestReceiverConfigBindsSourcePrivateTLSAndReadOnlyIdentity(t *testing.T) {
	c := Config{Mode: "receiver", SourceID: testSourceID, DeploymentID: receiverTestDeployment, Endpoint: "https://audit.example", Region: "us-east-1", Bucket: "audit", AccessKey: "fixture", SecretKey: "fixture-secret", RetentionDays: 365, ReceiverToken: receiverTestToken, Listen: "127.0.0.1:9443", TLSCert: "/private/server.pem", TLSKey: "/private/server-key.pem"}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.Listen = ":9443" }, func(c *Config) { c.Listen = "127.0.0.1:0" }, func(c *Config) { c.ReceiverToken = "short" }, func(c *Config) { c.TLSKey = "relative.pem" }, func(c *Config) { c.PostgresDSN = "postgres://other" }, func(c *Config) { c.Root = Ref{rootKey(testSourceID), "v", digest([]byte("x"))} }, func(c *Config) { c.Mode = "auditor" }, func(c *Config) { c.Checkpoint = Ref{"access/foreign/root.json", "v", digest([]byte("x"))} },
	} {
		bad := c
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("unsafe receiver configuration accepted")
		}
	}
	c.Mode = "receiver-auditor"
	c.ReceiverToken = ""
	c.Listen = ""
	c.TLSCert = ""
	c.TLSKey = ""
	c.Root = Ref{accessPrefix(testSourceID) + "root.json", "v", digest([]byte("x"))}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	c.ReceiverToken = receiverTestToken
	if c.Validate() == nil {
		t.Fatal("read-only configuration has incoming credential")
	}
}
