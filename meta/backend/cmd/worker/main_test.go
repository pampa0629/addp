package main

import (
	"testing"

	"github.com/addp/common/format"
)

func TestWorkerRegistersBuiltinFormats(t *testing.T) {
	if got := format.DetectFormat("source.mdb", nil); got != format.FormatAccess {
		t.Fatalf("DetectFormat(source.mdb) = %q, want %q", got, format.FormatAccess)
	}
}
