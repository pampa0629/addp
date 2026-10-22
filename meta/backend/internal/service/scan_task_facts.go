package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/common/jsonmap"
	"gorm.io/gorm"
)

// GetScanTaskFacts only reads Meta-owned persisted task fields. It neither
// starts an execution nor obtains a source connection to prepare an approval.
func (s *ScanExecutionService) GetScanTaskFacts(ctx context.Context, tenantID uint, taskID string) (*execution.MetaScanTaskFacts, error) {
	id, err := strconv.ParseUint(taskID, 10, 63)
	if tenantID == 0 || err != nil || id == 0 || strconv.FormatUint(id, 10) != taskID {
		return nil, commonapi.ErrBadRequest
	}
	var result *execution.MetaScanTaskFacts
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		raw, err := scanTaskFunctionalDefinition(tx, tenantID, taskID)
		if err != nil {
			return err
		}
		definition, err := jsonmap.DecodeObjectStrict(raw, execution.MaxExecutionConfigBytes, 64)
		if err != nil {
			return ErrScanPlanFactsUnavailable
		}
		engineID, ok := scanPlanUint(definition["engine_id"])
		if !ok {
			return ErrScanPlanFactsUnavailable
		}
		// Use the same producer defaults and strict execution/definition binding
		// as the frozen-plan reader; unsupported selectors stay unsupported.
		params := definition["parameters"].(map[string]any)
		config := map[string]any{"engine_id": definition["engine_id"], "scan_depth": "deep", "force": false}
		for key, value := range params {
			config[key] = value
		}
		if !scanExecutionMatchesTask(raw, config, "manual") {
			return ErrScanPlanFactsUnavailable
		}
		digest, err := execution.ComputeMetaScanTaskDefinitionDigest(raw)
		if err != nil {
			return ErrScanPlanFactsUnavailable
		}
		result = &execution.MetaScanTaskFacts{SchemaVersion: execution.MetaScanTaskFactsSchemaV1, TaskID: taskID,
			TaskDefinitionDigest: digest, EngineID: engineID, ScopeKind: "engine"}
		// Binding already checked the exact types above; marshal through the
		// explicit options DTO without exposing the task's raw configuration.
		options, err := json.Marshal(params)
		if err != nil {
			return ErrScanPlanFactsUnavailable
		}
		if err := json.Unmarshal(options, &result.ReadOptions); err != nil {
			return ErrScanPlanFactsUnavailable
		}
		result.ScanDepth = config["scan_depth"].(string)
		result.Force = config["force"].(bool)
		if result.Validate() != nil {
			return ErrScanPlanFactsUnavailable
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}
