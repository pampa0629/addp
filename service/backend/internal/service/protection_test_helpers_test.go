package service

import (
	"context"

	"github.com/addp/common/engine/plugin"
)

type allowServiceProtectionGate struct{}

func (allowServiceProtectionGate) BeginPreparedQuery(context.Context, uint, plugin.EnginePlugin, plugin.PreparedQuery) (func(*plugin.QueryResult) error, func(), error) {
	return func(*plugin.QueryResult) error { return nil }, func() {}, nil
}

func (allowServiceProtectionGate) BeginCatalogPath(context.Context, uint, plugin.EnginePlugin, plugin.EngineCatalogPath) (func(), error) {
	return func() {}, nil
}

func (allowServiceProtectionGate) BeginUnresolvedRead(context.Context, uint) (func(), error) {
	return func() {}, nil
}
