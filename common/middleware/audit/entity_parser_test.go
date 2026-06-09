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
