package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/format"
	commonjson "github.com/addp/common/jsonmap"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/protection"
)

var ErrConfigCheckDependency = errors.New("task config check dependency unavailable")
var ErrConfigCheckForbidden = errors.New("task config check requires tenant user permissions")

var configCheckPermissions = []string{"transfer.task.create", "meta.catalog.read", "system.engine.read"}

type ConfigCheckMetaFacts interface {
	GetItemSnapshot(context.Context, uint, uint, uint, string) (*commonmodels.MetaItem, error)
}

type TaskConfigCheckService struct {
	engines  protection.EngineDescriptorGetter
	metadata ConfigCheckMetaFacts
}

func NewTaskConfigCheckService(engines protection.EngineDescriptorGetter, metadata ConfigCheckMetaFacts) *TaskConfigCheckService {
	return &TaskConfigCheckService{engines: engines, metadata: metadata}
}
func (s *TaskService) SetTaskConfigChecker(checker *TaskConfigCheckService) {
	s.configChecker = checker
}
func (s *TaskService) CheckTaskConfig(ctx context.Context, subject authorization.AuthContext, body []byte) (*TaskConfigCheckReport, error) {
	if s == nil || s.configChecker == nil {
		return nil, ErrConfigCheckDependency
	}
	return s.configChecker.Check(ctx, subject, body)
}

type TaskConfigCheckDiagnostic struct {
	Status       plugin.DefinitionCheckStatus `json:"status"`
	Code         string                       `json:"code"`
	Check        string                       `json:"check"`
	ConfigPath   string                       `json:"config_path"`
	TargetField  string                       `json:"target_field,omitempty"`
	SourceField  string                       `json:"source_field,omitempty"`
	PhysicalType string                       `json:"physical_type,omitempty"`
}
type TaskConfigCheckFact struct {
	Role            string                       `json:"role"`
	EngineID        uint                         `json:"engine_id"`
	EngineVersion   int64                        `json:"engine_version"`
	EngineType      string                       `json:"engine_type"`
	EngineFactsHash string                       `json:"engine_facts_hash"`
	SnapshotHash    string                       `json:"snapshot_hash"`
	Coverage        string                       `json:"schema_coverage"`
	NativeAccess    plugin.DefinitionCheckStatus `json:"native_access"`
	MetaItemID      uint                         `json:"meta_item_id,omitempty"`
	ScannedAt       *time.Time                   `json:"scanned_at,omitempty"`
}
type TaskConfigCheckReport struct {
	SchemaVersion         string                       `json:"schema_version"`
	TenantID              uint                         `json:"tenant_id"`
	PrincipalID           string                       `json:"principal_id"`
	ConfigHash            string                       `json:"config_hash"`
	BindingHash           string                       `json:"binding_hash"`
	ObservedAt            time.Time                    `json:"observed_at"`
	Status                plugin.DefinitionCheckStatus `json:"status"`
	Checked               []string                     `json:"checked"`
	NotChecked            []string                     `json:"not_checked"`
	RuntimeChecksRequired []string                     `json:"runtime_checks_required"`
	Facts                 []TaskConfigCheckFact        `json:"facts"`
	Diagnostics           []TaskConfigCheckDiagnostic  `json:"diagnostics"`
}

// Check authorizes before reading service-side facts. It never calls the
// planner that opens resources, query providers, PrepareTableWrite, or a task repository.
func (s *TaskConfigCheckService) Check(ctx context.Context, subject authorization.AuthContext, body []byte) (*TaskConfigCheckReport, error) {
	if subject.Principal.Type != "user" || subject.Context.Type != "tenant" || subject.Context.TenantID == nil ||
		!authorization.HasContextPermissions(subject, configCheckPermissions...) {
		return nil, ErrConfigCheckForbidden
	}
	switch subject.Token.Type {
	case "first_party_access_token", "oauth_access_token", "delegated_access_token":
	default:
		return nil, ErrConfigCheckForbidden
	}
	if subject.Token.Type == "delegated_access_token" && (len(subject.Client.Audiences) != 1 || subject.Client.Audiences[0] != "transfer" || len(subject.Client.Scopes) != 1 || subject.Client.Scopes[0] != "transfer.task.config.check") {
		return nil, ErrConfigCheckForbidden
	}
	tenant, err := strconv.ParseInt(*subject.Context.TenantID, 10, 64)
	if err != nil || tenant <= 0 || strings.TrimSpace(subject.Principal.ID) == "" {
		return nil, ErrConfigCheckForbidden
	}
	if len(body) == 0 || len(body) > 512<<10 || !utf8.Valid(body) {
		return nil, ErrInvalidTaskConfig
	}
	var request models.CreateTaskRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(interface{})) != io.EOF || strings.TrimSpace(request.Name) == "" || request.TaskType != "sync" || len(request.Config) == 0 || request.BatchSize < 0 {
		return nil, ErrInvalidTaskConfig
	}
	var submitted interface{}
	raw := json.NewDecoder(bytes.NewReader(body))
	raw.UseNumber()
	if raw.Decode(&submitted) != nil {
		return nil, ErrInvalidTaskConfig
	}
	report := &TaskConfigCheckReport{
		SchemaVersion: "addp.transfer_task_config_check/v1", TenantID: uint(tenant), PrincipalID: subject.Principal.ID,
		ConfigHash: configCheckHash(submitted), ObservedAt: time.Now().UTC(), Status: plugin.DefinitionSupported,
		Checked: []string{"request_shape"}, NotChecked: []string{"live_catalog", "live_connection", "meta_snapshot_freshness", "business_rows", "source_query", "source_data_authorization", "schedule", "auto_scan_metadata", "database_privileges", "constraints_and_triggers"},
		RuntimeChecksRequired: []string{"current_authorization", "current_source_schema", "current_target_schema", "per_value_encoding", "database_privileges", "constraints_and_triggers"},
		Facts:                 []TaskConfigCheckFact{}, Diagnostics: []TaskConfigCheckDiagnostic{},
	}
	add := func(status plugin.DefinitionCheckStatus, code, check, path string) {
		report.add(TaskConfigCheckDiagnostic{Status: status, Code: code, Check: check, ConfigPath: path})
	}
	runtime, _ := request.Config["runtime"].(map[string]interface{})
	load, _ := request.Config["load"].(map[string]interface{})
	source, _ := request.Config["source"].(map[string]interface{})
	target, _ := request.Config["target"].(map[string]interface{})
	policy, _ := target["policy"].(map[string]interface{})
	objectImport := source["representation"] == "encoded" && source["format"] == "parquet"
	if runtime["boundary"] != "bounded" || load["mode"] != "snapshot" || source["data_type"] != "table" || target["data_type"] != "table" || (source["representation"] != "native" && !objectImport) || target["representation"] != "native" || (policy["apply_mode"] != "replace" && policy["apply_mode"] != "append") {
		add(plugin.DefinitionUnknown, "task_scope_not_checked", "scope", "/config")
		return finishConfigCheck(report), nil
	}
	var sourceLocator, parent *resourcetree.ResourceLocator
	if objectImport {
		if _, supplied := request.Config["object_source"]; supplied {
			return nil, ErrInvalidTaskConfig
		}
		sourceLocator, parent, err = execution.TransferObjectImportLocators(request.Config)
		if err != nil || strings.TrimSpace(request.Schedule) != "" {
			add(plugin.DefinitionUnknown, "task_scope_not_checked", "scope", "/config")
			return finishConfigCheck(report), nil
		}
	}
	spec, err := planner.ParseTableExportTaskSpec(request.Config, request.BatchSize)
	if err != nil {
		return nil, ErrInvalidTaskConfig
	}
	report.Checked = append(report.Checked, "task_config_shape")
	if !objectImport {
		sourceLocator, err = spec.Source.ResourceLocator()
		if err != nil {
			return nil, ErrInvalidTaskConfig
		}
		parent, err = spec.Target.ParentResourceLocator()
		if err != nil {
			return nil, ErrInvalidTaskConfig
		}
	}
	targetLocator := &resourcetree.ResourceLocator{EngineID: parent.EngineID, Type: resourcetree.TypeTable, Path: append(append([]string{}, parent.Path...), spec.Target.Name)}
	sourceInfo, sourceSpatial, sourceFact, sourcePlugin, err := s.facts(ctx, uint(tenant), "source", sourceLocator, spec.Source.Query != nil, false)
	if err != nil {
		return nil, err
	}
	if objectImport && sourceFact.EngineType != "rustfs" {
		add(plugin.DefinitionUnknown, "task_scope_not_checked", "scope", "/config/source")
		return finishConfigCheck(report), nil
	}
	report.Facts = append(report.Facts, sourceFact)
	targetInfo, _, targetFact, targetPlugin, err := s.facts(ctx, uint(tenant), "target", targetLocator, false, policy["apply_mode"] == "replace")
	if err != nil {
		return nil, err
	}
	report.Facts = append(report.Facts, targetFact)
	report.Checked = append(report.Checked, "registered_engine_facts", "persisted_meta_snapshots")
	if objectImport && targetFact.EngineType != "postgresql" {
		add(plugin.DefinitionUnsupported, "object_import_target_not_supported", "scope", "/config/target")
		return finishConfigCheck(report), nil
	}
	for _, fact := range report.Facts {
		if fact.NativeAccess != plugin.DefinitionSupported {
			path := "/config/" + fact.Role
			code := "registered_native_access_not_supported"
			if objectImport && fact.Role == "source" {
				code = "registered_object_read_not_supported"
			}
			if fact.Role == "target" && policy["apply_mode"] == "replace" {
				path += "/policy/apply_mode"
			}
			add(fact.NativeAccess, code, "engine_capability", path)
		}
	}
	sourceComplete := sourceInfo != nil && sourceFact.Coverage == plugin.QuerySchemaCoverageComplete
	if sourceFact.Coverage != plugin.QuerySchemaCoverageComplete && sourceFact.Coverage != plugin.QuerySchemaCoverageSampled {
		// Payloads without an observed snapshot cannot provide counterexamples.
		// Keep their full payload in the binding hash, not as field evidence.
		sourceInfo, sourceSpatial = nil, nil
	}
	if spec.Source.Query != nil {
		// A saved source table is not query output. Only the pure Provider contract
		// may supply partial output, never PreparedQuery runtime catalog methods.
		var output *datatype.TableInfo
		snapshotComplete := sourceComplete
		sourceComplete = false
		sourceSpatial = nil
		checker, available := sourcePlugin.(plugin.QueryDefinitionChecker)
		model, hasModel := sourcePlugin.(plugin.EngineCatalogModelProvider)
		if available && hasModel && len(spec.Source.Query.Inputs) == 0 {
			path, pathErr := resourcetree.EngineCatalogPathFromLocator(model.EngineCatalogModel(), sourceLocator)
			if pathErr == nil {
				result := checker.CheckQueryDefinition(plugin.QueryDefinition{
					Request: plugin.QueryRequest{EngineID: sourceLocator.EngineID, Language: spec.Source.Query.Language, Query: spec.Source.Query.Statement, TargetPath: &path,
						Options: plugin.QueryOptions{ReadOnly: true, Parameters: spec.Source.Query.Parameters}},
					SourcePath: path, Source: sourceInfo, SchemaCoverage: sourceFact.Coverage,
				})
				output = result.Output
				sourceComplete = output != nil && result.SchemaCoverage == plugin.QuerySchemaCoverageComplete && snapshotComplete
				for _, check := range result.Checks {
					report.add(TaskConfigCheckDiagnostic{Status: check.Status, Code: check.Code, Check: "source_query_definition", SourceField: check.Field, ConfigPath: "/config/source/query"})
				}
				report.Checked = append(report.Checked, "provider_query_definition")
			}
		}
		sourceInfo = output
		if output == nil {
			add(plugin.DefinitionUnknown, "query_output_not_checked", "source_definition", "/config/source/query")
		}
	}
	if !sourceComplete {
		add(plugin.DefinitionUnknown, "source_schema_not_complete", "source_definition", "/config/source/locator")
	}
	definition, err := planner.CheckableTableDefinition(spec, sourceInfo, sourceSpatial, sourceComplete)
	if err != nil {
		add(plugin.DefinitionUnknown, "transform_definition_not_checked", "transform", "/config/transforms")
		return finishConfigCheck(report), nil
	}
	// The runtime config parser currently uses float64 for interface constants.
	// Such numeric defaults cannot be certified as preserving the submitted number.
	for i, transform := range spec.Transforms {
		for j, field := range transform.Fields {
			if containsConfigNumber(field.Default) {
				add(plugin.DefinitionUnknown, "numeric_default_runtime_conversion", "constant_encoding", "/config/transforms/"+strconv.Itoa(i)+"/fields/"+strconv.Itoa(j)+"/default")
			}
		}
	}
	if policy["apply_mode"] == "append" {
		if targetInfo != nil && targetFact.Coverage == "complete" {
			definition.Existing = targetInfo
		} else {
			add(plugin.DefinitionUnknown, "target_schema_not_complete", "existing_target", "/config/target")
		}
	} else {
		report.NotChecked = append(report.NotChecked, "replace_target_side_effects")
	}
	checker, ok := targetPlugin.(plugin.TableWriteDefinitionChecker)
	if !ok {
		add(plugin.DefinitionUnknown, "provider_definition_check_not_available", "target_definition", "/config/target")
		return finishConfigCheck(report), nil
	}
	if len(definition.Fields) == 0 && !sourceComplete {
		return finishConfigCheck(report), nil
	}
	for _, check := range checker.CheckTableWriteDefinition(definition).Checks {
		if !sourceComplete && check.Check == "source_assignment" && check.Status == plugin.DefinitionSupported {
			// An observed shape can refute a mapping, never prove that every row
			// has that shape. Keep definite Provider rejections unchanged.
			check.Status, check.Code = plugin.DefinitionUnknown, "source_schema_not_complete"
		}
		diagnostic := TaskConfigCheckDiagnostic{Status: check.Status, Code: check.Code, Check: check.Check, TargetField: check.Field, PhysicalType: check.PhysicalType, ConfigPath: "/config/target"}
		for _, binding := range definition.Fields {
			if binding.Target.Name == check.Field && binding.Source != nil {
				diagnostic.SourceField = binding.Source.Name
			}
		}
		for i, transform := range spec.Transforms {
			for j, field := range transform.Fields {
				if field.Target == check.Field {
					diagnostic.ConfigPath = "/config/transforms/" + strconv.Itoa(i) + "/fields/" + strconv.Itoa(j)
				}
			}
		}
		report.add(diagnostic)
	}
	report.Checked = append(report.Checked, "provider_write_definition")
	return finishConfigCheck(report), nil
}

func (r *TaskConfigCheckReport) add(d TaskConfigCheckDiagnostic) {
	r.Diagnostics = append(r.Diagnostics, d)
	if d.Status == plugin.DefinitionUnsupported || (d.Status == plugin.DefinitionUnknown && r.Status != plugin.DefinitionUnsupported) {
		r.Status = d.Status
	}
}
func finishConfigCheck(report *TaskConfigCheckReport) *TaskConfigCheckReport {
	report.BindingHash = ""
	report.BindingHash = configCheckHash(report)
	return report
}
func configCheckHash(value interface{}) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	// Normalize structs and maps alike to JSON objects with sorted keys. Keep
	// numeric tokens and array order; callers in other languages can reproduce it.
	var normalized interface{}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if decoder.Decode(&normalized) != nil {
		return ""
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}
func containsConfigNumber(value interface{}) bool {
	switch value := value.(type) {
	case json.Number, float64:
		return true
	case []interface{}:
		for _, child := range value {
			if containsConfigNumber(child) {
				return true
			}
		}
	case map[string]interface{}:
		for _, child := range value {
			if containsConfigNumber(child) {
				return true
			}
		}
	}
	return false
}

func (s *TaskConfigCheckService) facts(ctx context.Context, tenantID uint, role string, locator *resourcetree.ResourceLocator, querySource, replaceTarget bool) (*datatype.TableInfo, *datatype.SpatialInfo, TaskConfigCheckFact, plugin.EnginePlugin, error) {
	fact := TaskConfigCheckFact{Role: role, EngineID: locator.EngineID, Coverage: "unknown", NativeAccess: plugin.DefinitionUnknown}
	objectSource := role == "source" && !querySource && locator.Type == resourcetree.TypeObject
	if s == nil || s.engines == nil || s.metadata == nil {
		return nil, nil, fact, nil, ErrConfigCheckDependency
	}
	engine, err := s.engines(ctx, tenantID, locator.EngineID)
	if err != nil || engine == nil || engine.ID != locator.EngineID || engine.IsBuiltin || engine.LifecycleState != commonmodels.EngineLifecycleActive || engine.Version < 1 {
		return nil, nil, fact, nil, ErrConfigCheckDependency
	}
	fact.EngineType, fact.EngineVersion = engine.EngineType, engine.Version
	if objectSource && fact.EngineType != "rustfs" {
		return nil, nil, fact, nil, nil
	}
	var declared *plugin.EngineCapabilities
	if engine.Capabilities != nil && string(*engine.Capabilities) != "" {
		declared, err = plugin.ParseEngineCapabilities(string(*engine.Capabilities))
		if err != nil {
			return nil, nil, fact, nil, ErrConfigCheckDependency
		}
	}
	if objectSource {
		if declared != nil {
			fact.NativeAccess = plugin.DefinitionUnsupported
			if declared.Storage != nil && declared.Storage.Store != nil && declared.Storage.Store.StreamRead {
				fact.NativeAccess = plugin.DefinitionSupported
			}
		}
	} else if role == "source" && querySource && declared != nil {
		fact.NativeAccess = plugin.DefinitionUnsupported
		if declared.Compute != nil && declared.Compute.Query != nil && declared.Compute.Query.Supported && declared.Compute.Query.ReadSession {
			fact.NativeAccess = plugin.DefinitionSupported
		}
	} else if declared != nil && declared.Storage != nil && declared.Storage.Store != nil {
		store := declared.Storage.Store
		fact.NativeAccess = plugin.DefinitionUnsupported
		if (role == "source" && (store.TableReadSession || store.BatchRead)) || (role == "target" && (store.TableWriteSession || store.BatchWrite) && (!replaceTarget || store.Delete)) {
			fact.NativeAccess = plugin.DefinitionSupported
		}
	}
	registered, _ := plugin.Get(engine.EngineType)
	var capabilities *plugin.EngineCapabilities
	if registered != nil {
		observed := registered.Capabilities()
		capabilities = &observed
	}
	if objectSource && fact.NativeAccess == plugin.DefinitionSupported {
		if registered == nil {
			fact.NativeAccess = plugin.DefinitionUnknown
		} else if _, ok := registered.(plugin.ContentReadableProvider); !ok || capabilities.Storage == nil || capabilities.Storage.Store == nil || !capabilities.Storage.Store.StreamRead {
			fact.NativeAccess = plugin.DefinitionUnsupported
		} else if _, err := format.GetTableReaderProvider(format.FormatParquet); err != nil {
			fact.NativeAccess = plugin.DefinitionUnknown
		}
	}
	if replaceTarget {
		// replace consumes ResourceDeleteProvider before preparing or writing the
		// table, even when Meta has no snapshot. Check both the persisted promise
		// and this process's implementation without opening the target.
		if registered == nil {
			if fact.NativeAccess == plugin.DefinitionSupported {
				fact.NativeAccess = plugin.DefinitionUnknown
			}
		} else if _, ok := registered.(plugin.ResourceDeleteProvider); !ok || capabilities.Storage == nil || capabilities.Storage.Store == nil || !capabilities.Storage.Store.Delete {
			fact.NativeAccess = plugin.DefinitionUnsupported
		}
	}
	fact.EngineFactsHash = configCheckHash(struct {
		ID             uint
		TenantID       uint
		Type           string
		Version        int64
		Origin         string
		Declared       *commonmodels.JSONString
		Implementation *plugin.EngineCapabilities
	}{engine.ID, tenantID, engine.EngineType, engine.Version, engine.EngineOrigin, engine.Capabilities, capabilities})
	if fact.EngineFactsHash == "" {
		return nil, nil, fact, nil, ErrConfigCheckDependency
	}
	itemID := uint(0)
	if locator.ItemID != nil {
		itemID = *locator.ItemID
	}
	item, err := s.metadata.GetItemSnapshot(ctx, tenantID, locator.EngineID, itemID, locator.FullName())
	if errors.Is(err, commonapi.ErrNotFound) {
		fact.SnapshotHash = configCheckHash(nil)
		return nil, nil, fact, registered, nil
	}
	if err != nil || item == nil || item.ID == 0 || (itemID != 0 && item.ID != itemID) || item.TenantID != tenantID || item.EngineID != locator.EngineID || item.FullName != locator.FullName() || item.ItemType != string(locator.Type) {
		return nil, nil, fact, nil, ErrConfigCheckDependency
	}
	fact.MetaItemID, fact.ScannedAt = item.ID, item.ScannedAt
	tablePayload := commonjson.Section(item.Attributes, "type_info.table")
	spatialPayload := commonjson.Section(item.Attributes, "capabilities.spatial")
	info, spatial := datatype.TableInfoFromPayload(tablePayload, item.Name), datatype.SpatialInfoFromPayload(spatialPayload)
	var objectKind map[string]string
	if objectSource {
		section := commonjson.Section(item.Attributes, "item")
		objectKind = make(map[string]string, 3)
		for _, key := range []string{"layout", "data_type", "format"} {
			objectKind[key], _ = section[key].(string)
		}
	}
	if info != nil && len(info.Fields) > 0 && item.ScannedAt != nil && item.ScannedDepth != "" && item.ScannedDepth != "none" && capabilities != nil {
		if objectSource {
			if item.ScannedDepth == "deep" && objectKind["layout"] == "single" && objectKind["data_type"] == "table" && objectKind["format"] == "parquet" &&
				item.Fingerprint == commonmodels.GenerateItemFingerprint(locator.EngineID, locator.FullName()) {
				fact.Coverage = "complete"
			}
		} else if capabilities.EngineFamily == "dynamic_schema" {
			fact.Coverage = "sampled"
		} else if capabilities.EngineFamily == "tabular" {
			fact.Coverage = "complete"
		}
	}
	// Bind the complete persisted structural payload before normalization. Never
	// include or return preview rows, credentials, or SQL/default expressions.
	fact.SnapshotHash = configCheckHash(struct {
		ID, TenantID, EngineID, NodeID                     uint
		Type, Name, FullName, Fingerprint, Depth, Coverage string
		ScannedAt                                          *time.Time
		Table, Spatial                                     map[string]interface{}
		ObjectKind                                         map[string]string `json:",omitempty"`
	}{item.ID, item.TenantID, item.EngineID, item.NodeID, item.ItemType, item.Name, item.FullName, item.Fingerprint, item.ScannedDepth, fact.Coverage, item.ScannedAt, tablePayload, spatialPayload, objectKind})
	if fact.SnapshotHash == "" {
		return nil, nil, fact, nil, ErrConfigCheckDependency
	}
	return info, spatial, fact, registered, nil
}
