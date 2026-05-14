package metapath

import "testing"

func TestJoinObjectPathPartsTrimsEmptySegments(t *testing.T) {
	t.Parallel()

	got := JoinObjectPathParts("/root/", "", "child/file.parquet")
	if got != "root/child/file.parquet" {
		t.Fatalf("joined path = %q", got)
	}
}
