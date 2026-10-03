package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
)

func (p relationPair) granularity() string {
	if p.fields.SourceFieldName != "" {
		return "field"
	}
	return "item"
}

func lineageRefSnapshot(ref *resolvedRef) commonModels.JSONMap {
	snapshot := itemSnapshot(ref.item)
	if ref.ref.SchemaSnapshot != nil {
		snapshot["schema_snapshot"] = ref.ref.SchemaSnapshot
	}
	return snapshot
}

func knownLineageWriteMode(mode string) bool {
	return mode == "replace" || mode == "append" || mode == "upsert" || mode == "cdc"
}

func fieldOperationPairs(inputs, outputs []*resolvedRef, operations []models.LineageOperation) ([]relationPair, error) {
	byPort := func(refs []*resolvedRef) map[string]*resolvedRef {
		result := map[string]*resolvedRef{}
		for _, ref := range refs {
			if _, exists := result[ref.ref.Port]; exists {
				result[ref.ref.Port] = nil
			} else {
				result[ref.ref.Port] = ref
			}
		}
		return result
	}
	in, out := byPort(inputs), byPort(outputs)
	var pairs []relationPair
	for index := range operations {
		op := &operations[index]
		if op.FieldLineageStatus != "" && op.FieldLineageStatus != "unavailable" && op.FieldLineageStatus != "complete" {
			return nil, fmt.Errorf("lineage_facts field lineage status is invalid")
		}
		if op.FieldLineageStatus != "complete" {
			if len(op.FieldMappings) != 0 {
				return nil, fmt.Errorf("lineage_facts mappings require complete field lineage")
			}
			continue
		}
		if op.Kind != "derive" && op.Kind != "reference" {
			return nil, fmt.Errorf("lineage_facts field relation kind is invalid")
		}
		contains := func(ports []string, port string) bool {
			for _, value := range ports {
				if value == port {
					return true
				}
			}
			return false
		}
		seen := map[string]bool{}
		targetGenerated := map[string]bool{}
		for _, mapping := range op.FieldMappings {
			target := out[mapping.OutputPort]
			if target == nil || !contains(op.OutputPorts, mapping.OutputPort) || target.ref.SchemaSnapshot.Validate() != nil || !target.ref.SchemaSnapshot.HasField(mapping.TargetField) {
				return nil, fmt.Errorf("lineage_facts target field snapshot is invalid")
			}
			targetKey := fmt.Sprintf("%q:%q", mapping.OutputPort, mapping.TargetField)
			generated := mapping.Transformation == "generated"
			if previous, exists := targetGenerated[targetKey]; exists && (previous || generated) {
				return nil, fmt.Errorf("lineage_facts generated field mapping conflicts with another mapping")
			}
			targetGenerated[targetKey] = generated
			if mapping.Transformation == "generated" {
				if mapping.SourceField != "" || mapping.InputPort != "" {
					return nil, fmt.Errorf("lineage_facts generated field cannot have a source")
				}
				continue
			}
			source := in[mapping.InputPort]
			if source == nil || !contains(op.InputPorts, mapping.InputPort) || source.ref.SchemaSnapshot.Validate() != nil || !source.ref.SchemaSnapshot.HasField(mapping.SourceField) {
				return nil, fmt.Errorf("lineage_facts source field snapshot is invalid")
			}
			if mapping.Transformation != "direct" && mapping.Transformation != "derived" {
				return nil, fmt.Errorf("lineage_facts field transformation is invalid")
			}
			fields := models.LineageFieldEndpoints{SourceFieldName: mapping.SourceField, TargetFieldName: mapping.TargetField, SourceSchemaHash: source.ref.SchemaSnapshot.Hash, TargetSchemaHash: target.ref.SchemaSnapshot.Hash}
			key := fmt.Sprintf("%d:%d:%q:%q:%s:%s", source.item.ID, target.item.ID, fields.SourceFieldName, fields.TargetFieldName, fields.SourceSchemaHash, fields.TargetSchemaHash)
			if seen[key] {
				return nil, fmt.Errorf("lineage_facts duplicate field mapping")
			}
			seen[key] = true
			pairs = append(pairs, relationPair{source: source, target: target, kind: op.Kind, fields: fields, transformation: mapping.Transformation, operation: op})
		}
	}
	return pairs, nil
}

func (s *LineageService) hasNewerTargetObservation(ctx context.Context, tenantID, targetID uint, at time.Time) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&models.LineageObservation{}).Where("tenant_id = ? AND target_item_id = ? AND relation_kind = 'derive' AND observed_at > ?", tenantID, targetID, at).Count(&count).Error
	return count > 0, err
}

// Lock each target before checking evidence or closing mappings. A replay only
// reads its immutable observations and cannot reactivate a superseded mapping.
func (s *LineageService) prepareLineageProjection(ctx context.Context, tenantID uint, executionID string, outputs []*resolvedRef, at time.Time) error {
	ordered := append([]*resolvedRef(nil), outputs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].item.ID < ordered[j].item.ID })
	for _, output := range ordered {
		if s.db.Dialector.Name() == "postgres" {
			if err := s.db.Exec("SELECT pg_advisory_xact_lock(?, ?)", int32(tenantID), int32(output.item.ID)).Error; err != nil {
				return err
			}
		}
		if output.ref.WriteMode != "replace" {
			continue
		}
		newer, err := s.hasNewerTargetObservation(ctx, tenantID, output.item.ID, at)
		if err != nil {
			return err
		}
		if newer {
			continue
		}
		var already int64
		if err := s.db.WithContext(ctx).Model(&models.LineageObservation{}).Where("tenant_id = ? AND execution_id = ? AND target_item_id = ?", tenantID, executionID, output.item.ID).Count(&already).Error; err != nil {
			return err
		}
		if already > 0 {
			continue
		}
		if err := s.db.WithContext(ctx).Model(&models.LineageItemRelation{}).Where("tenant_id = ? AND target_item_id = ? AND relation_kind = 'derive' AND granularity = 'field' AND status <> 'closed' AND last_observed_at <= ?", tenantID, output.item.ID, at).Updates(map[string]interface{}{"status": "closed", "closed_at": at, "updated_at": at}).Error; err != nil {
			return err
		}
	}
	return nil
}

func decodeJSONValue(raw interface{}, target interface{}) error {
	payload, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, target)
}

func snapshotFromEvidence(raw interface{}) *commonExecution.LineageSchemaSnapshot {
	// JSONMap values loaded from PostgreSQL and SQLite both have JSON payloads.
	var snapshot commonExecution.LineageSchemaSnapshot
	if decodeJSONValue(raw, &snapshot) != nil || snapshot.Validate() != nil {
		return nil
	}
	return &snapshot
}

func fieldProjectionStatus(pair relationPair) string {
	if pair.granularity() != "field" {
		return "active"
	}
	for _, ref := range []*resolvedRef{pair.source, pair.target} {
		fields, err := metaquery.FieldsFromMetaItem(ref.item)
		if err != nil {
			return "stale"
		}
		current, err := commonExecution.NewLineageSchemaSnapshot(fields)
		if err != nil || ref.ref.SchemaSnapshot == nil || current.Hash != ref.ref.SchemaSnapshot.Hash {
			return "stale"
		}
	}
	return "active"
}
