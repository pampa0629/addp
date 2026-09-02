package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

type LineageService struct {
	db            *gorm.DB
	engineCatalog lineageEngineCatalog
}

type lineageEngineCatalog interface {
	GetEnginesByTenant(tenantID uint) ([]*commonModels.Engine, error)
}

func NewLineageService(db *gorm.DB, engineCatalog lineageEngineCatalog) *LineageService {
	return &LineageService{db: db, engineCatalog: engineCatalog}
}

// RunCollector periodically consumes successful executions that declare lineage facts.
func (s *LineageService) RunCollector(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.CollectPendingExecutions(ctx, 500); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("[Meta Lineage] collect pending executions failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *LineageService) CollectPendingExecutions(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 5000 {
		return 0, fmt.Errorf("limit must be between 1 and 5000")
	}
	var executions []commonExecution.TaskExecution
	if err := s.db.WithContext(ctx).
		Where("status = ? AND metadata IS NOT NULL AND metadata ? 'lineage_facts'", commonExecution.ExecutionStatusSuccess).
		Order("created_at ASC, id ASC").Limit(limit).Find(&executions).Error; err != nil {
		return 0, err
	}
	collected := 0
	for _, execution := range executions {
		result, err := s.CollectExecution(ctx, uint(execution.TenantID), execution.ExecutionID)
		if err != nil {
			return collected, fmt.Errorf("collect execution %s: %w", execution.ExecutionID, err)
		}
		collected += result.Observed
	}
	return collected, nil
}

type LineageCollectionResult = models.LineageCollectionResult

// CollectExecution consumes only successful executions with top-level metadata.lineage_facts.
func (s *LineageService) CollectExecution(ctx context.Context, tenantID uint, executionID string) (LineageCollectionResult, error) {
	var execution commonExecution.TaskExecution
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND execution_id = ?", tenantID, executionID).
		First(&execution).Error; err != nil {
		return LineageCollectionResult{}, err
	}
	if execution.Status != commonExecution.ExecutionStatusSuccess {
		return LineageCollectionResult{}, fmt.Errorf("execution %s is not successful", executionID)
	}
	facts, err := decodeLineageFacts(execution.Metadata)
	if err != nil {
		return LineageCollectionResult{}, err
	}
	return s.collectFacts(ctx, tenantID, executionID, execution.Module, facts)
}

// RecordServicePublication records one immutable publication observation and updates the active projection.
func (s *LineageService) RecordServicePublication(ctx context.Context, tenantID uint, request models.RecordServicePublicationRequest) error {
	if request.ServiceID == 0 || strings.TrimSpace(request.PublishedRevision) == "" {
		return fmt.Errorf("service_id and published_revision are required")
	}
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, dependency := range request.Dependencies {
			if dependency.SourceItemID == 0 {
				continue
			}
			kind := strings.TrimSpace(dependency.DependencyKind)
			if kind == "" {
				kind = "query"
			}
			granularity := strings.TrimSpace(dependency.Granularity)
			if granularity == "" {
				granularity = "item"
			}
			observation := models.LineageObservation{
				TenantID: tenantID, RelationKind: "serve", Granularity: granularity,
				SourceItemID: uintPtr(dependency.SourceItemID), ServiceID: uintPtr(request.ServiceID),
				PublishedRevision: stringPtr(request.PublishedRevision), ProducerModule: commonExecution.ModuleService,
				CaptureMethod: "declared", SourceSnapshot: commonModels.JSONMap{"item_id": dependency.SourceItemID},
				Evidence: commonModels.JSONMap{"service_id": request.ServiceID, "published_revision": request.PublishedRevision, "dependency_hash": request.DependencyHash}, ObservedAt: now,
			}
			if len(dependency.DependencyFields) > 0 {
				observation.Evidence["dependency_fields"] = dependency.DependencyFields
			}
			var existing models.LineageObservation
			if err := tx.Where("tenant_id = ? AND relation_kind = 'serve' AND source_item_id = ? AND service_id = ? AND published_revision = ?", tenantID, dependency.SourceItemID, request.ServiceID, request.PublishedRevision).First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Create(&observation).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			var projection models.LineageServiceDependency
			err := tx.Where("tenant_id = ? AND source_item_id = ? AND service_id = ? AND published_revision = ? AND granularity = ? AND status <> 'closed'", tenantID, dependency.SourceItemID, request.ServiceID, request.PublishedRevision, granularity).First(&projection).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				projection = models.LineageServiceDependency{TenantID: tenantID, SourceItemID: dependency.SourceItemID, ServiceID: request.ServiceID, PublishedRevision: request.PublishedRevision, DependencyHash: stringPtrIfNotEmpty(request.DependencyHash), DependencyKind: kind, Granularity: granularity, DependencyFields: dependency.DependencyFields, Status: "active", FirstObservedAt: now, LastObservedAt: now}
				if err := tx.Create(&projection).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if err := tx.Model(&projection).Updates(map[string]interface{}{"dependency_hash": request.DependencyHash, "dependency_kind": kind, "dependency_fields": commonModels.JSONMap(dependency.DependencyFields), "status": "active", "last_observed_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.LineageServiceDependency{}).Where("tenant_id = ? AND service_id = ? AND published_revision = ? AND status <> 'closed'", tenantID, request.ServiceID, request.PublishedRevision).Where("last_observed_at < ?", now).Updates(map[string]interface{}{"status": "closed", "closed_at": now, "updated_at": now}).Error
	})
}

func stringPtrIfNotEmpty(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func decodeLineageFacts(metadata commonModels.JSONMap) (*models.LineageFacts, error) {
	raw, ok := metadata["lineage_facts"]
	if !ok || raw == nil {
		return &models.LineageFacts{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal lineage_facts: %w", err)
	}
	var facts models.LineageFacts
	if err := json.Unmarshal(b, &facts); err != nil {
		return nil, fmt.Errorf("decode lineage_facts: %w", err)
	}
	if facts.SchemaVersion != commonExecution.LineageFactsSchemaVersion {
		return nil, fmt.Errorf("unsupported lineage_facts schema_version %q", facts.SchemaVersion)
	}
	return &facts, nil
}

func (s *LineageService) collectFacts(ctx context.Context, tenantID uint, executionID, producer string, facts *models.LineageFacts) (LineageCollectionResult, error) {
	result := LineageCollectionResult{}
	inputs, err := s.resolveRefs(ctx, tenantID, facts.Inputs)
	if err != nil {
		return result, err
	}
	outputs, err := s.resolveRefs(ctx, tenantID, facts.Outputs)
	if err != nil {
		return result, err
	}
	if len(inputs) == 0 || len(outputs) == 0 {
		return result, nil
	}

	pairs := operationPairs(inputs, outputs, facts.Operations)
	for _, pair := range pairs {
		if pair.source == nil || pair.target == nil {
			result.Skipped++
			continue
		}
		observed, err := s.persistItemRelation(ctx, tenantID, executionID, producer, pair)
		if err != nil {
			return result, err
		}
		if observed {
			result.Observed++
		} else {
			result.Skipped++
		}
	}
	if err := s.closeReplacedInputs(ctx, tenantID, pairs, outputs); err != nil {
		return result, err
	}
	return result, nil
}

func (s *LineageService) closeReplacedInputs(ctx context.Context, tenantID uint, pairs []relationPair, outputs []*resolvedRef) error {
	now := time.Now().UTC()
	for _, output := range outputs {
		if output.ref.WriteMode != "replace" {
			continue
		}
		currentSources := make([]uint, 0)
		for _, pair := range pairs {
			if pair.target != nil && pair.source != nil && pair.target.item.ID == output.item.ID && pair.kind == "derive" {
				currentSources = append(currentSources, pair.source.item.ID)
			}
		}
		query := s.db.WithContext(ctx).Model(&models.LineageItemRelation{}).
			Where("tenant_id = ? AND target_item_id = ? AND relation_kind = 'derive' AND status <> 'closed'", tenantID, output.item.ID)
		if len(currentSources) > 0 {
			query = query.Where("source_item_id NOT IN ?", currentSources)
		}
		if err := query.Updates(map[string]interface{}{"status": "closed", "closed_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
	}
	return nil
}

type resolvedRef struct {
	ref  models.LineageResourceRef
	item models.MetaItem
}

type relationPair struct {
	source *resolvedRef
	target *resolvedRef
	kind   string
}

func (s *LineageService) resolveRefs(ctx context.Context, tenantID uint, refs []models.LineageResourceRef) ([]*resolvedRef, error) {
	resolved := make([]*resolvedRef, 0, len(refs))
	for _, ref := range refs {
		var item models.MetaItem
		query := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
		if ref.ItemID != nil {
			query = query.Where("id = ?", *ref.ItemID)
		} else if strings.TrimSpace(ref.ItemFingerprint) != "" {
			query = query.Where("fingerprint = ?", ref.ItemFingerprint)
		} else if strings.TrimSpace(ref.Locator) != "" {
			loc, parseErr := resourcetree.ParseURI(strings.TrimSpace(ref.Locator))
			if parseErr != nil || loc == nil || loc.EngineID == 0 || len(loc.Path) == 0 {
				continue
			}
			catalogPath := strings.Join(loc.Path, "/")
			query = query.Where("engine_id = ? AND (full_name = ? OR full_name = ?)", loc.EngineID, catalogPath, strings.Join(loc.Path, "."))
		} else {
			continue
		}
		if err := query.First(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		resolved = append(resolved, &resolvedRef{ref: ref, item: item})
	}
	return resolved, nil
}

func operationPairs(inputs, outputs []*resolvedRef, operations []models.LineageOperation) []relationPair {
	if len(operations) == 0 {
		pairs := make([]relationPair, 0, len(inputs)*len(outputs))
		for _, input := range inputs {
			for _, output := range outputs {
				pairs = append(pairs, relationPair{source: input, target: output, kind: "derive"})
			}
		}
		return pairs
	}
	byInputPort := make(map[string][]*resolvedRef)
	byOutputPort := make(map[string][]*resolvedRef)
	for _, input := range inputs {
		byInputPort[input.ref.Port] = append(byInputPort[input.ref.Port], input)
	}
	for _, output := range outputs {
		byOutputPort[output.ref.Port] = append(byOutputPort[output.ref.Port], output)
	}
	var pairs []relationPair
	for _, operation := range operations {
		kind := strings.ToLower(strings.TrimSpace(operation.Kind))
		if kind != "derive" && kind != "reference" {
			continue
		}
		for _, inputPort := range operation.InputPorts {
			for _, outputPort := range operation.OutputPorts {
				for _, input := range byInputPort[inputPort] {
					for _, output := range byOutputPort[outputPort] {
						pairs = append(pairs, relationPair{source: input, target: output, kind: kind})
					}
				}
			}
		}
	}
	return pairs
}

func (s *LineageService) persistItemRelation(ctx context.Context, tenantID uint, executionID, producer string, pair relationPair) (bool, error) {
	now := time.Now().UTC()
	var existing models.LineageObservation
	err := s.db.WithContext(ctx).Where(
		"tenant_id = ? AND execution_id = ? AND relation_kind = ? AND source_item_id = ? AND target_item_id = ?",
		tenantID, executionID, pair.kind, pair.source.item.ID, pair.target.item.ID,
	).First(&existing).Error
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}

	observation := models.LineageObservation{
		TenantID:       tenantID,
		RelationKind:   pair.kind,
		Granularity:    "item",
		SourceItemID:   uintPtr(pair.source.item.ID),
		TargetItemID:   uintPtr(pair.target.item.ID),
		ExecutionID:    stringPtr(executionID),
		ProducerModule: producer,
		CaptureMethod:  "declared",
		SourceSnapshot: itemSnapshot(pair.source.item),
		TargetSnapshot: itemSnapshot(pair.target.item),
		Evidence: commonModels.JSONMap{
			"execution_id": executionID,
		},
		ObservedAt: now,
	}
	return true, s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&observation).Error; err != nil {
			return err
		}
		var relation models.LineageItemRelation
		relationErr := tx.Where(
			"tenant_id = ? AND source_item_id = ? AND target_item_id = ? AND relation_kind = ? AND granularity = ? AND status <> 'closed'",
			tenantID, pair.source.item.ID, pair.target.item.ID, pair.kind, "item",
		).First(&relation).Error
		if errors.Is(relationErr, gorm.ErrRecordNotFound) {
			relation = models.LineageItemRelation{
				TenantID: tenantID, SourceItemID: pair.source.item.ID, TargetItemID: pair.target.item.ID,
				RelationKind: pair.kind, Granularity: "item", Status: "active", FirstObservedAt: now, LastObservedAt: now,
			}
			if pair.target.ref.WriteMode != "" {
				relation.WriteMode = stringPtr(pair.target.ref.WriteMode)
			}
			return tx.Create(&relation).Error
		}
		if relationErr != nil {
			return relationErr
		}
		updates := map[string]interface{}{"last_observed_at": now, "status": "active", "updated_at": now}
		if pair.target.ref.WriteMode != "" {
			updates["write_mode"] = pair.target.ref.WriteMode
		}
		return tx.Model(&relation).Updates(updates).Error
	})
}

func itemSnapshot(item models.MetaItem) commonModels.JSONMap {
	return commonModels.JSONMap{"item_id": item.ID, "fingerprint": item.Fingerprint, "full_name": item.FullName, "item_type": item.ItemType}
}

func uintPtr(value uint) *uint       { return &value }
func stringPtr(value string) *string { return &value }

// GetGraph returns the current item projection and service dependencies.
func (s *LineageService) GetGraph(ctx context.Context, tenantID uint, request models.LineageGraphRequest) (models.LineageGraphResponse, error) {
	if request.Depth < 0 || request.Depth > 20 {
		return models.LineageGraphResponse{}, fmt.Errorf("depth must be between 0 and 20")
	}
	if request.Limit < 1 || request.Limit > 500 {
		return models.LineageGraphResponse{}, fmt.Errorf("limit must be between 1 and 500")
	}
	if request.Direction != "upstream" && request.Direction != "downstream" && request.Direction != "both" {
		return models.LineageGraphResponse{}, fmt.Errorf("direction must be upstream, downstream or both")
	}
	if request.SubjectKind != "data_item" && request.SubjectKind != "published_service" {
		return models.LineageGraphResponse{}, fmt.Errorf("subject_kind must be data_item or published_service")
	}
	if request.SubjectKind == "data_item" && request.ItemID == nil {
		return models.LineageGraphResponse{}, fmt.Errorf("item_id is required for data_item")
	}
	if request.SubjectKind == "published_service" && (request.ServiceID == nil || strings.TrimSpace(request.Revision) == "") {
		return models.LineageGraphResponse{}, fmt.Errorf("service_id and revision are required for published_service")
	}

	response := models.LineageGraphResponse{AsOf: request.AsOf}
	itemIDs := make(map[uint]struct{})
	if request.SubjectKind == "data_item" {
		itemIDs[*request.ItemID] = struct{}{}
		response.Subject = models.LineageNode{Kind: "data_item", ItemID: request.ItemID}
	} else {
		response.Subject = models.LineageNode{Kind: "published_service", ServiceID: request.ServiceID, PublishedRevision: request.Revision}
		var deps []models.LineageServiceDependency
		query := s.db.WithContext(ctx).Where("tenant_id = ? AND service_id = ? AND published_revision = ? AND status <> 'closed'", tenantID, *request.ServiceID, request.Revision)
		if request.AsOf != nil {
			query = query.Where("last_observed_at <= ?", *request.AsOf)
		}
		if err := query.Limit(request.Limit).Find(&deps).Error; err != nil {
			return response, err
		}
		for _, dep := range deps {
			itemIDs[dep.SourceItemID] = struct{}{}
		}
	}

	if request.SubjectKind == "data_item" && request.Depth > 0 {
		seed := *request.ItemID
		var walked []struct {
			ItemID uint `gorm:"column:item_id"`
		}
		directionPredicate := "r.source_item_id = w.item_id OR r.target_item_id = w.item_id"
		if request.Direction == "upstream" {
			directionPredicate = "r.target_item_id = w.item_id"
		} else if request.Direction == "downstream" {
			directionPredicate = "r.source_item_id = w.item_id"
		}
		statusClause := "r.status = 'active'"
		if request.AsOf != nil {
			statusClause = "(r.status = 'active' OR (r.status = 'closed' AND r.closed_at > ?))"
		}
		args := []interface{}{seed}
		if request.AsOf != nil {
			args = append(args, *request.AsOf)
			args = append(args, *request.AsOf)
		}
		asOfClause := ""
		if request.AsOf != nil {
			asOfClause = " AND r.last_observed_at <= ?"
			args = append(args, *request.AsOf)
		}
		args = append(args, request.Depth)
		query := fmt.Sprintf(`WITH RECURSIVE walk(item_id, depth) AS (
			SELECT CAST(? AS BIGINT), 0
			UNION
			SELECT CASE WHEN r.source_item_id = w.item_id THEN r.target_item_id ELSE r.source_item_id END, w.depth + 1
			FROM walk w
			JOIN meta.lineage_item_relations r ON (%s) AND %s%s
			WHERE w.depth < ?
		) SELECT item_id FROM walk`, directionPredicate, statusClause, asOfClause)
		if err := s.db.WithContext(ctx).Raw(query, args...).Scan(&walked).Error; err != nil {
			return response, err
		}
		for _, row := range walked {
			itemIDs[row.ItemID] = struct{}{}
		}
	}

	ids := make([]uint, 0, len(itemIDs))
	for id := range itemIDs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) > request.Limit {
		response.Truncated = true
		ids = ids[:request.Limit]
	}
	var items []models.MetaItem
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, ids).Find(&items).Error; err != nil {
			return response, err
		}
	}
	engineNames, err := s.lineageEngineNames(tenantID, items)
	if err != nil {
		return response, err
	}
	nodesByID := make(map[uint]models.LineageNode, len(items))
	activeItemIDs := make([]uint, 0, len(items))
	for _, item := range items {
		node := models.LineageNode{
			Kind: "data_item", ItemID: uintPtr(item.ID), ItemFingerprint: item.Fingerprint,
			EngineID: uintPtr(item.EngineID), EngineName: engineNames[item.EngineID],
			ItemType: item.ItemType, Name: item.Name, FullName: item.FullName,
		}
		nodesByID[item.ID] = node
		activeItemIDs = append(activeItemIDs, item.ID)
		response.Nodes = append(response.Nodes, node)
	}
	sort.Slice(activeItemIDs, func(i, j int) bool { return activeItemIDs[i] < activeItemIDs[j] })
	if request.SubjectKind == "data_item" {
		subject, ok := nodesByID[*request.ItemID]
		if !ok {
			return response, gorm.ErrRecordNotFound
		}
		response.Subject = subject
	}
	return s.populateGraphEdges(ctx, tenantID, request, activeItemIDs, response, nodesByID)
}

func (s *LineageService) lineageEngineNames(tenantID uint, items []models.MetaItem) (map[uint]string, error) {
	if len(items) == 0 {
		return map[uint]string{}, nil
	}
	if s.engineCatalog == nil {
		return nil, fmt.Errorf("lineage engine catalog is not configured")
	}
	engines, err := s.engineCatalog.GetEnginesByTenant(tenantID)
	if err != nil {
		return nil, fmt.Errorf("load lineage engines: %w", err)
	}
	names := make(map[uint]string, len(engines))
	for _, engine := range engines {
		if engine != nil {
			names[engine.ID] = engine.Name
		}
	}
	for _, item := range items {
		if strings.TrimSpace(names[item.EngineID]) == "" {
			return nil, fmt.Errorf("engine %d not found for lineage item %d", item.EngineID, item.ID)
		}
	}
	return names, nil
}

func (s *LineageService) populateGraphEdges(ctx context.Context, tenantID uint, request models.LineageGraphRequest, ids []uint, response models.LineageGraphResponse, nodesByID map[uint]models.LineageNode) (models.LineageGraphResponse, error) {
	if len(ids) == 0 {
		return response, nil
	}
	var relations []models.LineageItemRelation
	statusClause := "status = 'active'"
	if request.AsOf != nil {
		statusClause = "(status = 'active' OR (status = 'closed' AND closed_at > ?))"
	}
	queryArgs := []interface{}{tenantID}
	if request.AsOf != nil {
		queryArgs = append(queryArgs, *request.AsOf)
	}
	queryArgs = append(queryArgs, ids, ids)
	query := s.db.WithContext(ctx).Where("tenant_id = ? AND "+statusClause+" AND source_item_id IN ? AND target_item_id IN ?", queryArgs...)
	if request.AsOf != nil {
		query = query.Where("last_observed_at <= ?", *request.AsOf)
	}
	if err := query.Limit(request.Limit).Find(&relations).Error; err != nil {
		return response, err
	}
	for _, relation := range relations {
		if request.SubjectKind == "data_item" && request.Direction == "upstream" && relation.TargetItemID != *request.ItemID {
			continue
		}
		if request.SubjectKind == "data_item" && request.Direction == "downstream" && relation.SourceItemID != *request.ItemID {
			continue
		}
		evidence, err := s.latestEvidence(ctx, tenantID, relation)
		if err != nil {
			return response, err
		}
		response.Edges = append(response.Edges, models.LineageEdge{Source: nodesByID[relation.SourceItemID], Target: nodesByID[relation.TargetItemID], RelationKind: relation.RelationKind, Granularity: relation.Granularity, Evidence: evidence, Status: relation.Status, LastObservedAt: relation.LastObservedAt})
	}
	var deps []models.LineageServiceDependency
	depQuery := s.db.WithContext(ctx).Where("tenant_id = ? AND source_item_id IN ? AND status = 'active'", tenantID, ids)
	if request.SubjectKind == "published_service" {
		depQuery = depQuery.Where("service_id = ? AND published_revision = ?", *request.ServiceID, request.Revision)
	}
	if request.AsOf != nil {
		depQuery = depQuery.Where("last_observed_at <= ?", *request.AsOf)
	}
	if err := depQuery.Limit(request.Limit).Find(&deps).Error; err != nil {
		return response, err
	}
	for _, dep := range deps {
		serviceNode := models.LineageNode{Kind: "published_service", ServiceID: uintPtr(dep.ServiceID), PublishedRevision: dep.PublishedRevision}
		response.Nodes = append(response.Nodes, serviceNode)
		if request.SubjectKind == "data_item" && request.Direction == "upstream" {
			continue
		}
		evidence, err := s.latestServiceEvidence(ctx, tenantID, dep)
		if err != nil {
			return response, err
		}
		response.Edges = append(response.Edges, models.LineageEdge{Source: nodesByID[dep.SourceItemID], Target: serviceNode, RelationKind: "serve", Granularity: dep.Granularity, Evidence: evidence, Status: dep.Status, LastObservedAt: dep.LastObservedAt})
	}
	return response, nil
}

func (s *LineageService) latestServiceEvidence(ctx context.Context, tenantID uint, dependency models.LineageServiceDependency) (map[string]interface{}, error) {
	var observation models.LineageObservation
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND relation_kind = 'serve' AND source_item_id = ? AND service_id = ? AND published_revision = ?", tenantID, dependency.SourceItemID, dependency.ServiceID, dependency.PublishedRevision).Order("observed_at DESC, id DESC").First(&observation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(observation.Evidence), nil
}

func (s *LineageService) latestEvidence(ctx context.Context, tenantID uint, relation models.LineageItemRelation) (map[string]interface{}, error) {
	var observation models.LineageObservation
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND relation_kind = ? AND source_item_id = ? AND target_item_id = ?", tenantID, relation.RelationKind, relation.SourceItemID, relation.TargetItemID).
		Order("observed_at DESC, id DESC").First(&observation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(observation.Evidence), nil
}
