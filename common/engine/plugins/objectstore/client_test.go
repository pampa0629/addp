package objectstore

import "testing"

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
