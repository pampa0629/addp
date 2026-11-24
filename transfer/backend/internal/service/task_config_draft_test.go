package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/addp/common/authorization"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/query"
	"github.com/addp/transfer/internal/protection"
)

func draftFixture(t *testing.T) (TaskConfigDraftRequest, taskConfigDraftFacts) {
	t.Helper()
	r := TaskConfigDraftRequest{Name: "persons", SourceLocator: "addp://engine/1/path/synthetic/source?type=collection", Target: TaskConfigDraftTarget{ParentLocator: "addp://engine/2/path/synthetic?type=schema", Name: "target", ApplyMode: "replace"}, RowGrain: TaskConfigDraftRowGrain{Mode: "document"}, Fields: []TaskConfigDraftField{{Path: []string{"members"}, Target: "members"}}}
	f := taskConfigDraftFacts{sourceLocator: r.SourceLocator, targetParentLocator: r.Target.ParentLocator, sourceEngineType: "mongodb", targetEngineType: "postgresql", mql: query.ResourceQueryReference{Parts: []string{"source"}}, source: &datatype.TableInfo{Fields: []datatype.FieldInfo{
		{Name: "members", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON, Nullable: true, Element: &datatype.FieldInfo{Type: datatype.FieldTypeJSON, Fields: []datatype.FieldInfo{{Name: "name", Type: datatype.FieldTypeString, Nullable: true}}}},
		{Name: "_id", Type: datatype.FieldTypeString},
	}}}
	return r, f
}

func TestTaskConfigDraftComposesWithOwnerPrecheck(t *testing.T) {
	for _, mode := range []string{"document", "scalar_array", "unknown_array", "nested_array", "array_element"} {
		t.Run(mode, func(t *testing.T) {
			r, facts := draftFixture(t)
			switch mode {
			case "scalar_array":
				facts.source.Fields[0].Element = &datatype.FieldInfo{Type: datatype.FieldTypeString}
				facts.source.Fields[0].ElementType = datatype.FieldTypeString
			case "unknown_array":
				facts.source.Fields[0].Element = nil
				facts.source.Fields[0].ElementType = datatype.FieldTypeUnknown
			case "nested_array":
				facts.source.Fields[0].Element = &datatype.FieldInfo{Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON}
				facts.source.Fields[0].ElementType = datatype.FieldTypeArray
			}
			if mode == "array_element" {
				r.RowGrain = TaskConfigDraftRowGrain{Mode: mode, Path: []string{"members"}}
				r.Fields = []TaskConfigDraftField{{Path: []string{"members", "name"}, Target: "member_name"}}
			}
			before, _ := json.Marshal(facts.source)
			args, err := buildTaskConfigDraft(r, facts)
			if err != nil {
				t.Fatal(err)
			}
			again, err := buildTaskConfigDraft(r, facts)
			if err != nil || !reflect.DeepEqual(args, again) {
				t.Fatal("nondeterministic", err)
			}
			after, _ := json.Marshal(facts.source)
			if string(before) != string(after) {
				t.Fatal("mutated owner facts")
			}
			if _, ok := args["batch_size"]; ok {
				t.Fatal("invented optional default")
			}
			config := args["config"].(map[string]interface{})
			mappings := config["transforms"].([]interface{})[0].(map[string]interface{})["fields"].([]interface{})
			want := "json"
			if mode == "array_element" {
				want = "string"
			}
			if mappings[0].(map[string]interface{})["target_type"] != want {
				t.Fatal(mappings)
			}
			var command map[string]interface{}
			statement := config["source"].(map[string]interface{})["query"].(map[string]interface{})["statement"].(string)
			if err := json.Unmarshal([]byte(statement), &command); err != nil {
				t.Fatal(err)
			}
			pipeline := command["pipeline"].([]interface{})
			project := pipeline[len(pipeline)-1].(map[string]interface{})["$project"].(map[string]interface{})
			if command["aggregate"] != "source" || project["_id"] != float64(0) || len(project) != 2 {
				t.Fatal(command)
			}
			svc, stubs, subject, _ := configCheckFixture(t)
			mongo, _ := plugin.Get("mongodb")
			caps, _ := plugin.MarshalEngineCapabilities(mongo.Capabilities())
			declared := commonmodels.JSONString(caps)
			stubs.engines[1].EngineType = "mongodb"
			stubs.engines[1].Capabilities = &declared
			stubs.items[1].ItemType = "collection"
			stubs.items[1].Attributes["type_info"].(map[string]interface{})["table"] = datatype.TableInfoPayload(facts.source)
			// The existing REST creation/check envelope is exactly the SDK adaptation.
			// No configuration field or query is reconstructed here.
			args["task_type"] = "sync"
			args["enabled"] = false
			args["auto_scan_metadata"] = false
			body, _ := json.Marshal(args)
			report, err := svc.Check(context.Background(), subject, body)
			if err != nil || report == nil {
				t.Fatal(err)
			}
			if report.Status == plugin.DefinitionUnsupported || !slicesContains(report.Checked, "provider_query_definition") || stubs.calls != 4 {
				t.Fatalf("%+v calls=%d", report, stubs.calls)
			}
			for _, diagnostic := range report.Diagnostics {
				if diagnostic.Code == "query_source_field_not_observed" {
					t.Fatalf("compiler lost the observed source path: %+v", diagnostic)
				}
			}
			t.Logf("%s report=%s", mode, report.Status)
		})
	}
}

func TestTaskConfigDraftRejectsUnresolvedChoices(t *testing.T) {
	for _, name := range []string{"scope", "identity", "missing_facts", "query_reference", "unknown_field", "dot_field", "array_traversal", "duplicate_column", "missing_type", "invalid_grain", "invalid_precision", "empty_array_path"} {
		t.Run(name, func(t *testing.T) {
			r, f := draftFixture(t)
			switch name {
			case "scope":
				f.targetEngineType = "mysql"
			case "identity":
				r.SourceLocator += "&item_id=99"
			case "missing_facts":
				f.source = nil
			case "query_reference":
				f.mql.Parts = nil
			case "unknown_field":
				r.Fields[0].Path = []string{"absent"}
			case "dot_field":
				r.Fields[0].Path = []string{"literal.dot"}
			case "array_traversal":
				r.Fields[0].Path = []string{"members", "name"}
			case "duplicate_column":
				r.Fields = append(r.Fields, r.Fields[0])
			case "missing_type":
				f.source.Fields[0].Type = datatype.FieldTypeUnknown
			case "invalid_grain":
				r.RowGrain.Mode = "guess"
			case "invalid_precision":
				n := 0
				r.Fields[0].Precision = &n
			case "empty_array_path":
				r.RowGrain = TaskConfigDraftRowGrain{Mode: "array_element"}
			}
			got, err := buildTaskConfigDraft(r, f)
			if err == nil || got != nil {
				t.Fatal("invalid choice produced executable parameters", got)
			}
		})
	}
}

type draftMetadataFixture struct {
	item      *commonmodels.MetaItem
	ancestors *commonmodels.MetaItemAncestors
	parent    *commonmodels.MetaNode
	calls     int
}

func (f *draftMetadataFixture) GetItemSnapshot(context.Context, uint, uint, uint, string) (*commonmodels.MetaItem, error) {
	f.calls++
	return f.item, nil
}
func (f *draftMetadataFixture) GetItemAncestors(context.Context, uint) (*commonmodels.MetaItemAncestors, error) {
	f.calls++
	return f.ancestors, nil
}
func (f *draftMetadataFixture) GetNodeByID(context.Context, uint) (*commonmodels.MetaNode, error) {
	f.calls++
	return f.parent, nil
}
func (f *draftMetadataFixture) GetNodeByCatalogPath(context.Context, uint, string) (*commonmodels.MetaNode, error) {
	f.calls++
	return f.parent, nil
}

func TestTaskConfigDraftOwnerReadsAndAuthorization(t *testing.T) {
	for _, name := range []string{"user", "delegated", "missing_manager_permission", "extra_scope", "source_mismatch", "source_path_collision", "source_dotted_name", "parent_mismatch", "missing_structure", "untrusted_schema", "dependency"} {
		t.Run(name, func(t *testing.T) {
			_, engines, subject, _ := configCheckFixture(t)
			subject.Authorization.RoleAssignments[0].Permissions = append([]string{}, draftPermissions...)
			sort.Strings(subject.Authorization.RoleAssignments[0].Permissions)
			request, facts := draftFixture(t)
			item := engines.items[1]
			item.ItemType = "collection"
			item.NodeID = 90
			item.Attributes["type_info"].(map[string]interface{})["table"] = datatype.TableInfoPayload(facts.source)
			metadata := &draftMetadataFixture{item: item, ancestors: &commonmodels.MetaItemAncestors{Item: *item, Ancestors: []commonmodels.MetaNode{{ID: 90, TenantID: 7, EngineID: 1, Name: "synthetic", FullName: "synthetic", NodeType: "database"}}}, parent: &commonmodels.MetaNode{ID: 20, TenantID: 7, EngineID: 2, Name: "synthetic", FullName: "synthetic", NodeType: "schema"}}
			descriptorReads := 0
			svc := &TaskConfigDraftService{engines: func(_ context.Context, tenant, id uint) (*commonmodels.EngineRuntimeDescriptor, error) {
				descriptorReads++
				if tenant != 7 || (id != 1 && id != 2) {
					t.Fatalf("unexpected descriptor tenant=%d id=%d", tenant, id)
				}
				engineType := map[uint]string{1: facts.sourceEngineType, 2: facts.targetEngineType}[id]
				return &commonmodels.EngineRuntimeDescriptor{ID: id, EngineType: engineType, LifecycleState: "active", ConnectionStatus: "online"}, nil
			}, metadata: func(tenant uint) draftMetadata {
				if tenant != 7 {
					t.Fatal("wrong tenant")
				}
				return metadata
			}}
			switch name {
			case "delegated", "extra_scope":
				subject.Token.Type = "delegated_access_token"
				subject.Client.ScopeMode = "restricted"
				subject.Client.Audiences = []string{"transfer"}
				subject.Client.Scopes = []string{"transfer.task.draft.build"}
				subject.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *subject.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
				if name == "extra_scope" {
					subject.Client.Scopes = append(subject.Client.Scopes, "transfer.task.create")
				}
			case "missing_manager_permission":
				subject.Authorization.RoleAssignments[0].Permissions = configCheckPermissions
				sort.Strings(subject.Authorization.RoleAssignments[0].Permissions)
			case "source_mismatch":
				metadata.ancestors.Item.ID++
			case "source_path_collision":
				request.SourceLocator = "addp://engine/1/path/synthetic.extra/source?type=collection"
				item.Name = "extra.source"
				item.FullName = "synthetic.extra.source"
				metadata.ancestors.Item = *item
			case "source_dotted_name":
				request.SourceLocator = "addp://engine/1/path/synthetic/extra.source?type=collection"
				item.Name = "extra.source"
				item.FullName = "synthetic.extra.source"
				metadata.ancestors.Item = *item
			case "parent_mismatch":
				metadata.parent.EngineID++
			case "missing_structure":
				item.Attributes = nil
				metadata.ancestors.Item = *item
			case "dependency":
				svc.engines = nil
			}
			sort.Strings(subject.Client.Scopes)
			body, _ := json.Marshal(request)
			if name == "untrusted_schema" {
				var obj map[string]interface{}
				json.Unmarshal(body, &obj)
				obj["schema"] = map[string]interface{}{"fields": []string{"fake"}}
				body, _ = json.Marshal(obj)
			}
			result, err := svc.Build(context.Background(), subject, body)
			if name == "user" || name == "delegated" || name == "source_dotted_name" {
				if err != nil || result == nil {
					t.Fatal(err)
				}
				if metadata.calls != 3 {
					t.Fatal(metadata.calls)
				}
				var command map[string]interface{}
				source := result.Arguments["config"].(map[string]interface{})["source"].(map[string]interface{})
				statement := source["query"].(map[string]interface{})["statement"].(string)
				if json.Unmarshal([]byte(statement), &command) != nil || command["aggregate"] != item.Name {
					t.Fatal("query name was not derived from owner identity", command)
				}

				return
			}
			if err == nil || result != nil {
				t.Fatal("unauthorized or unresolved facts produced arguments")
			}
			if name == "missing_manager_permission" || name == "extra_scope" || name == "untrusted_schema" {
				if metadata.calls != 0 || descriptorReads != 0 {
					t.Fatal("read before authorization/input validation")
				}
			}
		})
	}
}

func TestTaskConfigDraftReadsOnlyTenantDescriptorsAndMeta(t *testing.T) {
	_, engines, subject, _ := configCheckFixture(t)
	subject.Authorization.RoleAssignments[0].Permissions = append([]string{}, draftPermissions...)
	sort.Strings(subject.Authorization.RoleAssignments[0].Permissions)
	request, facts := draftFixture(t)
	item := engines.items[1]
	item.ItemType = "collection"
	item.NodeID = 90
	item.Attributes["type_info"].(map[string]interface{})["table"] = datatype.TableInfoPayload(facts.source)
	ancestors := commonmodels.MetaItemAncestors{Item: *item, Ancestors: []commonmodels.MetaNode{{ID: 90, TenantID: 7, EngineID: 1, Name: "synthetic", FullName: "synthetic", NodeType: "database"}}}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	want, err := buildTaskConfigDraft(request, facts)
	if err != nil {
		t.Fatal(err)
	}
	const descriptors = "/api/v1/system/runtime/engine-descriptors/"
	for _, side := range []string{"1", "2"} {
		for _, state := range []string{"online", "offline", "deleted", "other_tenant", "shared_builtin", "wrong_id"} {
			if state == "online" && side == "2" {
				continue
			}
			t.Run(side+"/"+state, func(t *testing.T) {
				var paths []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					paths = append(paths, r.URL.Path)
					if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer tenant-7" || r.Header.Get("X-Tenant-ID") != "" {
						t.Errorf("unexpected owner credential or operation on %s", r.URL)
					}
					switch r.URL.Path {
					case descriptors + "1", descriptors + "2":
						d := commonmodels.EngineRuntimeDescriptor{ID: 1, EngineType: "mongodb", LifecycleState: "active", ConnectionStatus: "online"}
						if r.URL.Path == descriptors+"2" {
							d.ID, d.EngineType = 2, "postgresql"
						}
						if r.URL.Path == descriptors+side {
							switch state {
							case "other_tenant":
								w.WriteHeader(http.StatusForbidden)
								return
							case "offline":
								d.ConnectionStatus = "offline"
							case "deleted":
								d.LifecycleState = "deleted"
							case "shared_builtin":
								d.IsBuiltin = true
							case "wrong_id":
								d.ID = 99
							}
						}
						json.NewEncoder(w).Encode(d)
					case "/api/v1/meta/items/by-catalog-path":
						json.NewEncoder(w).Encode(item)
					case "/api/v1/meta/items/11/ancestors":
						json.NewEncoder(w).Encode(ancestors)
					case "/api/v1/meta/nodes/by-catalog-path":
						json.NewEncoder(w).Encode(commonmodels.MetaNode{ID: 20, TenantID: 7, EngineID: 2, Name: "synthetic", FullName: "synthetic", NodeType: "schema"})
					default:
						t.Errorf("unexpected route %s", r.URL)
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				defer server.Close()
				tokens := recommendationTokenSource{}
				system := commonclient.NewSystemServiceClient(server.URL, tokens, server.Client())
				svc := NewTaskConfigDraftService(protection.EngineDescriptorLookup(system), commonclient.NewMetaClient(server.URL, tokens))
				result, err := svc.Build(context.Background(), subject, body)
				wantPaths := []string{descriptors + "1"}
				if state == "online" || side == "2" {
					wantPaths = append(wantPaths, descriptors+"2")
				}
				if state == "online" {
					wantPaths = append(wantPaths, "/api/v1/meta/items/by-catalog-path", "/api/v1/meta/items/11/ancestors", "/api/v1/meta/nodes/by-catalog-path")
					if err != nil || result == nil || !reflect.DeepEqual(result.Arguments, want) {
						t.Fatalf("unexpected draft: %v, err=%v", result, err)
					}
				} else if !errors.Is(err, ErrConfigCheckDependency) || result != nil {
					t.Fatalf("expected dependency rejection, got %v, err=%v", result, err)
				}
				if !reflect.DeepEqual(paths, wantPaths) {
					t.Fatalf("read paths=%v, want %v", paths, wantPaths)
				}
			})
		}
	}
}
