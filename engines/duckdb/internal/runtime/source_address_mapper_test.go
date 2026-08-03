package runtime

import (
	"reflect"
	"testing"

	commonmodels "github.com/addp/common/models"
)

func TestMapSourceEngineLoopbackAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		engineType string
		connection commonmodels.ConnectionInfo
		want       commonmodels.ConnectionInfo
	}{
		{
			name:       "postgresql host",
			engineType: "postgresql",
			connection: commonmodels.ConnectionInfo{"host": "localhost", "port": float64(5433), "database": "business"},
			want:       commonmodels.ConnectionInfo{"host": "host.docker.internal", "port": float64(5433), "database": "business"},
		},
		{
			name:       "mysql ipv4 loopback host",
			engineType: "mysql",
			connection: commonmodels.ConnectionInfo{"host": "127.0.0.1", "port": float64(3306), "database": "business"},
			want:       commonmodels.ConnectionInfo{"host": "host.docker.internal", "port": float64(3306), "database": "business"},
		},
		{
			name:       "minio endpoint",
			engineType: "minio",
			connection: commonmodels.ConnectionInfo{"endpoint": "localhost:9002", "use_ssl": false},
			want:       commonmodels.ConnectionInfo{"endpoint": "host.docker.internal:9002", "use_ssl": false},
		},
		{
			name:       "s3 url endpoint",
			engineType: "s3",
			connection: commonmodels.ConnectionInfo{"endpoint": "http://[::1]:9002/storage", "use_ssl": false},
			want:       commonmodels.ConnectionInfo{"endpoint": "http://host.docker.internal:9002/storage", "use_ssl": false},
		},
		{
			name:       "remote endpoint unchanged",
			engineType: "s3",
			connection: commonmodels.ConnectionInfo{"endpoint": "s3.amazonaws.com", "use_ssl": true},
			want:       commonmodels.ConnectionInfo{"endpoint": "s3.amazonaws.com", "use_ssl": true},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			original := commonmodels.ConnectionInfo{}
			for key, value := range tt.connection {
				original[key] = value
			}
			engine := commonmodels.Engine{ID: 7, EngineType: tt.engineType, ConnectionInfo: tt.connection}

			got := mapSourceEngineLoopbackAddress(engine, "host.docker.internal")

			if !reflect.DeepEqual(got.ConnectionInfo, tt.want) {
				t.Fatalf("mapped connection_info = %#v, want %#v", got.ConnectionInfo, tt.want)
			}
			if !reflect.DeepEqual(engine.ConnectionInfo, original) {
				t.Fatalf("source engine was mutated: %#v", engine.ConnectionInfo)
			}
		})
	}
}

func TestMapSourceEngineLoopbackAddressDisabled(t *testing.T) {
	t.Parallel()

	engine := commonmodels.Engine{
		EngineType:     "postgresql",
		ConnectionInfo: commonmodels.ConnectionInfo{"host": "localhost", "port": float64(5433)},
	}
	got := mapSourceEngineLoopbackAddress(engine, "")
	if !reflect.DeepEqual(got.ConnectionInfo, engine.ConnectionInfo) {
		t.Fatalf("disabled mapping changed connection_info: %#v", got.ConnectionInfo)
	}
}
