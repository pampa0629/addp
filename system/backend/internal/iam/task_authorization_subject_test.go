package iam

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestTaskAuthorizationBindingsAreExactOwnerTypeAndClientPairs(t *testing.T) {
	for _, test := range []struct{ owner, kind, permission, client string }{
		{"transfer", "sync", "transfer.task.execute", "addp-transfer"},
		{"orchestrator", "orchestration", "orchestrator.workflow.execute", "addp-orchestrator"},
		{"transfer", "orchestration", "", ""}, {"orchestrator", "sync", "", ""}, {" transfer", "sync", "", ""}, {"manager", "sync", "", ""},
	} {
		permission, client := taskAuthorizationBinding(test.owner, test.kind)
		if permission != test.permission || client != test.client {
			t.Fatalf("incorrect owner/type binding: %#v", test)
		}
		err := validateTaskAuthorizationIdentity(test.owner, test.kind, uuid.New(), strings.Repeat("a", 64))
		if (err == nil) != (test.permission != "") {
			t.Fatalf("invalid identity acceptance: %#v", test)
		}
	}
}
