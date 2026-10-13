package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/builtin/general"
	commonmodels "github.com/addp/common/models"
)

type configFactsStub struct {
	engines            map[uint]*commonmodels.Engine
	items              map[uint]*commonmodels.MetaItem
	calls              int
	engineErr, metaErr error
}

func (f *configFactsStub) GetEngineForTenant(_ context.Context, tenantID, engineID uint) (*commonmodels.Engine, error) {
	f.calls++
	if tenantID != 7 {
		panic("wrong tenant")
	}
	return f.engines[engineID], f.engineErr
}
func (f *configFactsStub) GetItemSnapshot(_ context.Context, tenantID, engineID, itemID uint, path string) (*commonmodels.MetaItem, error) {
	f.calls++
	if tenantID != 7 || (engineID == 1 && path != "synthetic.source") || (engineID == 2 && path != "synthetic.target") {
		panic("wrong snapshot binding")
	}
	if f.metaErr != nil {
		return nil, f.metaErr
	}
	if f.items[engineID] == nil {
		return nil, commonapi.ErrNotFound
	}
	return f.items[engineID], nil
}
func configCheckFixture(t *testing.T) (*TaskConfigCheckService, *configFactsStub, authorization.AuthContext, []byte) {
	t.Helper()
	tenant := uint(7)
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	registered, _ := plugin.Get("postgresql")
	caps, _ := plugin.MarshalEngineCapabilities(registered.Capabilities())
	declared := commonmodels.JSONString(caps)
	f := &configFactsStub{engines: map[uint]*commonmodels.Engine{}, items: map[uint]*commonmodels.MetaItem{}}
	for id, name := range map[uint]string{1: "source", 2: "target"} {
		f.engines[id] = &commonmodels.Engine{ID: id, TenantID: &tenant, EngineType: "postgresql", Version: 1, LifecycleState: "active", Capabilities: &declared, ConnectionInfo: commonmodels.ConnectionInfo{"password": "never-return-secret"}}
		f.items[id] = &commonmodels.MetaItem{ID: id + 10, TenantID: 7, EngineID: id, ItemType: "table", Name: name, FullName: "synthetic." + name, ScannedAt: &now, ScannedDepth: "basic", Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{"table": datatype.TableInfoPayload(&datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "payload", Type: datatype.FieldTypeJSON, NativeType: "jsonb", Nullable: true}}})},
		}}
	}
	subject := authtest.NewTenantUserAuthContext("7", "91", configCheckPermissions)
	body := []byte(`{"name":"synthetic check","task_type":"sync","enabled":false,"auto_scan_metadata":false,"config":{"runtime":{"boundary":"bounded"},"load":{"mode":"snapshot"},"source":{"locator":"addp://engine/1/path/synthetic/source?type=table","data_type":"table","representation":"native"},"target":{"parent_locator":"addp://engine/2/path/synthetic?type=schema","name":"target","data_type":"table","representation":"native","policy":{"apply_mode":"replace"}},"transforms":[{"type":"field_mapping","version":"v1","mode":"project","fields":[{"source":"payload","target":"payload","target_type":"json","nullable":true}]}]}}`)
	return NewTaskConfigCheckService(f, f), f, subject, body
}
func checkFixtureReport(t *testing.T, svc *TaskConfigCheckService, subject authorization.AuthContext, body []byte) *TaskConfigCheckReport {
	t.Helper()
	result, err := svc.Check(context.Background(), subject, body)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestConfigCheckUsesNonSpatialProtocolProvider(t *testing.T) {
	for _, engineType := range []string{"opengauss", "kingbase", "tidb", "oceanbase"} {
		for _, scenario := range []string{"json", "invalid_constant", "object_array", "unknown_source_with_invalid_constant", "geometry", "append_json", "append_text", "source_text"} {
			t.Run(engineType+"/"+scenario, func(t *testing.T) {
				svc, facts, subject, body := configCheckFixture(t)
				registered, err := plugin.Get(engineType)
				if err != nil {
					t.Fatal(err)
				}
				caps, err := plugin.MarshalEngineCapabilities(registered.Capabilities())
				if err != nil {
					t.Fatal(err)
				}
				declared := commonmodels.JSONString(caps)
				facts.engines[2].EngineType = engineType
				facts.engines[2].Capabilities = &declared
				var request map[string]interface{}
				if err := json.Unmarshal(body, &request); err != nil {
					t.Fatal(err)
				}
				config := request["config"].(map[string]interface{})
				field := config["transforms"].([]interface{})[0].(map[string]interface{})["fields"].([]interface{})[0].(map[string]interface{})
				switch scenario {
				case "invalid_constant", "unknown_source_with_invalid_constant":
					delete(field, "source")
					field["default"] = "{"
					if scenario == "unknown_source_with_invalid_constant" {
						delete(facts.items, 1)
					}
				case "object_array":
					field["target_type"] = "array"
					facts.items[1].Attributes["type_info"].(map[string]interface{})["table"] = datatype.TableInfoPayload(&datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "payload", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON, NativeType: "jsonb[]", Nullable: true}}})
				case "geometry":
					field["target_type"] = "geometry"
				case "append_json", "append_text":
					config["target"].(map[string]interface{})["policy"].(map[string]interface{})["apply_mode"] = "append"
					native := "jsonb"
					if engineType == "tidb" || engineType == "oceanbase" {
						native = "json"
					}
					observedType := datatype.FieldTypeJSON
					if scenario == "append_text" {
						native = "text"
						observedType = datatype.FieldTypeString
					}
					facts.items[2].Attributes["type_info"].(map[string]interface{})["table"] = datatype.TableInfoPayload(&datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "payload", Type: observedType, NativeType: native, Nullable: true}}})
				case "source_text":
					facts.items[1].Attributes["type_info"].(map[string]interface{})["table"] = datatype.TableInfoPayload(&datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "payload", Type: datatype.FieldTypeString, NativeType: "text", Nullable: true}}})

				}
				body, _ = json.Marshal(request)
				report := checkFixtureReport(t, svc, subject, body)
				want := plugin.DefinitionUnsupported
				if scenario == "json" || scenario == "append_json" {
					want = plugin.DefinitionSupported
				}
				if scenario == "source_text" {
					want = plugin.DefinitionUnknown
				}
				if report.Status != want || !slicesContains(report.Checked, "provider_write_definition") || facts.calls != 4 {
					t.Fatalf("report=%+v facts calls=%d", report, facts.calls)
				}
				for _, diagnostic := range report.Diagnostics {
					if diagnostic.Code == "provider_definition_check_not_available" {
						t.Fatal("registered protocol checker was skipped")
					}
					if diagnostic.Status == plugin.DefinitionUnsupported && (diagnostic.TargetField != "payload" || diagnostic.ConfigPath != "/config/transforms/0/fields/0") {
						t.Fatalf("field location lost: %+v", diagnostic)
					}
				}
			})
		}
	}
}
func TestConfigCheckBindsFullRequestAndStructuralFacts(t *testing.T) {
	svc, f, subject, body := configCheckFixture(t)
	report := checkFixtureReport(t, svc, subject, body)
	if report.Status != plugin.DefinitionSupported || len(report.Facts) != 2 || f.calls != 4 {
		t.Fatalf("report=%+v calls=%d", report, f.calls)
	}
	binding := report.BindingHash
	report.BindingHash = ""
	if configCheckHash(report) != binding {
		t.Fatal("report binding mismatch")
	}
	report.BindingHash = binding
	serialized, _ := json.Marshal(report)
	if strings.Contains(string(serialized), "never-return-secret") {
		t.Fatal("credential leaked")
	}
	for _, edit := range []string{`"schedule":"0 * * * *"`, `"description":"different"`, `"batch_size":17`, `"auto_scan_metadata":true`} {
		changed := strings.Replace(string(body), `"auto_scan_metadata":false`, edit, 1)
		if checkFixtureReport(t, svc, subject, []byte(changed)).ConfigHash == report.ConfigHash {
			t.Fatalf("full request not bound: %s", edit)
		}
	}
	table := f.items[1].Attributes["type_info"].(map[string]interface{})["table"].(map[string]interface{})
	fields := table["fields"]
	var normalized []map[string]interface{}
	encoded, _ := json.Marshal(fields)
	_ = json.Unmarshal(encoded, &normalized)
	for _, property := range []string{"native_type", "element_type", "default_expression", "precision", "scale", "ordinal_position"} {
		previous := normalized[0][property]
		if property == "precision" || property == "scale" || property == "ordinal_position" {
			normalized[0][property] = 7
		} else {
			normalized[0][property] = "changed"
		}
		table["fields"] = normalized
		changed := checkFixtureReport(t, svc, subject, body)
		if changed.Facts[0].SnapshotHash == report.Facts[0].SnapshotHash {
			t.Fatalf("missing structural binding: %s", property)
		}
		if previous == nil {
			delete(normalized[0], property)
		} else {
			normalized[0][property] = previous
		}
	}
	normalized = append(normalized, map[string]interface{}{"name": "second", "type": "string", "nullable": true})
	table["fields"] = normalized
	before := checkFixtureReport(t, svc, subject, body).Facts[0].SnapshotHash
	normalized[0], normalized[1] = normalized[1], normalized[0]
	if checkFixtureReport(t, svc, subject, body).Facts[0].SnapshotHash == before {
		t.Fatal("field order not bound")
	}
}
func TestConfigCheckDeniesBeforeReadingFacts(t *testing.T) {
	for _, name := range []string{"create", "meta", "system", "other_tenant", "project_scope", "service", "ticket", "wrong_scope", "wrong_audience"} {
		t.Run(name, func(t *testing.T) {
			svc, f, a, body := configCheckFixture(t)
			switch name {
			case "create":
				a.Authorization.RoleAssignments[0].Permissions = configCheckPermissions[1:]
			case "meta":
				a.Authorization.RoleAssignments[0].Permissions = []string{"transfer.task.create", "system.engine.read"}
			case "system":
				a.Authorization.RoleAssignments[0].Permissions = configCheckPermissions[:2]
			case "other_tenant":
				other := "8"
				a.Authorization.RoleAssignments[0].Scope.TenantID = &other
			case "project_scope":
				a.Authorization.RoleAssignments[0].Scope.Type = "project_group"
			case "service":
				a.Principal.Type = "service_principal"
				a.Token.Type = "service_access_token"
			case "ticket":
				a.Token.Type = "resource_access_ticket"
			case "wrong_scope":
				a.Token.Type = "delegated_access_token"
				a.Client.Audiences = []string{"transfer"}
				a.Client.Scopes = []string{"transfer.task.create"}
			case "wrong_audience":
				a.Token.Type = "delegated_access_token"
				a.Client.Audiences = []string{"meta"}
				a.Client.Scopes = []string{"transfer.task.config.check"}
			}
			_, err := svc.Check(context.Background(), a, body)
			if !errors.Is(err, ErrConfigCheckForbidden) || f.calls != 0 {
				t.Fatalf("err=%v calls=%d", err, f.calls)
			}
		})
	}
}
func TestConfigCheckUnknownVersusDependencyFailure(t *testing.T) {
	for _, name := range []string{"query", "missing_source", "sampled", "missing_append_target", "no_checker", "bad_identity", "meta_denied", "engine_failure", "upsert", "numeric_default"} {
		t.Run(name, func(t *testing.T) {
			svc, f, a, body := configCheckFixture(t)
			wantError := false
			switch name {
			case "query":
				body = []byte(strings.Replace(string(body), `"locator":"addp://engine/1/path/synthetic/source?type=table"`, `"query":{"language":"sql","statement":"SELECT sensitive FROM source"},"locator":"addp://engine/1/path/synthetic/source?type=table"`, 1))
			case "missing_source":
				delete(f.items, 1)
			case "sampled":
				f.engines[1].EngineType = "mongodb"
			case "missing_append_target":
				delete(f.items, 2)
				body = []byte(strings.Replace(string(body), `"replace"`, `"append"`, 1))
			case "no_checker":
				f.engines[2].EngineType = "mysql"
			case "bad_identity":
				f.items[1].TenantID = 8
				wantError = true
			case "meta_denied":
				f.metaErr = errors.New("upstream 403 sensitive")
				wantError = true
			case "engine_failure":
				f.engineErr = errors.New("network credentials")
				wantError = true
			case "upsert":
				body = []byte(strings.Replace(string(body), `"replace"`, `"upsert"`, 1))
			case "numeric_default":
				body = []byte(strings.Replace(string(body), `"source":"payload"`, `"default":{"large":9007199254740993}`, 1))
			}
			report, err := svc.Check(context.Background(), a, body)
			if wantError {
				if !errors.Is(err, ErrConfigCheckDependency) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil || report.Status != plugin.DefinitionUnknown {
				t.Fatalf("report=%+v err=%v", report, err)
			}
			if name == "upsert" {
				if f.calls != 0 {
					t.Fatal("outside scope accessed dependencies")
				}
			}
			encoded, _ := json.Marshal(report)
			if strings.Contains(string(encoded), "SELECT sensitive") {
				t.Fatal("query leaked")
			}
		})
	}
}
func TestConfigCheckRejectsArrayDefinitionWithFieldLocation(t *testing.T) {
	svc, f, a, body := configCheckFixture(t)
	f.items[1].Attributes["type_info"].(map[string]interface{})["table"] = datatype.TableInfoPayload(&datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "payload", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON, NativeType: "jsonb[]", Nullable: true}}})
	body = []byte(strings.Replace(string(body), `"target_type":"json"`, `"target_type":"array"`, 1))
	report := checkFixtureReport(t, svc, a, body)
	if report.Status != plugin.DefinitionUnsupported {
		t.Fatalf("report=%+v", report)
	}
	found := false
	for _, d := range report.Diagnostics {
		if d.Status == plugin.DefinitionUnsupported && d.TargetField == "payload" && d.SourceField == "payload" && d.ConfigPath == "/config/transforms/0/fields/0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("field diagnostics missing: %+v", report.Diagnostics)
	}
}

func TestConfigCheckUnknownSourceStillChecksExplicitTarget(t *testing.T) {
	for _, source := range []string{"query", "missing", "sampled"} {
		for _, target := range []string{"valid", "json_default", "decimal", "known_null"} {
			// Query mapping structurally requires a source; explicit constants are native-source cases.
			if source == "query" && target == "known_null" {
				continue
			}
			t.Run(source+"/"+target, func(t *testing.T) {
				svc, f, a, body := configCheckFixture(t)
				var request map[string]interface{}
				if err := json.Unmarshal(body, &request); err != nil {
					t.Fatal(err)
				}
				config := request["config"].(map[string]interface{})
				switch source {
				case "query":
					config["source"].(map[string]interface{})["query"] = map[string]interface{}{"language": "sql", "statement": "SELECT secret_alias FROM source"}
				case "missing":
					delete(f.items, 1)
				case "sampled":
					f.engines[1].EngineType = "mongodb"
				}
				field := map[string]interface{}{"source": "not_observed", "target": "result", "target_type": "json", "nullable": false}
				switch target {
				case "json_default":
					field["default"] = "invalid-json"
				case "decimal":
					field["target_type"] = "decimal"
					field["precision"] = 1001
					field["scale"] = 0
				case "known_null":
					delete(field, "source")
				}
				config["transforms"] = []interface{}{map[string]interface{}{"type": "field_mapping", "version": "v1", "mode": "project", "fields": []interface{}{field}}}
				body, _ = json.Marshal(request)
				report := checkFixtureReport(t, svc, a, body)
				want := plugin.DefinitionUnsupported
				if target == "valid" {
					want = plugin.DefinitionUnknown
				}
				if report.Status != want || f.calls != 4 || len(report.Facts) != 2 {
					t.Fatalf("report=%+v calls=%d", report, f.calls)
				}
				if !slicesContains(report.Checked, "provider_write_definition") {
					t.Fatal("target skipped")
				}
				for _, d := range report.Diagnostics {
					if target == "valid" && d.Code == "null_constant_not_nullable" {
						t.Fatal("unobserved source invented NULL")
					}
					if d.Status == plugin.DefinitionUnsupported && d.ConfigPath != "/config/transforms/0/fields/0" {
						t.Fatalf("missing location: %+v", d)
					}
				}
				encoded, _ := json.Marshal(report)
				if strings.Contains(string(encoded), "secret_alias") {
					t.Fatal("query leaked")
				}
			})
		}
	}
}

func slicesContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func TestConfigCheckMongoProjectionUsesBoundSnapshotAndKeepsUnknown(t *testing.T) {
	svc, f, a, body := configCheckFixture(t)
	mongo, _ := plugin.Get("mongodb")
	caps, _ := plugin.MarshalEngineCapabilities(mongo.Capabilities())
	declared := commonmodels.JSONString(caps)
	f.engines[1].EngineType = "mongodb"
	f.engines[1].Capabilities = &declared
	f.items[1].ItemType = "collection"
	f.items[1].Attributes["type_info"].(map[string]interface{})["table"] = datatype.TableInfoPayload(&datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "observed", Type: datatype.FieldTypeString, Nullable: true}}})
	var request map[string]interface{}
	_ = json.Unmarshal(body, &request)
	config := request["config"].(map[string]interface{})
	source := config["source"].(map[string]interface{})
	source["locator"] = "addp://engine/1/path/synthetic/source?type=collection"
	source["query"] = map[string]interface{}{"language": "mql", "statement": `{"aggregate":"source","pipeline":[{"$project":{"alias":"$observed","unseen":"$not_observed","_id":0}}]}`}
	config["transforms"] = []interface{}{map[string]interface{}{"type": "field_mapping", "version": "v1", "mode": "project", "fields": []interface{}{map[string]interface{}{"source": "unseen", "target": "result", "target_type": "json", "nullable": false}}}}
	body, _ = json.Marshal(request)
	report := checkFixtureReport(t, svc, a, body)
	if report.Status != plugin.DefinitionUnknown || len(report.Facts) != 2 || report.Facts[0].Coverage != "sampled" || f.calls != 4 || !slicesContains(report.Checked, "provider_query_definition") {
		t.Fatalf("report=%+v", report)
	}
	found := false
	for _, d := range report.Diagnostics {
		if d.Code == "query_source_field_not_observed" {
			found = true
		}
		if d.Status == plugin.DefinitionUnsupported {
			t.Fatalf("sample proves absence: %+v", d)
		}
	}
	if !found {
		t.Fatal("query analysis not consumed")
	}
	before := report.BindingHash
	config["source"].(map[string]interface{})["query"].(map[string]interface{})["statement"] = `{"aggregate":"source","pipeline":[{"$group":{"_id":null}}]}`
	body, _ = json.Marshal(request)
	changed := checkFixtureReport(t, svc, a, body)
	if changed.BindingHash == before || changed.ConfigHash == report.ConfigHash {
		t.Fatal("query not bound")
	}
	for _, d := range changed.Diagnostics {
		if d.Code == "query_projection_definition_checked" {
			t.Fatal("complex query reused old output")
		}
	}
	// Ordinary table access cannot substitute for the query read-session route.
	declaredCaps, err := plugin.ParseEngineCapabilities(string(declared))
	if err != nil {
		t.Fatal(err)
	}
	declaredCaps.Compute.Query.ReadSession = false
	disabled, err := plugin.MarshalEngineCapabilities(*declaredCaps)
	if err != nil {
		t.Fatal(err)
	}
	disabledDeclaration := commonmodels.JSONString(disabled)
	f.engines[1].Capabilities = &disabledDeclaration
	rejected := checkFixtureReport(t, svc, a, body)
	if rejected.Status != plugin.DefinitionUnsupported || rejected.Facts[0].NativeAccess != plugin.DefinitionUnsupported {
		t.Fatalf("missing query capability accepted: %+v", rejected)
	}
}
func TestConfigCheckStrictRequestAndLosslessConfigHash(t *testing.T) {
	svc, _, a, body := configCheckFixture(t)
	for _, invalid := range [][]byte{append(append([]byte{}, body...), body...), []byte(`{"name":"x","config":{},"unexpected":true}`), []byte(strings.Repeat(" ", 513<<10)), bytes.Replace(body, []byte("synthetic check"), []byte{0xff}, 1)} {
		if _, err := svc.Check(context.Background(), a, invalid); !errors.Is(err, ErrInvalidTaskConfig) {
			t.Fatalf("err=%v", err)
		}
	}
	one := []byte(strings.Replace(string(body), `"source":"payload"`, `"default":9007199254740992`, 1))
	two := []byte(strings.Replace(string(body), `"source":"payload"`, `"default":9007199254740993`, 1))
	if checkFixtureReport(t, svc, a, one).ConfigHash == checkFixtureReport(t, svc, a, two).ConfigHash {
		t.Fatal("large numbers lost in request hash")
	}
}

func TestConfigCheckBindingUsesWireObjectsAndAllSnapshotFields(t *testing.T) {
	svc, f, a, body := configCheckFixture(t)
	table := &datatype.TableInfo{}
	for i := 0; i < 260; i++ {
		table.Fields = append(table.Fields, datatype.FieldInfo{Name: "field_" + strconv.Itoa(i), Type: datatype.FieldTypeString, NativeType: "text", Nullable: true})
	}
	table.Fields = append(table.Fields, datatype.FieldInfo{Name: "payload", Type: datatype.FieldTypeJSON, NativeType: "jsonb", Nullable: true})
	payload := f.items[1].Attributes["type_info"].(map[string]interface{})
	payload["table"] = datatype.TableInfoPayload(table)
	report := checkFixtureReport(t, svc, a, body)
	encoded, _ := json.Marshal(report)
	var wire map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&wire); err != nil {
		t.Fatal(err)
	}
	wire["binding_hash"] = ""
	canonical, _ := json.Marshal(wire)
	digest := sha256.Sum256(canonical)
	if report.BindingHash != hex.EncodeToString(digest[:]) {
		t.Fatal("wire report binding is not reproducible")
	}
	before := report.Facts[0].SnapshotHash
	table.Fields[259].DefaultExpression = "private default expression"
	payload["table"] = datatype.TableInfoPayload(table)
	after := checkFixtureReport(t, svc, a, body)
	if after.Facts[0].SnapshotHash == before {
		t.Fatal("snapshot truncated after 200 fields")
	}
	before = after.Facts[0].SnapshotHash
	f.items[1].Attributes["capabilities"] = map[string]interface{}{"spatial": map[string]interface{}{"srid": 4326}}
	after = checkFixtureReport(t, svc, a, body)
	if after.Facts[0].SnapshotHash == before {
		t.Fatal("spatial facts not bound")
	}
	before = after.Facts[0].SnapshotHash
	later := f.items[1].ScannedAt.Add(time.Second)
	f.items[1].ScannedAt = &later
	after = checkFixtureReport(t, svc, a, body)
	if after.Facts[0].SnapshotHash == before {
		t.Fatal("scan time not bound")
	}
	encoded, _ = json.Marshal(after)
	if strings.Contains(string(encoded), "private default expression") {
		t.Fatal("default expression leaked")
	}
	beforeEngine := after.Facts[0].EngineFactsHash
	f.engines[1].Version++
	if checkFixtureReport(t, svc, a, body).Facts[0].EngineFactsHash == beforeEngine {
		t.Fatal("engine revision not bound")
	}
}
