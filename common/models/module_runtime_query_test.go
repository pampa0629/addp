package models

import (
	"strings"
	"testing"
)

func TestRuntimeInstanceIDsAreBoundedCanonicalAndUnique(t *testing.T) {
	ids, err := ParseRuntimeInstanceIDs("1,22,333")
	if err != nil || len(ids) != 3 || ids[1] != 22 {
		t.Fatal(ids, err)
	}
	for _, raw := range []string{"", "0", "01", "1,1", "1,", "-1", "+1", "1, 2", "1.0", "9223372036854775808", strings.Repeat("1,", 100) + "2", strings.Repeat(" ", 2001)} {
		if _, err := ParseRuntimeInstanceIDs(raw); err == nil {
			t.Fatalf("invalid IDs accepted: %.100s", raw)
		}
	}
}
