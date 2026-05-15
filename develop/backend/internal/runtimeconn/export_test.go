package runtimeconn

import (
	"testing"

	"github.com/addp/common/models"

	_ "github.com/addp/common/engine/plugins/builtin/general"
)

func TestBuildNotebookConnectionForPostgreSQL(t *testing.T) {
	t.Parallel()

	got, err := BuildNotebookConnection(&models.Engine{
		ID:         7,
		Name:       "pg",
		EngineType: "postgresql",
		ConnectionInfo: models.ConnectionInfo{
			"host":     "localhost",
			"port":     15432,
			"user":     "postgres",
			"password": "secret",
			"database": "business",
		},
	}, ExportOptions{})
	if err != nil {
		t.Fatalf("BuildNotebookConnection() error = %v", err)
	}

	if got["type"] != "postgresql" || got["engine_id"] != uint(7) {
		t.Fatalf("unexpected base fields: %+v", got)
	}
	if got["user"] != "postgres" || got["database"] != "business" {
		t.Fatalf("unexpected driver fields: %+v", got)
	}
	if got["connection_string"] == "" {
		t.Fatalf("expected connection_string, got %+v", got)
	}
}

func TestBuildNotebookConnectionForObjectStorage(t *testing.T) {
	t.Parallel()

	got, err := BuildNotebookConnection(&models.Engine{
		ID:         8,
		Name:       "minio",
		EngineType: "minio",
		ConnectionInfo: models.ConnectionInfo{
			"endpoint":   "localhost:19000",
			"access_key": "ak",
			"secret_key": "sk",
			"bucket":     "data",
			"secure":     true,
		},
	}, ExportOptions{})
	if err != nil {
		t.Fatalf("BuildNotebookConnection() error = %v", err)
	}

	if got["type"] != "minio" || got["endpoint"] != "localhost:19000" || got["secure"] != true {
		t.Fatalf("unexpected object storage fields: %+v", got)
	}
	if _, ok := got["connection_string"]; ok {
		t.Fatalf("object storage should not expose connection_string: %+v", got)
	}
}
