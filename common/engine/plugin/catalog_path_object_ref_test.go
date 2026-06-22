package plugin

import "testing"

func TestSplitObjectRefPath(t *testing.T) {
	t.Parallel()

	bucket, objectPath, err := SplitObjectRefPath("/manager/path/farmland.geojson")
	if err != nil {
		t.Fatalf("SplitObjectRefPath() error = %v", err)
	}
	if bucket != "manager" || objectPath != "path/farmland.geojson" {
		t.Fatalf("bucket/objectPath = %q/%q, want manager/path/farmland.geojson", bucket, objectPath)
	}
}

func TestSplitObjectRefPathRejectsInvalidRefs(t *testing.T) {
	t.Parallel()

	for _, refPath := range []string{"", "/", "manager", "manager/", "/manager/"} {
		refPath := refPath
		t.Run(refPath, func(t *testing.T) {
			t.Parallel()
			if _, _, err := SplitObjectRefPath(refPath); err == nil {
				t.Fatalf("SplitObjectRefPath(%q) accepted invalid ref", refPath)
			}
		})
	}
}

func TestObjectItemPathFromRefPath(t *testing.T) {
	t.Parallel()

	path, err := ObjectItemPathFromRefPath(7, "manager/farmland.geojson")
	if err != nil {
		t.Fatalf("ObjectItemPathFromRefPath() error = %v", err)
	}
	if got := path.StringPath(); got != "manager/farmland.geojson" {
		t.Fatalf("path = %q, want manager/farmland.geojson", got)
	}
}

func TestObjectItemPathFromBucketRefDoesNotRepeatBucket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pathValue string
		want      string
	}{
		{name: "bucket relative key", pathValue: "farmland.geojson", want: "manager/farmland.geojson"},
		{name: "same bucket qualified ref", pathValue: "manager/farmland.geojson", want: "manager/farmland.geojson"},
		{name: "nested same bucket qualified ref", pathValue: "manager/path/farmland.geojson", want: "manager/path/farmland.geojson"},
		{name: "different bucket-like key remains object key", pathValue: "archive/farmland.geojson", want: "manager/archive/farmland.geojson"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ObjectItemPathFromBucketRef(7, "manager", tt.pathValue).StringPath(); got != tt.want {
				t.Fatalf("path = %q, want %q", got, tt.want)
			}
		})
	}
}
