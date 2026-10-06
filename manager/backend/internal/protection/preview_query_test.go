package protection

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
)

type previewTestPlan struct {
	set        *plugin.QueryReadSet
	readErr    error
	executions int
}

func (*previewTestPlan) Analysis(context.Context) (*plugin.QueryAnalysis, error) { return nil, nil }
func (p *previewTestPlan) ReadSet(context.Context) (*plugin.QueryReadSet, error) {
	return p.set, p.readErr
}
func (*previewTestPlan) OutputLineage(context.Context) (*plugin.QueryOutputLineage, error) {
	return nil, nil
}
func (p *previewTestPlan) Execute(context.Context) (*plugin.QueryResult, error) {
	p.executions++
	return &plugin.QueryResult{Columns: []string{"email"}, Rows: []map[string]interface{}{{"email": "private"}}}, nil
}

type previewTestProtection struct {
	t             *testing.T
	plan          plugin.PreparedQuery
	subject       dataprotection.SubjectReference
	beforeExecute func()
	calls         int
	err           error
}

func (s *previewTestProtection) PrepareQueryProtection(_ context.Context, tenant int64, model plugin.EngineCatalogModelSpec, plan plugin.PreparedQuery, action string, subject dataprotection.SubjectReference, now time.Time) (*dataprotection.PreparedTableProtection, error) {
	s.calls++
	if tenant != 7 || plan != s.plan || subject != s.subject || action != ActionPreview || now.IsZero() || !reflect.DeepEqual(model, plugin.TabularCatalogModel("schema")) {
		s.t.Fatal("protection did not receive current subject and the authorized plan")
	}
	if s.beforeExecute != nil {
		s.beforeExecute()
	}
	if s.err != nil {
		return nil, s.err
	}
	return &dataprotection.PreparedTableProtection{Apply: func(result *plugin.QueryResult) error {
		for _, row := range result.Rows {
			row["email"] = "masked"
		}
		return nil
	}}, nil
}

func TestPreviewQueryExecutorUsesCurrentCredentialAndSameCompletePlan(t *testing.T) {
	for _, token := range []string{"addp_at_current_user", "addp_dat_current_tool"} {
		t.Run(token, func(t *testing.T) {
			set, err := plugin.NewQueryReadSet(plugin.TabularItemPath(12, "schema", "public", "view"), plugin.TabularItemPath(12, "schema", "public", "base"))
			if err != nil {
				t.Fatal(err)
			}
			plan := &previewTestPlan{set: set}
			checks := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				checks++
				if r.URL.Path != "/api/v1/system/engine-access/read-checks/manager-preview" || r.Header.Get("Authorization") != "Bearer "+token || plan.executions != 0 {
					t.Error("wrong credential, endpoint or query ordering")
				}
				var body struct {
					Targets []plugin.EngineCatalogPath `json:"targets"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !reflect.DeepEqual(body.Targets, set.Paths) {
					t.Error("incomplete read set")
				}
				_, _ = w.Write([]byte(`{"observed_at":"2026-10-05T12:00:00Z"}`))
			}))
			defer server.Close()
			subject := dataprotection.SubjectReference{Type: "user", ID: "41"}
			store := &previewTestProtection{t: t, plan: plan, subject: subject, beforeExecute: func() {
				if checks != 1 || plan.executions != 0 {
					t.Fatal("query ran before both gates")
				}
			}}
			execute := PreviewQueryExecutor(client.NewSystemServiceClient(server.URL, nil, server.Client()), store, 7, 12, token, subject)
			result, protection, err := execute(context.Background(), &postgresql.PostgreSQLPlugin{}, plan)
			if err != nil || checks != 1 || store.calls != 1 || plan.executions != 1 || protection == nil {
				t.Fatalf("execution: %v", err)
			}
			if result.Rows[0]["email"] != "private" {
				t.Fatal("provider result was transformed before response boundary")
			}
			if err := protection.Apply(result); err != nil || result.Rows[0]["email"] != "masked" {
				t.Fatal("prepared protection not bound to result")
			}
		})
	}
}

func TestPreviewQueryExecutorRejectsBeforeSourceRead(t *testing.T) {
	base := plugin.TabularItemPath(12, "schema", "public", "base")
	valid, _ := plugin.NewQueryReadSet(base)
	for _, test := range []struct {
		name                string
		set                 *plugin.QueryReadSet
		readErr             error
		status              int
		protectionErr       error
		want                error
		checks, protections int
	}{
		{"no read set", nil, nil, 200, nil, ErrRequired, 0, 0},
		{"incomplete proof", valid, plugin.ErrQueryReadSetUnresolved, 200, nil, ErrRequired, 0, 0},
		{"empty proof", &plugin.QueryReadSet{}, nil, 200, nil, ErrRequired, 0, 0},
		{"duplicate proof", &plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{base, base}}, nil, 200, nil, ErrRequired, 0, 0},
		{"other engine", &plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{plugin.TabularItemPath(13, "schema", "public", "base")}}, nil, 200, nil, ErrRequired, 0, 0},
		{"invalid credential", valid, nil, 401, nil, client.ErrManagerPreviewCredentialRejected, 1, 0},
		{"denied source", valid, nil, 403, nil, client.ErrManagerPreviewReadDenied, 1, 0},
		{"System unavailable", valid, nil, 503, nil, client.ErrManagerPreviewReadUnavailable, 1, 0},
		{"protection unavailable", valid, nil, 200, errors.New("projection unavailable"), ErrRequired, 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := &previewTestPlan{set: test.set, readErr: test.readErr}
			checks := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				checks++
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"observed_at":"2026-10-05T12:00:00Z"}`))
			}))
			defer server.Close()
			store := &previewTestProtection{t: t, plan: plan, err: test.protectionErr}
			execute := PreviewQueryExecutor(client.NewSystemServiceClient(server.URL, nil, server.Client()), store, 7, 12, "addp_at_current_user", dataprotection.SubjectReference{})
			result, protection, err := execute(context.Background(), &postgresql.PostgreSQLPlugin{}, plan)
			if !errors.Is(err, test.want) || result != nil || protection != nil || plan.executions != 0 || checks != test.checks || store.calls != test.protections {
				t.Fatalf("fail-open: err=%v reads=%d checks=%d protections=%d", err, plan.executions, checks, store.calls)
			}
		})
	}
}
