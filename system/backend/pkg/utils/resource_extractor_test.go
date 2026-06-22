package utils

import "testing"

func TestExtractResourceInfoVersionedRoutes(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantType   string
		wantEntity string
	}{
		{
			name:       "system user",
			path:       "/api/v1/system/users/12",
			wantType:   "user",
			wantEntity: "12",
		},
		{
			name:       "system engine sub action",
			path:       "/api/v1/system/engines/34/test",
			wantType:   "engine",
			wantEntity: "34",
		},
		{
			name:       "transfer task provider route",
			path:       "/api/v1/transfer/tasks/sync/56",
			wantType:   "task",
			wantEntity: "56",
		},
		{
			name:       "old unversioned path rejected",
			path:       "/api/users/78",
			wantType:   "",
			wantEntity: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotEntity := ExtractResourceInfo("GET", tt.path, "")
			if gotType != tt.wantType || gotEntity != tt.wantEntity {
				t.Fatalf("ExtractResourceInfo() = %q/%q, want %q/%q", gotType, gotEntity, tt.wantType, tt.wantEntity)
			}
		})
	}
}
