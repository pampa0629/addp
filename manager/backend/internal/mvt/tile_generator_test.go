package mvt

import (
	"context"
	"errors"
	"testing"

	commonModels "github.com/addp/common/models"
)

type tenantAwareEngineLookup struct {
	tenantID uint
	engineID uint
	err      error
}

func (l *tenantAwareEngineLookup) GetEngineForTenant(_ context.Context, tenantID, engineID uint) (*commonModels.Engine, error) {
	l.tenantID = tenantID
	l.engineID = engineID
	return nil, l.err
}

func TestTileGeneratorResolvesEngineWithinTenantContext(t *testing.T) {
	wantErr := errors.New("stop after tenant-aware lookup")
	lookup := &tenantAwareEngineLookup{err: wantErr}
	generator := NewTileGenerator(lookup, 1)

	_, err := generator.GetOrCreateDBPool(context.Background(), 11, 7)
	if !errors.Is(err, wantErr) {
		t.Fatalf("GetOrCreateDBPool() error = %v, want %v", err, wantErr)
	}
	if lookup.tenantID != 7 || lookup.engineID != 11 {
		t.Fatalf("engine lookup tenant=%d engine=%d, want tenant=7 engine=11", lookup.tenantID, lookup.engineID)
	}
}

func TestMVTBufferForExtent(t *testing.T) {
	tests := []struct {
		name   string
		extent int
		want   int
	}{
		{name: "default", extent: 1024, want: 32},
		{name: "half", extent: 512, want: 16},
		{name: "minimum", extent: 256, want: 8},
		{name: "lower bound", extent: 128, want: 8},
		{name: "upper bound", extent: 4096, want: 64},
		{name: "invalid", extent: 0, want: 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mvtBufferForExtent(tt.extent); got != tt.want {
				t.Fatalf("mvtBufferForExtent(%d) = %d, want %d", tt.extent, got, tt.want)
			}
		})
	}
}
