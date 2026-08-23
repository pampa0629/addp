package engineaccess

import (
	"errors"
	"testing"

	commonModels "github.com/addp/common/models"
)

func TestEnsureAvailable(t *testing.T) {
	tests := []struct {
		name   string
		engine *commonModels.Engine
		want   error
	}{
		{name: "active online", engine: &commonModels.Engine{LifecycleState: commonModels.EngineLifecycleActive, ConnectionStatus: commonModels.EngineConnectionOnline}},
		{name: "active offline", engine: &commonModels.Engine{LifecycleState: commonModels.EngineLifecycleActive, ConnectionStatus: commonModels.EngineConnectionOffline}, want: ErrUnavailable},
		{name: "active unknown", engine: &commonModels.Engine{LifecycleState: commonModels.EngineLifecycleActive, ConnectionStatus: commonModels.EngineConnectionUnknown}, want: ErrUnavailable},
		{name: "disabled online", engine: &commonModels.Engine{LifecycleState: commonModels.EngineLifecycleDisabled, ConnectionStatus: commonModels.EngineConnectionOnline}, want: ErrUnavailable},
		{name: "missing", engine: nil, want: ErrUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := EnsureAvailable(tt.engine); !errors.Is(err, tt.want) {
				t.Fatalf("EnsureAvailable() error = %v, want %v", err, tt.want)
			}
		})
	}
}
