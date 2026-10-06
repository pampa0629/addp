package preview

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
)

type profilePageProvider struct {
	recordingDatabasePreviewPlugin
	sets []*plugin.QueryReadSet
	errs []error
}

func (p *profilePageProvider) PrepareQuery(ctx context.Context, conn plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	index := len(p.prepareCalls)
	p.prepareCalls = append(p.prepareCalls, req)
	return plugin.NewPreparedQuery(&plugin.QueryAnalysis{Language: "sql", SchemaCoverage: plugin.QuerySchemaCoverageComplete}, func(context.Context) (*plugin.QueryReadSet, error) {
		if index < len(p.errs) && p.errs[index] != nil {
			return nil, p.errs[index]
		}
		return p.sets[index].Clone(), nil
	}, nil, func(context.Context) (*plugin.QueryResult, error) {
		p.executeCalls++
		return &plugin.QueryResult{}, nil
	})
}

func profilePagesTestRequest(t *testing.T) (*PreviewRequest, *profilePageProvider, []datatype.FieldInfo) {
	t.Helper()
	set, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(12, "schema", "public", "base"), plugin.TabularItemPath(12, "schema", "public", "view"))
	p := &profilePageProvider{recordingDatabasePreviewPlugin: recordingDatabasePreviewPlugin{engineType: "postgresql"}, sets: []*plugin.QueryReadSet{set, set}}
	req := &PreviewRequest{Engine: &models.Engine{ID: 12}, EnginePlugin: p,
		ProviderPath: plugin.TabularItemPath(12, "schema", "public", "view"),
		DataScope: dataprofile.DataScope{Kind: dataprofile.DataScopeKindCondition, Logic: dataprofile.DataScopeLogicAnd,
			Conditions: []dataprofile.DataScopeCondition{{Field: "status", Operator: "eq", Value: "active' OR true --"}}},
	}
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true}, {Name: "status", Type: datatype.FieldTypeString}}
	return req, p, fields
}

func TestProfilePagesPrepareAllSourcesWithoutReadingRows(t *testing.T) {
	req, provider, fields := profilePagesTestRequest(t)
	positions := []TablePage{{Offset: 0, Limit: 5}, {Offset: 100, Limit: 5}}
	plan, err := prepareProfilePages(t.Context(), req, fields, positions)
	if err != nil || plan == nil || len(provider.prepareCalls) != 2 || provider.executeCalls != 0 ||
		len(provider.describePaths) != 0 || len(provider.readBatchCalls) != 0 || len(provider.openSessionCalls) != 0 {
		t.Fatalf("preparation read business data or failed: %v", err)
	}
	if !reflect.DeepEqual(plan.ReadSet(), provider.sets[0]) {
		t.Fatal("view/base source closure missing")
	}
	for _, query := range provider.prepareCalls {
		if !query.Options.ReadOnly || strings.Contains(query.Query, "active'") || !reflect.DeepEqual(query.Options.Args, []any{"active' OR true --"}) {
			t.Fatal("conditions were interpolated or the query is not read-only")
		}
	}
	positions[0].Offset = 999
	plan.Positions()[1].Limit = 999
	plan.ReadSet().Paths[0].Segments[2].Name = "changed"
	if plan.Positions()[0].Offset != 0 || plan.Positions()[1].Limit != 5 || plan.ReadSet().Paths[0].Segments[2].Name != "base" {
		t.Fatal("plan facts alias caller memory")
	}
	query, err := plan.Query(0)
	if err != nil || query == nil {
		t.Fatal("prepared page was lost")
	}
	if _, err := query.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := query.Execute(t.Context()); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
		t.Fatal("same page could execute twice")
	}
	if _, err := plan.Query(2); err == nil {
		t.Fatal("unprepared page was returned")
	}
}

func TestProfilePagesRejectInvalidPositionsBeforePreparation(t *testing.T) {
	for _, pages := range [][]TablePage{
		nil, {{Offset: -1, Limit: 1}}, {{Offset: 0, Limit: 0}}, {{Offset: 0, Limit: 2001}},
		{{Offset: 0, Limit: 5}, {Offset: 4, Limit: 5}}, {{Offset: int(^uint(0) >> 1), Limit: 1}},
		{{Offset: 0, Limit: 2000}, {Offset: 2000, Limit: 2000}, {Offset: 4000, Limit: 2000}, {Offset: 6000, Limit: 2000}, {Offset: 8000, Limit: 2000}, {Offset: 10000, Limit: 1}},
	} {
		req, provider, fields := profilePagesTestRequest(t)
		if plan, err := prepareProfilePages(t.Context(), req, fields, pages); err == nil || plan != nil || len(provider.prepareCalls) != 0 {
			t.Fatalf("invalid page budget reached provider: %#v %v", pages, err)
		}
	}
}

func TestProfilePagesFailClosedWhenAnyDependencyIsUnproven(t *testing.T) {
	for _, change := range []func(*profilePageProvider){
		func(p *profilePageProvider) { p.errs = []error{nil, plugin.ErrQueryReadSetUnresolved} },
		func(p *profilePageProvider) { p.sets[1] = nil },
		func(p *profilePageProvider) { p.sets[1] = &plugin.QueryReadSet{} },
		func(p *profilePageProvider) {
			p.sets[1] = &plugin.QueryReadSet{Paths: append(p.sets[1].Paths, p.sets[1].Paths[0])}
		},
		func(p *profilePageProvider) {
			p.sets[1], _ = plugin.NewQueryReadSet(plugin.TabularItemPath(13, "schema", "public", "base"))
		},
	} {
		req, provider, fields := profilePagesTestRequest(t)
		change(provider)
		plan, err := prepareProfilePages(t.Context(), req, fields, []TablePage{{Offset: 0, Limit: 5}, {Offset: 5, Limit: 5}})
		if err == nil || plan != nil || provider.executeCalls != 0 {
			t.Fatal("incomplete source proof permitted content access")
		}
	}
}

func TestProfilePagesUnionAllActualSources(t *testing.T) {
	req, provider, fields := profilePagesTestRequest(t)
	provider.sets[1], _ = plugin.NewQueryReadSet(plugin.TabularItemPath(12, "schema", "public", "other"))
	plan, err := prepareProfilePages(t.Context(), req, fields, []TablePage{{Offset: 0, Limit: 5}, {Offset: 5, Limit: 5}})
	if err != nil || len(plan.ReadSet().Paths) != 3 || provider.executeCalls != 0 {
		t.Fatal("union omitted actual page dependencies")
	}
}

func TestProfilePagesRejectOversizedUnionBeforeContentRead(t *testing.T) {
	req, provider, fields := profilePagesTestRequest(t)
	for index := range provider.sets {
		var paths []plugin.EngineCatalogPath
		for source := 0; source < 101; source++ {
			paths = append(paths, plugin.TabularItemPath(12, "schema", "public", fmt.Sprintf("source_%d_%d", index, source)))
		}
		provider.sets[index], _ = plugin.NewQueryReadSet(paths...)
	}
	if plan, err := prepareProfilePages(t.Context(), req, fields, []TablePage{{Offset: 0, Limit: 1}, {Offset: 1, Limit: 1}}); err == nil || plan != nil || provider.executeCalls != 0 {
		t.Fatal("union budget did not reject before content reads")
	}
}
