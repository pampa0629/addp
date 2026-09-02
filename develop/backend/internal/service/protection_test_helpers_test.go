package service

import (
	"context"

	"github.com/addp/common/engine/plugin"
)

type allowDevelopProtectionGate struct{}

func (allowDevelopProtectionGate) BeginPreparedQuery(context.Context, uint, plugin.EnginePlugin, plugin.PreparedQuery) (func(*plugin.QueryResult) error, func(), error) {
	return func(*plugin.QueryResult) error { return nil }, func() {}, nil
}

func (allowDevelopProtectionGate) BeginCatalogPath(context.Context, uint, plugin.EnginePlugin, plugin.EngineCatalogPath) (func(), error) {
	return func() {}, nil
}

func (allowDevelopProtectionGate) BeginUnresolvedRead(context.Context, uint) (func(), error) {
	return func() {}, nil
}
