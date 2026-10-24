package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/addp/system/internal/auditarchive"
)

func TestArchiveCLIRejectsUntrustedIdentityAndWriteActions(t *testing.T) {
	for _, args := range [][]string{nil, {"delete"}, {"query", "--actor", "fake"}, {"collect", "--tenant-id", "1"}, {"verify", "--config", "/does-not-exist"}, {"check-store", "--tenant-id", "1"}, {"check-store", "--config", "/does-not-exist"}} {
		var out bytes.Buffer
		if run(context.Background(), args, &out) == nil || out.Len() != 0 {
			t.Fatal("invalid invocation produced output")
		}
	}
}

func TestArchivePolicyIsOfflineAndContainsNoDeploymentSecrets(t *testing.T) {
	const source = "a21db94b-6dd5-446a-b66a-df379ea51dc8"
	config := auditarchive.Config{Mode: "auditor", SourceID: source, Endpoint: "https://must-not-connect.invalid", Region: "us-east-1", Bucket: "system", AccessKey: "private-identity", SecretKey: "private-secret", RetentionDays: 365, Actor: "748d2bb7-8071-4a85-83b8-873f5c8e1fd7", Root: auditarchive.Ref{Key: "audit/sources/" + source + "/root.json", Version: "fixture-version", SHA: strings.Repeat("a", 64)}}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "auditor.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = run(context.Background(), []string{"policy", "--config", path}, &out); err != nil {
		t.Fatal("offline policy tried to contact a deployment", err)
	}
	text := out.String()
	for _, secret := range []string{config.AccessKey, config.SecretKey, config.Endpoint, config.Root.Version} {
		if strings.Contains(text, secret) {
			t.Fatal("policy disclosed private deployment material")
		}
	}
	if !strings.Contains(text, "audit/sources/"+source) || !strings.Contains(text, "audit/queries/"+config.Actor) || strings.Contains(text, "DateGreaterThan") {
		t.Fatal("policy lost its scopes or introduced an expiring date floor")
	}
}

func TestArchiveCLIReceiverActionsCannotCrossCredentialModes(t *testing.T) {
	// Invalid arguments must fail before loading a credential file or opening a
	// listener. Receiver source/credentials cannot be selected through flags.
	for _, args := range [][]string{{"receive", "--source-id", "forged"}, {"receiver-init", "--tenant-id", "1"}, {"receiver-verify", "--event-name", "GetObject"}, {"receive", "--config", "/does-not-exist"}} {
		var out bytes.Buffer
		if run(context.Background(), args, &out) == nil || out.Len() != 0 {
			t.Fatal("receiver invocation escaped its private configuration")
		}
	}
}
