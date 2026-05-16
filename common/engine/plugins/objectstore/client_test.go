package objectstore

import (
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestSplitBucketPrefixKeepsObjectKey(t *testing.T) {
	bucket, prefix := SplitBucketPrefix("addp/contain/shapefile.zip")
	if bucket != "addp" || prefix != "contain/shapefile.zip" {
		t.Fatalf("SplitBucketPrefix() = %q, %q", bucket, prefix)
	}
}

func TestSplitBucketDirectoryNormalizesPrefix(t *testing.T) {
	tests := []struct {
		path       string
		wantBucket string
		wantPrefix string
	}{
		{path: "addp/contain", wantBucket: "addp", wantPrefix: "contain/"},
		{path: "addp/contain/", wantBucket: "addp", wantPrefix: "contain/"},
		{path: "addp", wantBucket: "addp", wantPrefix: ""},
	}

	for _, tt := range tests {
		bucket, prefix := SplitBucketDirectory(tt.path)
		if bucket != tt.wantBucket || prefix != tt.wantPrefix {
			t.Fatalf("SplitBucketDirectory(%q) = %q, %q; want %q, %q",
				tt.path, bucket, prefix, tt.wantBucket, tt.wantPrefix)
		}
	}
}

func TestCreateContentRejectsInvalidPath(t *testing.T) {
	_, err := CreateContent(nil, nil, "bucket-only", plugin.WriteOptions{Overwrite: true})
	if err == nil {
		t.Fatal("CreateContent succeeded, want invalid path error")
	}
	if !strings.Contains(err.Error(), "object store client is required") {
		t.Fatalf("error = %q, want client error", err)
	}
}
