package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/jsonmap"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrScanPlanFactsUnavailable means no truthful frozen plan can be projected.
// In particular a current Engine.Version must not repair an old execution.
var ErrScanPlanFactsUnavailable = errors.New("frozen Meta scan plan facts unavailable")

func freezeScanEngineSnapshot(run *execution.TaskExecution, engine *commonmodels.Engine) {
	if run == nil || engine == nil || engine.Version < 1 || run.ExecutionConfig["engine_id"] != engine.ID || engine.TenantID == nil || int(*engine.TenantID) != run.TenantID {
		return
	}
	run.ExecutionConfig["engine_version"] = strconv.FormatInt(engine.Version, 10)
	run.ExecutionConfig["engine_type"] = engine.EngineType
}

// GetScanPlanFacts reads committed owner facts in one snapshot; no Provider,
// System API, source connection, scan, or write is performed here.
func (s *ScanExecutionService) GetScanPlanFacts(ctx context.Context, tenantID uint, executionID string) (*execution.MetaScanPlanFacts, error) {
	id, err := uuid.Parse(executionID)
	if tenantID == 0 || err != nil || id == uuid.Nil || id.String() != executionID {
		return nil, commonapi.ErrBadRequest
	}
	var facts *execution.MetaScanPlanFacts
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Scan the raw column directly: models.JSONMap would round JSON numbers.
		var row struct {
			ExecutionID     string
			SourceTaskID    *string
			ExecutionConfig string
		}
		if err := tx.Table((execution.TaskExecution{}).TableName()).
			Select("execution_id, source_task_id, execution_config").
			Where("tenant_id = ? AND module = ? AND task_type = ? AND execution_id = ?", tenantID, execution.ModuleMeta, execution.TaskTypeScan, executionID).Take(&row).Error; err != nil {
			return err
		}
		raw := json.RawMessage(row.ExecutionConfig)
		config, err := jsonmap.DecodeObjectStrict(raw, execution.MaxExecutionConfigBytes, 64)
		if err != nil {
			return ErrScanPlanFactsUnavailable
		}
		digest, err := execution.ComputeMetaScanExecutionPlanDigest(raw)
		if err != nil {
			return ErrScanPlanFactsUnavailable
		}
		engineID, ok := scanPlanUint(config["engine_id"])
		version, versionOK := config["engine_version"].(string)
		depth, depthOK := config["scan_depth"].(string)
		if !ok || !versionOK || !depthOK {
			return ErrScanPlanFactsUnavailable
		}
		facts = &execution.MetaScanPlanFacts{SchemaVersion: execution.MetaScanPlanFactsSchemaV1, ExecutionID: executionID, TaskID: row.SourceTaskID, EngineID: engineID, EngineVersion: version, ScanDepth: depth, ExecutionPlanDigest: digest, BasisState: authorizationfacts.AuthorizationBasisIncomplete}
		if encoding, exists := config["encoding"]; exists {
			value, ok := encoding.(string)
			if !ok {
				return ErrScanPlanFactsUnavailable
			}
			facts.ReadOptions.Encoding = value
		}
		if header, exists := config["has_header"]; exists {
			value, ok := header.(bool)
			if !ok {
				return ErrScanPlanFactsUnavailable
			}
			facts.ReadOptions.HasHeader = &value
		}
		if row.SourceTaskID != nil {
			frozen, ok := config["task_definition_digest"].(string)
			if !ok || !authorizationfacts.ValidItemFingerprint(frozen) {
				return ErrScanPlanFactsUnavailable
			}
			facts.TaskDefinitionDigest = &frozen
			definition, err := scanTaskFunctionalDefinition(tx, tenantID, *row.SourceTaskID)
			if err != nil {
				return err
			}
			value, err := execution.ComputeMetaScanTaskDefinitionDigest(definition)
			if err != nil {
				return ErrScanPlanFactsUnavailable
			}
			if value != frozen {
				facts.BasisState = authorizationfacts.AuthorizationBasisNeedsRefresh
				if facts.Validate() != nil {
					return ErrScanPlanFactsUnavailable
				}
				return nil
			}
		}
		if err := projectScanSourceScope(ctx, tx, tenantID, config, facts); err != nil {
			return err
		}
		if err := facts.Validate(); err != nil {
			return ErrScanPlanFactsUnavailable
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return facts, nil
}

func scanPlanUint(value any) (uint, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(string(number), 10, 63)
	return uint(n), err == nil && n > 0 && strconv.FormatUint(n, 10) == string(number)
}

// The real task's explicit functional fields are distinct from its display,
// scheduler bookkeeping, creator and last-execution fields. Raw JSON is retained.
func scanTaskFunctionalDefinition(tx *gorm.DB, tenantID uint, taskID string) (json.RawMessage, error) {
	n, err := strconv.ParseUint(taskID, 10, 63)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != taskID {
		return nil, ErrScanPlanFactsUnavailable
	}
	var row struct {
		EngineID   uint
		Scope      string
		Parameters string
	}
	if err := tx.Model(&models.ScanTask{}).Select("engine_id, scope, parameters").Where("tenant_id = ? AND id = ?", tenantID, taskID).Take(&row).Error; err != nil {
		return nil, err
	}
	scope, err := scanTaskConfigObject(row.Scope)
	if err != nil {
		return nil, ErrScanPlanFactsUnavailable
	}
	parameters, err := scanTaskConfigObject(row.Parameters)
	if err != nil {
		return nil, ErrScanPlanFactsUnavailable
	}
	// Select the existing functional contract explicitly; arbitrary task extras
	// (including credentials) neither execute nor become authorization facts.
	selector := make(map[string]any)
	for _, key := range []string{"type", "catalog_paths", "ref_groups"} {
		if value, exists := scope[key]; exists {
			selector[key] = value
		}
	}
	options := make(map[string]any)
	for _, key := range []string{"scan_depth", "force", "encoding", "has_header"} {
		if value, exists := parameters[key]; exists {
			options[key] = value
		}
	}
	return json.Marshal(struct {
		EngineID   uint           `json:"engine_id"`
		Scope      map[string]any `json:"scope"`
		Parameters map[string]any `json:"parameters"`
	}{row.EngineID, selector, options})
}

func scanTaskConfigObject(raw string) (map[string]any, error) {
	if raw == "" || raw == "null" {
		return map[string]any{}, nil
	}
	return jsonmap.DecodeObjectStrict([]byte(raw), execution.MaxExecutionConfigBytes, 64)
}

func freezeScanTaskDefinition(tx *gorm.DB, run *execution.TaskExecution) error {
	if run == nil || run.SourceTaskID == nil {
		return ErrScanPlanFactsUnavailable
	}
	raw, err := scanTaskFunctionalDefinition(tx, uint(run.TenantID), *run.SourceTaskID)
	if err != nil {
		return err
	}
	digest, err := execution.ComputeMetaScanTaskDefinitionDigest(raw)
	if err != nil {
		return ErrScanPlanFactsUnavailable
	}
	run.ExecutionConfig["task_definition_digest"] = digest
	return nil
}

func projectScanSourceScope(ctx context.Context, tx *gorm.DB, tenantID uint, config map[string]any, facts *execution.MetaScanPlanFacts) error {
	allowed := map[string]bool{"engine_id": true, "engine_version": true, "engine_type": true, "task_definition_digest": true, "storage_type": true, "targets": true, "catalog_paths": true, "ref_groups": true, "item_id": true, "scan_depth": true, "force": true, "source": true, "planned_run_at": true, "encoding": true, "has_header": true}
	for key := range config {
		if !allowed[key] {
			return nil
		}
	}
	// These legacy selectors do not prove complete typed anchors. Do not call
	// ResolvePath/ListChildren, guess from strings, or fall back to Engine scope.
	for _, key := range []string{"targets", "catalog_paths", "ref_groups"} {
		if value := config[key]; value != nil {
			values, ok := value.([]any)
			if !ok || len(values) > 0 {
				return nil
			}
		}
	}
	engineType, ok := config["engine_type"].(string)
	if !ok || engineType == "" {
		return nil
	}
	p, err := plugin.Get(engineType)
	if err != nil {
		facts.BasisState = authorizationfacts.AuthorizationBasisUnsupported
		return nil
	}
	model := scanflow.EngineCatalogModelForPlugin(p)
	if model == nil {
		facts.BasisState = authorizationfacts.AuthorizationBasisUnsupported
		return nil
	}
	if value, exists := config["item_id"]; exists {
		itemID, ok := scanPlanUint(value)
		if !ok {
			return nil
		}
		var item models.MetaItem
		if err := tx.Where("tenant_id = ? AND engine_id = ? AND id = ?", tenantID, facts.EngineID, itemID).First(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		// The nested owner read uses this same transaction/connection snapshot.
		itemFacts, err := NewMetadataQueryService(tx).GetDataItemAuthorizationFacts(ctx, tenantID, item.Fingerprint)
		if err != nil {
			return err
		}
		if itemFacts.BasisState != authorizationfacts.AuthorizationBasisComplete || itemFacts.Boundary == nil || itemFacts.Boundary.FileComposition != nil {
			facts.BasisState = itemFacts.BasisState
			// Composition targets need the authoritative complete composition;
			// a single primary anchor must not masquerade as that boundary.
			if facts.BasisState == authorizationfacts.AuthorizationBasisComplete {
				facts.BasisState = authorizationfacts.AuthorizationBasisUnsupported
			}
			return nil
		}
		if !scanPlanNativePathMatchesModel(itemFacts.Boundary.NativeTarget, *model) {
			facts.BasisState = authorizationfacts.AuthorizationBasisNeedsRefresh
			return nil
		}
		facts.SourceScope = &execution.MetaScanSourceScope{Mode: "exact", Anchors: []plugin.EngineCatalogPath{itemFacts.Boundary.NativeTarget}}
		facts.BasisState = authorizationfacts.AuthorizationBasisComplete
		return nil
	}
	facts.SourceScope = &execution.MetaScanSourceScope{Mode: "full", Anchors: []plugin.EngineCatalogPath{plugin.EngineCatalogRootPath(*model, facts.EngineID)}}
	facts.BasisState = authorizationfacts.AuthorizationBasisComplete
	return nil
}

// This projection currently supports only the native two-level item facts
// contract. It must not infer other catalog models from a dotted display path.
func scanPlanNativePathMatchesModel(path plugin.EngineCatalogPath, model plugin.EngineCatalogModelSpec) bool {
	if len(model.Levels) != 2 || len(path.Segments) != 3 || path.Version != model.PathVersion || path.Segments[0].Term != model.RootTerm {
		return false
	}
	for i, level := range model.Levels {
		segment := path.Segments[i+1]
		if segment.Term != level.Term {
			return false
		}
		matched := false
		for _, kind := range level.Kinds {
			if segment.Kind == kind {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return model.Levels[0].Role == plugin.EngineCatalogRoleBranch && model.Levels[1].Role == plugin.EngineCatalogRoleLeaf
}
