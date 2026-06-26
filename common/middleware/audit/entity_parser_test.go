package audit

import "testing"

func TestParseEntityFromPathManagerEmbeddingRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantType   string
		wantEntity string
	}{
		{
			name:       "ad hoc embedding execution",
			method:     "POST",
			path:       "/api/v1/manager/embedding_executions",
			wantType:   "embedding_execution",
			wantEntity: "",
		},
		{
			name:       "delete embedding result",
			method:     "DELETE",
			path:       "/api/v1/manager/embeddings/42",
			wantType:   "embedding",
			wantEntity: "42",
		},
		{
			name:       "create embedding task",
			method:     "POST",
			path:       "/api/v1/manager/embedding_tasks",
			wantType:   "embedding_task",
			wantEntity: "",
		},
		{
			name:       "update embedding task",
			method:     "PUT",
			path:       "/api/v1/manager/embedding_tasks/7",
			wantType:   "embedding_task",
			wantEntity: "7",
		},
		{
			name:       "execute embedding task",
			method:     "POST",
			path:       "/api/v1/manager/tasks/embedding/7/execute",
			wantType:   "embedding_execution",
			wantEntity: "7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseEntityFromPath(tt.method, tt.path)
			if got == nil {
				t.Fatalf("ParseEntityFromPath() = nil, want %s/%s", tt.wantType, tt.wantEntity)
			}
			if got.Type != tt.wantType || got.ID != tt.wantEntity {
				t.Fatalf("ParseEntityFromPath() = %s/%s, want %s/%s", got.Type, got.ID, tt.wantType, tt.wantEntity)
			}
		})
	}
}

func TestParseEntityFromPathVersionedModuleRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantType   string
		wantEntity string
	}{
		{
			name:       "system user",
			method:     "PUT",
			path:       "/api/v1/system/users/123",
			wantType:   "user",
			wantEntity: "123",
		},
		{
			name:       "system engine sub action",
			method:     "POST",
			path:       "/api/v1/system/engines/456/test",
			wantType:   "engine",
			wantEntity: "456",
		},
		{
			name:       "transfer task",
			method:     "GET",
			path:       "/api/v1/transfer/tasks/sync/789",
			wantType:   "transfer_task",
			wantEntity: "789",
		},
		{
			name:       "develop workflow engine operators",
			method:     "GET",
			path:       "/api/v1/develop/workflow-engines/12/operators",
			wantType:   "workflow_engine",
			wantEntity: "12",
		},
		{
			name:       "old develop operator route is not accepted",
			method:     "GET",
			path:       "/api/v1/develop/operators/buffer",
			wantType:   "",
			wantEntity: "",
		},
		{
			name:       "old unversioned system route is not accepted",
			method:     "GET",
			path:       "/api/users/123",
			wantType:   "",
			wantEntity: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseEntityFromPath(tt.method, tt.path)
			if tt.wantType == "" {
				if got != nil {
					t.Fatalf("ParseEntityFromPath() = %s/%s, want nil", got.Type, got.ID)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParseEntityFromPath() = nil, want %s/%s", tt.wantType, tt.wantEntity)
			}
			if got.Type != tt.wantType || got.ID != tt.wantEntity {
				t.Fatalf("ParseEntityFromPath() = %s/%s, want %s/%s", got.Type, got.ID, tt.wantType, tt.wantEntity)
			}
		})
	}
}
