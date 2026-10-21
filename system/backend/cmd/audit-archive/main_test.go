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
