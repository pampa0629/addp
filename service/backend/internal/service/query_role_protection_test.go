package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
)

type queryRoleTestGate struct {
	allowServiceProtectionGate
	deny        bool
	validations int
	roles       []string
}

func (g *queryRoleTestGate) RoleOptions(context.Context, int64, dataprotection.ResourceReference, []string) ([]dataprotection.FieldRoleOptions, error) {
	return []dataprotection.FieldRoleOptions{}, nil
}
func (g *queryRoleTestGate) ValidateRoleChoices(_ context.Context, _ int64, s *dataprotection.QueryRoleSelection) error {
	g.validations++
	g.roles = append([]string(nil), s.RoleKeys...)
	if g.deny {
		return dataprotection.ErrRoleOptionUnavailable
	}
	return nil
}

func roleTestService() *models.QueryService {
	s := consumerDescriptorTestService()
	s.ConfigType = "table"
	snapshot := s.SourceSnapshot()
	snapshot.Source = &models.QueryServiceSourceRef{ItemID: 33, ItemFingerprint: "role-test-source"}
	snapshot.Table = &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt, Nullable: false}, {Name: "phone", Type: datatype.FieldTypeString, Nullable: true}}, PrimaryKey: []string{"id"}}
	snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
	s.DataConfig[models.QueryServiceSourceSnapshotKey] = queryServiceSnapshotPayload(snapshot)
	s.DataConfig[models.QueryProtectionChoicesKey] = []dataprotection.FieldRoleChoice{{Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "role-test-source"}, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "policy", PolicyRevision: 1, AssessmentRevision: 1, OptionID: "option"}}}
	s.DataConfig["default_fields"] = []string{"id", "phone"}
	s.DataConfig["filterable_fields"] = []string{"id"}
	s.DataConfig["stable_key"] = []string{"id"}
	return s
}

func TestServiceChoiceSaveUsesCurrentUserAndRechecksRetainedChoices(t *testing.T) {
	item := roleTestService()
	item.ID = 0
	item.Status = "inactive"
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"service.definition.update"})
	item.TenantID = 7
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		if r.URL.Path != "/api/v1/system/auth/subject-roles" || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer addp_at_current" || r.URL.RawQuery != "" {
			t.Error("role check changed current identity or added selectors")
		}
		json.NewEncoder(w).Encode(authorization.CurrentSubjectRoles{TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: *facts.Context.TenantMembershipID, AuthorizationVersion: facts.Authorization.AuthorizationVersion, RoleKeys: []string{"custom.analytics"}, ObservedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Minute)})
	}))
	defer server.Close()
	gate := &queryRoleTestGate{}
	executor := NewQueryExecutorService(nil, client.NewSystemServiceClient(server.URL, nil, server.Client()), nil)
	executor.SetProtectionGate(gate)
	db := openQueryServiceCreateTestDB(t)
	if err := db.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewQueryServiceRepository(db)
	owner := NewQueryServiceService(repo, nil, nil, "")
	owner.SetRoleProtection(executor)
	ctx := WithQueryProtectionUser(t.Context(), facts, "addp_at_current")
	title := "permitted"
	dto, err := owner.UpdateService(ctx, item.ID, 7, &models.UpdateQueryServiceRequest{Version: 1, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if dto.Version != 2 || reads != 1 || gate.validations != 1 || len(gate.roles) != 1 || gate.roles[0] != "custom.analytics" {
		t.Fatal("current pure-business role was not checked")
	}
	gate.deny = true
	title = "must-not-save"
	if _, err := owner.UpdateService(ctx, item.ID, 7, &models.UpdateQueryServiceRequest{Version: 2, Title: &title}); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
		t.Fatal(err)
	}
	stored, err := repo.GetByIDAndTenant(item.ID, 7)
	if err != nil || stored.Title != "permitted" || stored.Version != 2 {
		t.Fatal("failed check changed definition", err)
	}
	if _, err := owner.RebindMetricSource(ctx, item.ID, 7, &models.RebindMetricSourceRequest{Version: 2, MetricSource: &models.MetricSourceRequest{ImplementationID: 3, RevisionID: 4}}); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
		t.Fatal("source switch silently discarded saved choices", err)
	}
	before := reads
	if _, err := owner.UpdateService(ctx, item.ID, 7, &models.UpdateQueryServiceRequest{Version: 2, DataConfig: map[string]any{models.QueryProtectionChoicesKey: []any{}}}); err != nil {
		t.Fatal(err)
	}
	if reads != before {
		t.Fatal("default restore requires roles")
	}
}

func TestServiceOnlyDeactivationPreservesUnavailableChoiceAndRequiresFreshVersion(t *testing.T) {
	item := roleTestService()
	item.ID, item.TenantID, item.Status = 0, 7, "active"
	db := openQueryServiceCreateTestDB(t)
	if err := db.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewQueryServiceRepository(db)
	owner := NewQueryServiceService(repo, nil, nil, "")
	// No role client or gate is available. Stopping must not depend on either.
	before, err := json.Marshal(item.DataConfig)
	if err != nil {
		t.Fatal(err)
	}
	inactive, active, title := "inactive", "active", "changed"
	dto, err := owner.UpdateService(t.Context(), item.ID, 7, &models.UpdateQueryServiceRequest{Version: 1, Status: &inactive})
	if err != nil || dto.Status != "inactive" || dto.Version != 2 {
		t.Fatalf("stop failed: dto=%+v err=%v", dto, err)
	}
	stored, err := repo.GetByIDAndTenant(item.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(stored.DataConfig)
	if err != nil || string(before) != string(after) {
		t.Fatal("stopping changed saved config or choices", err)
	}
	for name, req := range map[string]*models.UpdateQueryServiceRequest{
		"reactivate":            {Version: 2, Status: &active},
		"stop and rename":       {Version: 2, Status: &inactive, Title: &title},
		"stop and empty config": {Version: 2, Status: &inactive, DataConfig: map[string]any{}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := owner.UpdateService(t.Context(), item.ID, 7, req); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
				t.Fatal("non-stop update bypassed qualification", err)
			}
		})
	}
	if _, err := owner.UpdateService(t.Context(), item.ID, 7, &models.UpdateQueryServiceRequest{Version: 1, Status: &inactive}); err == nil {
		t.Fatal("stopping bypassed version conflict")
	}
	stored, err = repo.GetByIDAndTenant(item.ID, 7)
	if err != nil || stored.Status != "inactive" || stored.Version != 2 || stored.Title != item.Title {
		t.Fatal("rejected update changed definition", err)
	}
	if _, err := owner.UpdateService(t.Context(), item.ID, 7, &models.UpdateQueryServiceRequest{Version: 2, DataConfig: map[string]any{models.QueryProtectionChoicesKey: []any{}}}); err != nil {
		t.Fatal("explicit default restore failed", err)
	}
}

func TestOnlyDeactivationRejectsEveryAccompanyingUpdateField(t *testing.T) {
	inactive, empty, enabled, count := "inactive", "", false, 5
	requests := []*models.UpdateQueryServiceRequest{
		{Status: &inactive, Title: &empty},
		{Status: &inactive, Description: &empty},
		{Status: &inactive, Keywords: []string{}},
		{Status: &inactive, DataConfig: map[string]any{}},
		{Status: &inactive, Protocols: map[string]any{}},
		{Status: &inactive, PublicAccess: &enabled},
		{Status: &inactive, MaxFeatures: &count},
		{}, nil,
	}
	for i, req := range requests {
		if onlyDeactivatesQueryService(req) {
			t.Fatalf("request %d bypasses current qualification", i)
		}
	}
}

func TestServiceChoiceRejectsUnboundIdentityAndUnsupportedSources(t *testing.T) {
	service := roleTestService()
	service.TenantID = 7
	executor := NewQueryExecutorService(nil, nil, nil)
	for name, ctx := range map[string]context.Context{"anonymous": t.Context(), "other tenant": WithQueryProtectionUser(t.Context(), authtest.NewTenantUserAuthContext("8", "9", nil), "addp_at_current")} {
		t.Run(name, func(t *testing.T) {
			if _, err := executor.serviceRoleSelection(ctx, service); !errors.Is(err, dataprotection.ErrRoleOptionIneligible) {
				t.Fatal(err)
			}
		})
	}
	for _, kind := range []string{"sql", "analytical"} {
		q := *service
		q.ConfigType = kind
		if _, err := executor.serviceRoleSelection(t.Context(), &q); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
			t.Fatal(kind, err)
		}
	}
	q := *service
	runtime := uint(11)
	q.RuntimeEngineID = &runtime
	if _, err := executor.serviceRoleSelection(t.Context(), &q); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
		t.Fatal(err)
	}
	service.DataConfig[models.QueryProtectionChoicesKey] = []any{}
	if selected, err := executor.serviceRoleSelection(t.Context(), service); err != nil || selected != nil {
		t.Fatal("default path gained identity dependency", err)
	}
}

func TestServiceChoiceChangesConsumerVersionAndEmptyRestoresDefault(t *testing.T) {
	service := roleTestService()
	selected := QueryServiceVersion(service)
	choices, err := service.ProtectionChoices()
	if err != nil {
		t.Fatal(err)
	}
	second := choices[0]
	second.ComponentKey = "id"
	service.DataConfig[models.QueryProtectionChoicesKey] = append(choices, second)
	pair := QueryServiceVersion(service)
	service.DataConfig[models.QueryProtectionChoicesKey] = []dataprotection.FieldRoleChoice{second, choices[0]}
	if QueryServiceVersion(service) != pair {
		t.Fatal("choice order changed the consumption contract")
	}
	delete(service.DataConfig, models.QueryProtectionChoicesKey)
	baseline := QueryServiceVersion(service)
	service.DataConfig[models.QueryProtectionChoicesKey] = []any{}
	if selected == baseline || baseline == "" || QueryServiceVersion(service) != baseline {
		t.Fatal("choice not included in consumer version, or empty choice changes default")
	}
}

type roleExecutionPlugin struct {
	querySampleSQLPlugin
	runs      int
	onExecute func()
}

func (*roleExecutionPlugin) Type() string { return "service_role_execution_test" }
func (p *roleExecutionPlugin) PrepareQuery(context.Context, plugin.ConnectionInfo, plugin.QueryRequest) (plugin.PreparedQuery, error) {
	return plugin.NewPreparedQuery(&plugin.QueryAnalysis{Language: "sql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil, nil, nil, func(context.Context) (*plugin.QueryResult, error) {
		p.runs++
		if p.onExecute != nil {
			p.onExecute()
		}
		return &plugin.QueryResult{Columns: []string{"id", "phone"}, Rows: []map[string]any{{"id": int64(1), "phone": "13661384499"}}}, nil
	})
}

type roleExecutionGate struct {
	queryRoleTestGate
	applied  int
	selected bool
}

func (g *roleExecutionGate) BeginPreparedQuery(_ context.Context, _ uint, _ plugin.EnginePlugin, _ plugin.PreparedQuery, selection *dataprotection.QueryRoleSelection) (func(*plugin.QueryResult) error, func(), error) {
	g.selected = selection != nil
	return func(result *plugin.QueryResult) error {
		g.applied++
		for _, row := range result.Rows {
			if g.selected {
				row["phone"] = "***********"
			} else {
				row["phone"] = "136****4499"
			}
		}
		return nil
	}, func() {}, nil
}

func TestServiceExecutionRechecksActualCallingUserBeforeDeliveryAndDefaultHasNoRoleDependency(t *testing.T) {
	p := &roleExecutionPlugin{}
	plugin.Register(p)
	t.Cleanup(func() { plugin.Unregister(p.Type()) })
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"service.data_read.execute"})
	reads := 0
	eligible := true
	engineID := uint(99107)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/auth/subject-roles" {
			reads++
			if r.Header.Get("Authorization") != "Bearer addp_at_current" {
				t.Error("publisher or machine credentials were used")
			}
			if !eligible {
				http.Error(w, "revoked", 401)
				return
			}
			json.NewEncoder(w).Encode(authorization.CurrentSubjectRoles{TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: *facts.Context.TenantMembershipID, AuthorizationVersion: facts.Authorization.AuthorizationVersion, RoleKeys: []string{"custom.analytics"}, ObservedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Minute)})
			return
		}
		json.NewEncoder(w).Encode(commonmodels.Engine{ID: engineID, EngineType: p.Type()})
	}))
	defer server.Close()
	tokens := client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "addp_at_owner", nil })
	executor := NewQueryExecutorService(client.NewSystemClient(server.URL, tokens), client.NewSystemServiceClient(server.URL, nil, server.Client()), []byte("0123456789abcdef0123456789abcdef"))
	gate := &roleExecutionGate{}
	executor.SetProtectionGate(gate)
	service := roleTestService()
	service.TenantID = 7
	service.EngineID = &engineID
	service.SchemaName = "public"
	service.TargetTable = "persons"
	ctx := WithQueryProtectionUser(t.Context(), facts, "addp_at_current")
	request := &models.QueryExecutionRequest{Select: []string{"id", "phone"}, Page: models.QueryPageRequest{Limit: 5}}
	result, err := executor.ExecuteQuery(ctx, service, request)
	if err != nil || result == nil || result.Data[0]["phone"] != "***********" || reads != 2 || gate.applied != 1 || !gate.selected {
		t.Fatalf("selected delivery was not checked twice: %v, reads=%d", err, reads)
	}
	csv, err := executor.FormatAsCSV(result)
	if err != nil || !strings.Contains(string(csv), "***********") || strings.Contains(string(csv), "13661384499") {
		t.Fatal("CSV bypassed protected output", err)
	}
	p.onExecute = func() { eligible = false }
	before := gate.applied
	if result, err := executor.ExecuteQuery(ctx, service, request); result != nil || !errors.Is(err, dataprotection.ErrRoleOptionIneligible) || gate.applied != before {
		t.Fatal("role revoked during query reached delivery", err)
	}
	p.onExecute = nil
	service.DataConfig[models.QueryProtectionChoicesKey] = []any{}
	before = reads
	if result, err := executor.ExecuteQuery(t.Context(), service, request); err != nil || result.Data[0]["phone"] != "136****4499" || reads != before || gate.selected {
		t.Fatal("default query acquired a role dependency", err)
	}
}
