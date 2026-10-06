package service

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/preview"
)

type executableProfilePagesForTest struct {
	profilePreparedPagesForTest
	queries []plugin.PreparedQuery
}

func (p *executableProfilePagesForTest) Query(i int) (plugin.PreparedQuery, error) {
	if i < 0 || i >= len(p.queries) {
		return nil, ErrDataProfileSourceAuthorizationRequired
	}
	return p.queries[i], nil
}

func executableProfilePlanForTest(t *testing.T) (*PreviewDataProfileSampleProvider, *DataProfileTarget, *DataProfileSamplePlan, DataProfileBudget, *int) {
	t.Helper()
	budget := DataProfileBudget{SampleSize: 2, MaxRowsScanned: 6, PageSize: 2, Timeout: time.Second}
	count := int64(6)
	target := &DataProfileTarget{EngineID: 1, RowCount: &count, RowCountExact: true,
		Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true}}, resolved: &preview.PreviewResolverRequest{}}
	positions, err := dataProfilePagePositions(target.RowCount, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, budget)
	if err != nil {
		t.Fatal(err)
	}
	set, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(1, "schema", "public", "base"))
	pages := &executableProfilePagesForTest{profilePreparedPagesForTest: profilePreparedPagesForTest{set: set, positions: positions}}
	calls := new(int)
	for _, page := range positions {
		query, err := plugin.NewPreparedQuery(&plugin.QueryAnalysis{Language: "sql", SchemaCoverage: plugin.QuerySchemaCoverageComplete},
			func(context.Context) (*plugin.QueryReadSet, error) { return set.Clone(), nil }, nil,
			func(context.Context) (*plugin.QueryResult, error) {
				*calls++
				return &plugin.QueryResult{Columns: []string{"id"}, Rows: []map[string]interface{}{{"id": page.Offset}, {"id": page.Offset + 1}}}, nil
			})
		if err != nil {
			t.Fatal(err)
		}
		pages.queries = append(pages.queries, query)
	}
	return NewPreviewDataProfileSampleProvider(&preview.PreviewResolver{}, nil), target,
		&DataProfileSamplePlan{pages: pages, model: plugin.TabularCatalogModel("schema")}, budget, calls
}

func TestProfileProductionSamplingUsesSamePreparedPagesAfterEachGate(t *testing.T) {
	sampler, target, plan, budget, calls := executableProfilePlanForTest(t)
	checks := 0
	result, err := sampler.Sample(t.Context(), target, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, budget, plan, func(context.Context) error {
		checks++
		return nil
	})
	if err != nil || result == nil || checks != 3 || *calls != 3 || result.RowsScanned != 6 || len(result.Rows) != 2 ||
		!reflect.DeepEqual(result.ReadSet, plan.ReadSet()) || !result.RowCountExact || result.Partial || result.Truncated {
		t.Fatalf("same-plan sampling failed: checks=%d calls=%d result=%+v err=%v", checks, *calls, result, err)
	}
	query, _ := plan.pages.Query(0)
	if _, err := query.Execute(t.Context()); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
		t.Fatal("sampler did not consume the original one-shot query")
	}
}

func TestProfileProductionSamplingStopsBeforeDeniedPage(t *testing.T) {
	for _, denied := range []int{1, 2, 3} {
		t.Run(strconv.Itoa(denied), func(t *testing.T) {
			sampler, target, plan, budget, calls := executableProfilePlanForTest(t)
			checks := 0
			result, err := sampler.Sample(t.Context(), target, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, budget, plan, func(context.Context) error {
				checks++
				if checks == denied {
					return ErrDataProfileSourceAuthorizationRequired
				}
				return nil
			})
			if result != nil || !errors.Is(err, ErrDataProfileSourceAuthorizationRequired) || checks != denied || *calls != denied-1 {
				t.Fatal("denied page executed or partial result escaped")
			}
		})
	}
}

func TestProfileProductionSamplingRejectsUnsupportedSources(t *testing.T) {
	for _, kind := range []string{"view", "materialized_view", "external_table", "multiple"} {
		t.Run(kind, func(t *testing.T) {
			sampler, target, plan, budget, calls := executableProfilePlanForTest(t)
			pages := plan.pages.(*executableProfilePagesForTest)
			if kind == "multiple" {
				pages.set, _ = plugin.NewQueryReadSet(pages.set.Paths[0], plugin.TabularItemPath(1, "schema", "public", "other"))
			} else {
				pages.set.Paths[0].Segments[len(pages.set.Paths[0].Segments)-1].Kind = kind
			}
			checks := 0
			result, err := sampler.Sample(t.Context(), target, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, budget, plan, func(context.Context) error { checks++; return nil })
			if result != nil || !errors.Is(err, ErrDataProfileUnsupported) || checks != 0 || *calls != 0 {
				t.Fatal("unsupported source read business content")
			}
		})
	}
}

func TestProfileProductionSamplingRejectsResultStructureAndBudgetChanges(t *testing.T) {
	for _, name := range []string{"nil", "columns", "row_keys", "row_limit", "positions"} {
		t.Run(name, func(t *testing.T) {
			sampler, target, plan, budget, calls := executableProfilePlanForTest(t)
			pages := plan.pages.(*executableProfilePagesForTest)
			if name == "positions" {
				pages.positions[0].Limit++
			} else {
				query, err := plugin.NewPreparedQuery(&plugin.QueryAnalysis{Language: "sql", SchemaCoverage: plugin.QuerySchemaCoverageComplete},
					func(context.Context) (*plugin.QueryReadSet, error) { return pages.set.Clone(), nil }, nil,
					func(context.Context) (*plugin.QueryResult, error) {
						*calls++
						result := &plugin.QueryResult{Columns: []string{"id"}, Rows: []map[string]interface{}{{"id": 1}}}
						switch name {
						case "nil":
							return nil, nil
						case "columns":
							result.Columns = []string{"other"}
						case "row_keys":
							result.Rows[0] = map[string]interface{}{"other": 1}
						case "row_limit":
							result.Rows = append(result.Rows, result.Rows[0], result.Rows[0])
						}
						return result, nil
					})
				if err != nil {
					t.Fatal(err)
				}
				pages.queries[0] = query
			}
			result, err := sampler.Sample(t.Context(), target, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, budget, plan, func(context.Context) error { return nil })
			if result != nil || !errors.Is(err, ErrDataProfileSourceChanged) || *calls > 1 {
				t.Fatal("changed result or page budget accepted")
			}
		})
	}
}

func TestProfileWorkerRechecksSourceBeforeCommitAndKeepsPreviousResult(t *testing.T) {
	svc, sampler, executions := queuedProfileForTest(t, DataProfileBudget{SampleSize: 2, MaxRowsScanned: 4, PageSize: 2, Timeout: time.Second})
	previous := &dataprofile.Profile{Mode: dataprofile.ModeSample}
	store := svc.profiles.(*dataProfileServiceTestProfileStore)
	store.profile = previous
	consumer := svc.authorizationConsumer.(*profileConsumerForTest)
	consumer.onCheck = func() {
		if consumer.calls == 4 {
			consumer.err = &commonClient.SystemAPIError{StatusCode: 403}
		}
	}
	if err := svc.runClaimedExecution(profileClaimContextForTest(t, executions.createdExecution), executions.createdExecution); err != nil {
		t.Fatal(err)
	}
	if consumer.calls != 4 || sampler.sampleCalls != 1 || executions.completed || store.replaceCalls != 0 || store.profile != previous || executions.failedCode != "source_authorization_required" {
		t.Fatal("revocation after sampling committed a new result")
	}
}

func TestProfileWorkerStopsOnEachPageAuthorizationRevocation(t *testing.T) {
	for _, page := range []int{1, 2, 3} {
		t.Run(string(rune('0'+page)), func(t *testing.T) {
			svc, sampler, executions := queuedProfileForTest(t, DataProfileBudget{SampleSize: 2, MaxRowsScanned: 6, PageSize: 2, Timeout: time.Second})
			store := svc.profiles.(*dataProfileServiceTestProfileStore)
			previous := &dataprofile.Profile{Mode: dataprofile.ModeSample}
			store.profile = previous
			consumer := svc.authorizationConsumer.(*profileConsumerForTest)
			consumer.onCheck = func() {
				if consumer.calls == page+1 {
					consumer.err = &commonClient.SystemAPIError{StatusCode: 403}
				}
			}
			if err := svc.runClaimedExecution(profileClaimContextForTest(t, executions.createdExecution), executions.createdExecution); err != nil {
				t.Fatal(err)
			}
			if consumer.calls != page+1 || sampler.sampleCalls != 1 || executions.completed || store.replaceCalls != 0 || store.profile != previous || executions.failedCode != "source_authorization_required" {
				t.Fatal("page revocation was ignored or replaced the previous result")
			}
		})
	}
}

func TestProfileWorkerCannotIgnoreProtectedUnderlyingSource(t *testing.T) {
	svc, sampler, executions := queuedProfileForTest(t, DefaultDataProfileBudget)
	set, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(1, "schema", "public", "orders"))
	target, err := dataprotection.DataItemTargetFromCatalogPath(plugin.TabularCatalogModel("schema"), set.Paths[0])
	if err != nil {
		t.Fatal(err)
	}
	fields := []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString}}
	svc.protectionGate = managedDataProfileServiceTestGate(t, target.ResourceIdentity, fields, dataprotection.EffectSuppress)
	if err := svc.runClaimedExecution(profileClaimContextForTest(t, executions.createdExecution), executions.createdExecution); err != nil {
		t.Fatal(err)
	}
	if sampler.sampleCalls != 0 || executions.completed || executions.failedCode != "security_protection_required" {
		t.Fatal("protected dependency was ignored")
	}
}
