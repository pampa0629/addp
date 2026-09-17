package semantic

import (
	"bytes"
	"strings"
	"testing"
)

func TestRestoreSnapshot(t *testing.T) {
	s := mustFreeze(t, outdoor())
	restored, err := Restore(s.CanonicalJSON(), s.Digest())
	if err != nil || restored.Scope() != s.Scope() || !bytes.Equal(restored.CanonicalJSON(), s.CanonicalJSON()) {
		t.Fatalf("restore: %v", err)
	}
	for _, data := range [][]byte{
		[]byte(`{}`), append(s.CanonicalJSON(), []byte(` {}`)...),
		append([]byte(" "), s.CanonicalJSON()...),
		bytes.ReplaceAll(s.CanonicalJSON(), []byte(CompilerVersion), []byte("other")),
		bytes.ReplaceAll(s.CanonicalJSON(), []byte(`"contract":`), []byte(`"extra":0,"contract":`)),
		[]byte(strings.Repeat(" ", (2<<20)+1)),
	} {
		if _, err := Restore(data, s.Digest()); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
	if _, err := Restore(s.CanonicalJSON(), strings.Repeat("0", 64)); err == nil {
		t.Fatal("bad digest accepted")
	}
}
