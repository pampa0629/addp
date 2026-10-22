package main

import (
	"bytes"
	"context"
	"testing"
)

func TestArchiveCLIRejectsUntrustedIdentityAndWriteActions(t *testing.T) {
	for _, args := range [][]string{nil, {"delete"}, {"query", "--actor", "fake"}, {"collect", "--tenant-id", "1"}, {"verify", "--config", "/does-not-exist"}} {
		var out bytes.Buffer
		if run(context.Background(), args, &out) == nil || out.Len() != 0 {
			t.Fatal("invalid invocation produced output")
		}
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
