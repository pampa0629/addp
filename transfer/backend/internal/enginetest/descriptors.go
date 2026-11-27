// Package enginetest provides engine fixtures for Transfer tests.
package enginetest

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"github.com/addp/transfer/internal/planner"
)

// Descriptors projects fixture bindings without exposing their connection details.
func Descriptors(bindings planner.StaticEngineResolver) func(context.Context, uint, uint) (*models.EngineRuntimeDescriptor, error) {
	return func(_ context.Context, _, id uint) (*models.EngineRuntimeDescriptor, error) {
		b, ok := bindings[id]
		if !ok {
			return nil, fmt.Errorf("engine %d not found", id)
		}
		d := &models.EngineRuntimeDescriptor{ID: id, EngineType: b.Type, LifecycleState: "active", ConnectionStatus: "online", Version: b.EngineVersion}
		if b.Capabilities != nil {
			caps, err := plugin.MarshalEngineCapabilities(*b.Capabilities)
			if err != nil {
				return nil, err
			}
			raw := models.JSONString(caps)
			d.Capabilities = &raw
		}
		return d, nil
	}
}
