package metapath

import "testing"

func TestJoinObjectPathPartsTrimsEmptySegments(t *testing.T) {
	t.Parallel()

	got := JoinObjectPathParts("/root/", "", "child/file.parquet")
	if got != "root/child/file.parquet" {
		t.Fatalf("joined path = %q", got)
	}
}

func TestPhysicalWhitespacePathsRemainDistinct(t *testing.T) {
	for _, raw := range []string{" TEST.PNG ", " models / TEST.PNG ", " ", "\t资料%2F\u3000"} {
		if got := SanitizeFSPath(raw); got != raw {
			t.Fatalf("filesystem %q became %q", raw, got)
		}
		if got := SanitizeObjectPath(raw); got != raw {
			t.Fatalf("object %q became %q", raw, got)
		}
	}
	if got := PrepareCatalogPaths([]string{"TEST.PNG", " TEST.PNG ", " "}, nil); len(got) != 3 {
		t.Fatalf("selectors merged: %#v", got)
	}
}
