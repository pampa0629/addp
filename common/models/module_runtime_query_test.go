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

func TestProcessInstanceIDsAreBoundedOpaqueAndUnique(t *testing.T) {
	ids, e := ParseProcessInstanceIDs("one,old-instance_2")
	if e != nil || len(ids) != 2 {
		t.Fatal(ids, e)
	}
	for _, value := range []string{"", "one,one", "one,", "../private", "a b", "\nprivate", strings.Repeat("a", 101), strings.Repeat("a,", 100) + "b"} {
		if _, e := ParseProcessInstanceIDs(value); e == nil {
			t.Fatal("unbounded process ID accepted")
		}
	}
}
