package service

import (
	"context"
	"errors"
	"testing"

	commonapi "github.com/addp/common/api"
	plugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/models"
)

type independentEngineLookup struct {
	engine *models.Engine
	err    error
}

func (f independentEngineLookup) GetForConnection(uint, uint) (*models.Engine, error) {
	return f.engine, f.err
}

func TestIndependentReadTargetVerifiesExactLeafAndEngineState(t *testing.T) {
	for _, kind := range []string{"table", "collection"} {
		t.Run(kind, func(t *testing.T) {
			tenant := uint(7)
			path := plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 3, Segments: []plugin.EngineCatalogSegment{
				{Term: "server", Kind: "server"}, {Term: "database", Kind: "namespace", Name: "Outdoor"}, {Term: kind, Kind: kind, Name: "Persons"},
			}}
			for _, tc := range []struct {
				name                 string
				change               func(*models.Engine, *plugin.EngineCatalogEntry)
				sourceErr, errorWant error
			}{
				{name: "valid"},
				{name: "wrong tenant", change: func(e *models.Engine, _ *plugin.EngineCatalogEntry) { v := uint(8); e.TenantID = &v }, errorWant: commonapi.ErrForbidden},
				{name: "disabled", change: func(e *models.Engine, _ *plugin.EngineCatalogEntry) { e.LifecycleState = "disabled" }, errorWant: commonapi.ErrForbidden},
				{name: "wrong engine", change: func(e *models.Engine, _ *plugin.EngineCatalogEntry) { e.ID = 4 }, errorWant: commonapi.ErrForbidden},
				{name: "branch", change: func(_ *models.Engine, e *plugin.EngineCatalogEntry) { e.Role = "branch" }, errorWant: commonapi.ErrBadRequest},
				{name: "view", change: func(_ *models.Engine, e *plugin.EngineCatalogEntry) { e.Kind = "view" }, errorWant: commonapi.ErrBadRequest},
				{name: "path substitution", change: func(_ *models.Engine, e *plugin.EngineCatalogEntry) { e.Path.EngineID = 4 }, errorWant: commonapi.ErrBadRequest},
				{name: "provider failure", sourceErr: errors.New("source unavailable"), errorWant: engineaccess.ErrIndependentGrantSourceUnavailable},
			} {
				t.Run(tc.name, func(t *testing.T) {
					engine := &models.Engine{ID: 3, TenantID: &tenant, LifecycleState: models.EngineLifecycleActive, Version: 23}
					entry := &plugin.EngineCatalogEntry{Path: path, Term: kind, Kind: kind, Role: "leaf"}
					if tc.change != nil {
						tc.change(engine, entry)
					}
					calls := 0
					verifier := &IndependentGrantTargetVerifier{engines: independentEngineLookup{engine: engine}, resolve: func(_ context.Context, e *models.Engine, p plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
						calls++
						if e != engine || p.EngineID != path.EngineID {
							t.Fatal("incorrect resolution context")
						}
						return entry, tc.sourceErr
					}}
					version, err := verifier.VerifyIndependentReadTarget(context.Background(), int64(tenant), path)
					if !errors.Is(err, tc.errorWant) {
						t.Fatalf("version=%d err=%v want=%v", version, err, tc.errorWant)
					}
					if err == nil && (version != 23 || calls != 1) {
						t.Fatalf("version=%d calls=%d", version, calls)
					}
					if errors.Is(err, commonapi.ErrForbidden) && calls != 0 {
						t.Fatal("source queried before engine ownership/lifecycle validation")
					}
				})
			}
		})
	}
}
