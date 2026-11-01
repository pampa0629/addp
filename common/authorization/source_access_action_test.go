package authorization

import "testing"

func TestSourceAccessActionRequiresExplicitCanonicalAction(t *testing.T) {
	for _, action := range []string{"", "READ", "metadata", "write", "*", "read ", "metadata_read ", "read,metadata_read"} {
		if ValidSourceAccessAction(action) {
			t.Fatalf("invalid source action accepted: %q", action)
		}
	}
	for _, action := range []string{SourceAccessRead, SourceAccessMetadataRead} {
		if !ValidSourceAccessAction(action) {
			t.Fatalf("source action rejected: %q", action)
		}
	}
}
