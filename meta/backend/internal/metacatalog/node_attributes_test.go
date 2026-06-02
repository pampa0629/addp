package metacatalog

import "testing"

func TestNodeAttributesUseNodeStorageSection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		attrs map[string]interface{}
		want  map[string]interface{}
	}{
		{
			name:  "bucket",
			attrs: ObjectBucketNodeAttributes("addp"),
			want:  map[string]interface{}{"bucket": "addp"},
		},
		{
			name:  "prefix",
			attrs: ObjectPrefixNodeAttributes("addp", "roads/"),
			want:  map[string]interface{}{"bucket": "addp", "path": "roads/"},
		},
		{
			name:  "file-directory",
			attrs: FileDirectoryNodeAttributes("docs"),
			want:  map[string]interface{}{"path": "docs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.attrs["schema_version"] != 1 {
				t.Fatalf("schema_version = %#v, want 1", tt.attrs["schema_version"])
			}
			if tt.attrs["bucket"] != nil || tt.attrs["path"] != nil {
				t.Fatalf("node attributes should not use flat bucket/path: %#v", tt.attrs)
			}
			storage, ok := tt.attrs["storage"].(map[string]interface{})
			if !ok {
				t.Fatalf("storage section = %#v, want map", tt.attrs["storage"])
			}
			for key, want := range tt.want {
				if storage[key] != want {
					t.Fatalf("storage[%s] = %#v, want %#v", key, storage[key], want)
				}
			}
		})
	}
}
