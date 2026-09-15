package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonmodels "github.com/addp/common/models"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resourcetree"
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MetricImplementationService struct {
	repo      *repository.MetricImplementationRepository
	tableRepo *repository.LogicalTableRepository
	standard  *commonClient.StandardClient
	system    *commonClient.SystemServiceClient
}

func NewMetricImplementationService(repo *repository.MetricImplementationRepository, tables *repository.LogicalTableRepository) *MetricImplementationService {
	return &MetricImplementationService{repo: repo, tableRepo: tables}
}
func (s *MetricImplementationService) SetStandardClient(client *commonClient.StandardClient) {
	s.standard = client
}
func (s *MetricImplementationService) SetSystemClient(client *commonClient.SystemServiceClient) {
	s.system = client
}
func (s *MetricImplementationService) List(factID, tenantID int64) ([]models.MetricImplementation, error) {
	if factID > 0 {
		if _, err := s.tableRepo.GetByID(factID, tenantID); err != nil {
			return nil, metricNotFound()
		}
	}
	return s.repo.ListByFactTable(factID, tenantID)
}
func metricNotFound() error {
	return apperrors.NotFound("metric_implementation_not_found", i18n.MsgInvalidMetricImplementationID)
}
func metricConflict() error {
	return apperrors.Conflict("metric_implementation_state_conflict", i18n.MsgMetricImplementationConflict)
}
func (s *MetricImplementationService) Get(id, tenantID int64) (*models.MetricImplementation, error) {
	item, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, metricNotFound()
	}
	return item, nil
}
func (s *MetricImplementationService) Create(ctx context.Context, tenantID, userID int64, req *models.CreateMetricImplementationRequest) (*models.MetricImplementation, error) {
	if req == nil || req.FactTableID <= 0 || req.MetricDefinitionID <= 0 || !validRequiredString(req.Name, 200) {
		return nil, invalidRequest()
	}
	if table, err := s.tableRepo.GetByID(req.FactTableID, tenantID); err != nil {
		return nil, metricNotFound()
	} else if table.TableType != "fact" || table.Status != "approved" {
		return nil, metricConflict()
	}
	if s.standard == nil {
		return nil, apperrors.Unavailable("standard_unavailable", i18n.MsgValidationFailed)
	}
	if err := s.standard.WithTenantID(uint(tenantID)).ValidateMetricDefinition(ctx, req.MetricDefinitionID); err != nil {
		return nil, standardReferenceError(err, "metric_definition_not_found")
	}
	item := &models.MetricImplementation{TenantID: tenantID, FactTableID: req.FactTableID, MetricDefinitionID: req.MetricDefinitionID, Name: strings.TrimSpace(req.Name), Note: strings.TrimSpace(req.Note), Version: 1, CreatedBy: userID, Revisions: []models.MetricImplementationRevision{}}
	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, requiredStandardReference(models.StandardResourceMetric, req.MetricDefinitionID)); err != nil {
			return err
		}
		table, err := repository.LockLogicalTable(tx, req.FactTableID, tenantID)
		if err != nil {
			return metricNotFound()
		}
		if table.TableType != "fact" || table.Status != "approved" {
			return metricConflict()
		}
		return tx.Create(item).Error
	})
	return item, err
}
func lockMetric(tx *gorm.DB, id, tenantID, version int64) (*models.MetricImplementation, error) {
	var item models.MetricImplementation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error; err != nil {
		return nil, metricNotFound()
	}
	if err := requireVersion(item.Version, version); err != nil {
		return nil, err
	}
	return &item, nil
}
func advanceMetric(tx *gorm.DB, item *models.MetricImplementation, userID int64) error {
	result := tx.Model(&models.MetricImplementation{}).Where("id = ? AND tenant_id = ? AND version = ?", item.ID, item.TenantID, item.Version).Updates(map[string]any{"version": item.Version + 1, "updated_by": userID, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return metricConflict()
	}
	item.Version++
	return nil
}
func (s *MetricImplementationService) SaveDraft(ctx context.Context, id, tenantID, userID int64, req *models.SaveMetricImplementationRevisionRequest) (*models.MetricImplementation, error) {
	if req == nil || req.Version <= 0 || req.MetricDefinitionRevisionID <= 0 {
		return nil, invalidRequest()
	}
	if req.Contract.Filters == nil {
		req.Contract.Filters = []models.MetricBooleanFilter{}
	}
	item, err := s.Get(id, tenantID)
	if err != nil {
		return nil, err
	}
	if s.standard == nil {
		return nil, apperrors.Unavailable("standard_unavailable", i18n.MsgValidationFailed)
	}
	if _, err := s.standard.WithTenantID(uint(tenantID)).GetPublishedMetricDefinitionRevision(ctx, item.MetricDefinitionID, req.MetricDefinitionRevisionID); err != nil {
		return nil, standardReferenceError(err, "metric_definition_revision_not_found")
	}
	engine, err := s.metricEngineDescriptor(ctx, item)
	if err != nil {
		return nil, err
	}
	err = s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, requiredStandardReference(models.StandardResourceMetric, item.MetricDefinitionID)); err != nil {
			return err
		}
		locked, err := lockMetric(tx, id, tenantID, req.Version)
		if err != nil {
			return err
		}
		_, snapshot, hash, err := resolveMetricPlan(tx, locked, req.Contract, engine)
		if err != nil {
			return err
		}
		var draft models.MetricImplementationRevision
		err = tx.Where("implementation_id = ? AND tenant_id = ? AND status = ?", id, tenantID, models.MetricImplementationDraft).First(&draft).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var number int64
			if err := tx.Model(&models.MetricImplementationRevision{}).Where("implementation_id = ? AND tenant_id = ?", id, tenantID).Select("COALESCE(MAX(revision_no),0)").Scan(&number).Error; err != nil {
				return err
			}
			draft = models.MetricImplementationRevision{ImplementationID: id, TenantID: tenantID, RevisionNo: number + 1, Status: models.MetricImplementationDraft}
		} else if err != nil {
			return err
		}
		draft.MetricDefinitionRevisionID = req.MetricDefinitionRevisionID
		draft.Contract = req.Contract
		draft.DependencySnapshot = snapshot
		draft.DependencyHash = hash
		if err := tx.Save(&draft).Error; err != nil {
			return err
		}
		return advanceMetric(tx, locked, userID)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(id, tenantID)
}
func (s *MetricImplementationService) ChangeRevisionState(ctx context.Context, id, revisionID, tenantID, userID, version int64, publish bool) (*models.MetricImplementation, error) {
	item, err := s.Get(id, tenantID)
	if err != nil {
		return nil, err
	}
	if publish {
		var definitionRevisionID int64
		for _, revision := range item.Revisions {
			if revision.ID == revisionID {
				definitionRevisionID = revision.MetricDefinitionRevisionID
			}
		}
		if definitionRevisionID <= 0 {
			return nil, metricNotFound()
		}
		if s.standard == nil {
			return nil, apperrors.Unavailable("standard_unavailable", i18n.MsgValidationFailed)
		}
		if _, err := s.standard.WithTenantID(uint(tenantID)).GetPublishedMetricDefinitionRevision(ctx, item.MetricDefinitionID, definitionRevisionID); err != nil {
			return nil, standardReferenceError(err, "metric_definition_revision_not_found")
		}
	}
	var engine *commonmodels.EngineRuntimeDescriptor
	if publish {
		engine, err = s.metricEngineDescriptor(ctx, item)
		if err != nil {
			return nil, err
		}
	}
	err = s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, requiredStandardReference(models.StandardResourceMetric, item.MetricDefinitionID)); err != nil {
			return err
		}
		locked, err := lockMetric(tx, id, tenantID, version)
		if err != nil {
			return err
		}
		var revision models.MetricImplementationRevision
		if err := tx.Where("id = ? AND implementation_id = ? AND tenant_id = ?", revisionID, id, tenantID).First(&revision).Error; err != nil {
			return metricNotFound()
		}
		if publish {
			if revision.Status != models.MetricImplementationDraft {
				return metricConflict()
			}
			_, _, hash, err := resolveMetricPlan(tx, locked, revision.Contract, engine)
			if err != nil {
				return err
			}
			if hash != revision.DependencyHash {
				return apperrors.Conflict("metric_dependency_changed", i18n.MsgMetricImplementationConflict)
			}
			now := time.Now()
			revision.Status = models.MetricImplementationPublished
			revision.PublishedAt = &now
		} else {
			if revision.Status != models.MetricImplementationPublished {
				return metricConflict()
			}
			revision.Status = models.MetricImplementationWithdrawn
		}
		if err := tx.Save(&revision).Error; err != nil {
			return err
		}
		return advanceMetric(tx, locked, userID)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(id, tenantID)
}
func (s *MetricImplementationService) Delete(id, tenantID, userID, version int64) error {
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		item, err := lockMetric(tx, id, tenantID, version)
		if err != nil {
			return err
		}
		var published int64
		if err := tx.Model(&models.MetricImplementationRevision{}).Where("implementation_id = ? AND tenant_id = ? AND status <> ?", id, tenantID, models.MetricImplementationDraft).Count(&published).Error; err != nil {
			return err
		}
		if published > 0 {
			return metricConflict()
		}
		if err := tx.Where("implementation_id = ? AND tenant_id = ?", id, tenantID).Delete(&models.MetricImplementationRevision{}).Error; err != nil {
			return err
		}
		return tx.Delete(item).Error
	})
}

type MetricCompiledPlan struct {
	Parameters                 []commonClient.ModelMetricParameter `json:"parameters"`
	Fields                     []datatype.FieldInfo                `json:"fields"`
	StableKey                  []string                            `json:"stable_key"`
	ImplementationID           int64                               `json:"implementation_id"`
	RevisionID                 int64                               `json:"revision_id"`
	MetricDefinitionID         int64                               `json:"metric_definition_id"`
	MetricDefinitionRevisionID int64                               `json:"metric_definition_revision_id"`
	DependencyHash             string                              `json:"dependency_hash"`
	EngineID                   uint                                `json:"engine_id"`
	SQL                        string                              `json:"sql"`
}

func (s *MetricImplementationService) PublishedPlan(ctx context.Context, id, revisionID, tenantID int64, input *models.MetricQueryInput) (*MetricCompiledPlan, error) {
	if input != nil {
		if err := validateMetricQueryInput(*input); err != nil {
			return nil, err
		}
	}
	identity, err := s.Get(id, tenantID)
	if err != nil {
		return nil, err
	}
	var definitionRevisionID int64
	for _, r := range identity.Revisions {
		if r.ID == revisionID {
			definitionRevisionID = r.MetricDefinitionRevisionID
		}
	}
	if definitionRevisionID == 0 {
		return nil, metricNotFound()
	}
	if s.standard == nil {
		return nil, apperrors.Unavailable("standard_unavailable", i18n.MsgValidationFailed)
	}
	if _, err := s.standard.WithTenantID(uint(tenantID)).GetPublishedMetricDefinitionRevision(ctx, identity.MetricDefinitionID, definitionRevisionID); err != nil {
		return nil, standardReferenceError(err, "metric_definition_revision_not_found")
	}
	engine, err := s.metricEngineDescriptor(ctx, identity)
	if err != nil {
		return nil, err
	}
	var result *MetricCompiledPlan
	err = s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := repository.NewMetricImplementationRepository(tx).GetByID(id, tenantID)
		if err != nil {
			return metricNotFound()
		}
		var revision *models.MetricImplementationRevision
		for i := range item.Revisions {
			if item.Revisions[i].ID == revisionID {
				revision = &item.Revisions[i]
			}
		}
		if revision == nil {
			return metricNotFound()
		}
		if revision.Status != models.MetricImplementationPublished {
			return metricConflict()
		}
		if input != nil {
			if err := validateMetricOperationInput(revision.Contract.Operation, *input); err != nil {
				return err
			}
		}
		sql, snapshot, hash, err := resolveMetricPlan(tx, item, revision.Contract, engine)
		if err != nil {
			return err
		}
		if hash != revision.DependencyHash {
			return apperrors.Conflict("metric_dependency_changed", i18n.MsgMetricImplementationConflict)
		}
		engineID := uint(snapshot["engine_id"].(uint))
		parameters, fields, stableKey := metricPlanSignature(revision.Contract.Operation)
		result = &MetricCompiledPlan{Parameters: parameters, Fields: fields, StableKey: stableKey, ImplementationID: id, RevisionID: revisionID, MetricDefinitionID: item.MetricDefinitionID, MetricDefinitionRevisionID: revision.MetricDefinitionRevisionID, DependencyHash: hash, EngineID: engineID, SQL: sql}
		return nil
	})
	return result, err
}

// Fetch remote metadata before taking local aggregate locks. The compiler checks
// the locked sources against this descriptor and includes the dialect in its hash.
func (s *MetricImplementationService) metricEngineDescriptor(ctx context.Context, item *models.MetricImplementation) (*commonmodels.EngineRuntimeDescriptor, error) {
	table, err := s.tableRepo.GetByID(item.FactTableID, item.TenantID)
	if err != nil {
		return nil, metricNotFound()
	}
	uri, _ := table.Materialization["target_parent_locator"].(string)
	locator, err := resourcetree.ParseURI(uri)
	if err != nil || locator.EngineID == 0 {
		return nil, invalidRequest()
	}
	if s.system == nil {
		return nil, apperrors.Unavailable("engine_descriptor_unavailable", i18n.MsgMetricEngineUnavailable)
	}
	engine, err := s.system.WithTenantID(uint(item.TenantID)).GetEngineRuntimeDescriptor(ctx, locator.EngineID)
	if err != nil {
		return nil, apperrors.Wrap(apperrors.KindUnavailable, "engine_descriptor_unavailable", i18n.MsgMetricEngineUnavailable, err)
	}
	if engine == nil || engine.ID != locator.EngineID {
		return nil, metricConflict()
	}
	return engine, nil
}

func resolveMetricPlan(tx *gorm.DB, item *models.MetricImplementation, contract models.MetricContract, engine *commonmodels.EngineRuntimeDescriptor) (string, models.JSONB, string, error) {
	bindings := metricPlanBindings{Relations: map[int64]metricPlanRelation{}}
	var engineID uint
	tables := map[int64]models.JSONB{}
	load := func(id int64) (metricPlanSource, error) {
		table, err := repository.LockLogicalTable(tx, id, item.TenantID)
		if err != nil {
			return metricPlanSource{}, metricNotFound()
		}
		if table.Status != "approved" {
			return metricPlanSource{}, metricConflict()
		}
		if (id == item.FactTableID && table.TableType != "fact") || (id != item.FactTableID && table.TableType != "dimension") {
			return metricPlanSource{}, metricConflict()
		}
		uri, ok := table.Materialization["target_parent_locator"].(string)
		if !ok {
			return metricPlanSource{}, invalidRequest()
		}
		locator, err := resourcetree.ParseURI(uri)
		if err != nil || len(locator.Path) != 1 || locator.EngineID == 0 {
			return metricPlanSource{}, invalidRequest()
		}
		name, ok := table.Materialization["target_name"].(string)
		if !ok || name == "" {
			return metricPlanSource{}, invalidRequest()
		}
		if engineID != 0 && engineID != locator.EngineID {
			return metricPlanSource{}, invalidRequest()
		}
		engineID = locator.EngineID
		var fields []models.LogicalField
		if err := tx.Where("table_id = ?", id).Order("id").Find(&fields).Error; err != nil {
			return metricPlanSource{}, err
		}
		source := metricPlanSource{Schema: locator.Path[0], Table: name, Fields: map[int64]models.LogicalField{}}
		for _, field := range fields {
			source.Fields[field.ID] = field
		}
		tables[id] = models.JSONB{"id": id, "locator": uri, "name": name}
		return source, nil
	}
	var err error
	bindings.Fact, err = load(item.FactTableID)
	if err != nil {
		return "", nil, "", err
	}
	refs := []models.MetricFieldReference{contract.Subject, contract.Distinct, contract.Time}
	for _, filter := range contract.Filters {
		refs = append(refs, filter.Field)
	}
	ids := map[int64]bool{contract.SubjectRelationID: true}
	for _, ref := range refs {
		if ref.RelationID > 0 {
			ids[ref.RelationID] = true
		}
	}
	var relations []models.TableRelation
	relationIDs := make([]int64, 0, len(ids))
	for id := range ids {
		relationIDs = append(relationIDs, id)
	}
	if err := tx.Where("id IN ? AND tenant_id = ? AND source_table = ?", relationIDs, item.TenantID, item.FactTableID).Order("target_table, id").Find(&relations).Error; err != nil {
		return "", nil, "", err
	}
	if len(relations) != len(ids) {
		return "", nil, "", invalidRequest()
	}
	for _, relation := range relations {
		target, err := load(relation.TargetTable)
		if err != nil {
			return "", nil, "", err
		}
		bindings.Relations[relation.ID] = metricPlanRelation{SourceField: relation.SourceField, TargetField: relation.TargetField, Target: target}
		refs = append(refs, models.MetricFieldReference{FieldID: relation.SourceField}, models.MetricFieldReference{FieldID: relation.TargetField, RelationID: relation.ID})
	}
	if engine == nil || engine.ID != engineID {
		return "", nil, "", metricConflict()
	}
	dialect, err := plugin.ResolveAnalyticalSQLDialect(engine.EngineType)
	if err != nil {
		return "", nil, "", apperrors.Wrap(apperrors.KindValidation, "analytical_sql_unavailable", i18n.MsgMetricEngineUnsupported, err)
	}
	enginePlugin, err := plugin.Get(engine.EngineType)
	if err != nil {
		return "", nil, "", invalidRequest()
	}
	catalogProvider, ok := enginePlugin.(plugin.EngineCatalogModelProvider)
	if !ok {
		return "", nil, "", invalidRequest()
	}
	for _, table := range tables {
		locator, err := resourcetree.ParseURI(table["locator"].(string))
		if err != nil {
			return "", nil, "", invalidRequest()
		}
		path, err := resourcetree.EngineCatalogPathFromLocator(catalogProvider.EngineCatalogModel(), locator)
		if err != nil {
			return "", nil, "", invalidRequest()
		}
		segments := plugin.EngineCatalogPathWithoutRoot(path).Segments
		if len(segments) != 1 || segments[0].Kind != plugin.EngineCatalogKindNamespace {
			return "", nil, "", invalidRequest()
		}
	}
	query, err := compileMetricSQL(contract, bindings, dialect)
	if err != nil {
		return "", nil, "", err
	}
	dependencies := []models.JSONB{}
	seen := map[models.MetricFieldReference]bool{}
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		source := bindings.Fact
		if ref.RelationID != 0 {
			source = bindings.Relations[ref.RelationID].Target
		}
		field := source.Fields[ref.FieldID]
		dependencies = append(dependencies, models.JSONB{"field_id": field.ID, "relation_id": ref.RelationID, "column": field.ColumnName, "type": field.DataType, "nullable": field.Nullable, "pk": field.IsPK, "element_revision_id": field.ElementRevisionID})
	}
	relationSnapshot := make([]models.JSONB, 0, len(relations))
	for _, relation := range relations {
		relationSnapshot = append(relationSnapshot, models.JSONB{"id": relation.ID, "source_table": relation.SourceTable, "source_field": relation.SourceField, "target_table": relation.TargetTable, "target_field": relation.TargetField, "relation_type": relation.RelationType})
	}
	snapshot := models.JSONB{"engine_id": engineID, "sql_dialect": dialect.Name(), "tables": tables, "fields": dependencies, "relations": relationSnapshot, "contract": contract}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", nil, "", err
	}
	digest := sha256.Sum256(raw)
	return query, snapshot, hex.EncodeToString(digest[:]), nil
}

func metricPlanSignature(operation string) ([]commonClient.ModelMetricParameter, []datatype.FieldInfo, []string) {
	parameters := []commonClient.ModelMetricParameter{{Name: "subject_id", Type: datatype.FieldTypeString, Required: true}, {Name: "start_date", Type: datatype.FieldTypeDate, Required: true}, {Name: "end_date", Type: datatype.FieldTypeDate, Required: true}, {Name: "grain", Type: datatype.FieldTypeString, Required: true, Options: metricParameterOptions("grain")}}
	fields := []datatype.FieldInfo{{Name: "subject_id", Type: datatype.FieldTypeString, Nullable: false}, {Name: "bucket", Type: datatype.FieldTypeDate, Nullable: false}, {Name: "value", Type: datatype.FieldTypeBigInt, Nullable: false}}
	keys := []string{"subject_id", "bucket"}
	if operation == "directional_overlap" {
		parameters = append(parameters, commonClient.ModelMetricParameter{Name: "comparison_id", Type: datatype.FieldTypeString, Required: true}, commonClient.ModelMetricParameter{Name: "directions", Type: datatype.FieldTypeString, Required: true, Options: metricParameterOptions("directions")})
		fields[2].Type = datatype.FieldTypeDecimal
		fields = append(fields, datatype.FieldInfo{Name: "comparison_id", Type: datatype.FieldTypeString}, datatype.FieldInfo{Name: "direction", Type: datatype.FieldTypeString}, datatype.FieldInfo{Name: "subject_count", Type: datatype.FieldTypeBigInt}, datatype.FieldInfo{Name: "comparison_count", Type: datatype.FieldTypeBigInt}, datatype.FieldInfo{Name: "shared_count", Type: datatype.FieldTypeBigInt})
		keys = []string{"direction", "bucket"}
	}
	return parameters, fields, keys
}

func metricParameterOptions(name string) []commonquery.ParameterOption {
	var values []string
	switch name {
	case "grain":
		values = []string{"total", "month"}
	case "directions":
		values = []string{"forward", "both"}
	}
	result := make([]commonquery.ParameterOption, 0, len(values))
	for _, value := range values {
		key := "model.metric.option." + name + "." + value
		result = append(result, commonquery.ParameterOption{Value: value, Labels: map[string]string{"zh-cn": commoni18n.ForLanguage("zh-cn", key), "en": commoni18n.ForLanguage("en", key)}})
	}
	return result
}
