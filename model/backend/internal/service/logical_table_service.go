package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	commonClient "github.com/addp/common/client"
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

type LogicalTableService struct {
	repo        *repository.LogicalTableRepository
	entityRepo  *repository.EntityRepository
	dwLayerRepo *repository.DWLayerRepository
	standard    *commonClient.StandardClient
}

func (s *LogicalTableService) SetStandardClient(client *commonClient.StandardClient) {
	s.standard = client
}

func (s *LogicalTableService) validateReferences(tenantID int64, domainID, elementID, hierarchyID *int64) error {
	if s.standard == nil {
		return nil
	}
	client := s.standard.WithTenantID(uint(tenantID))
	if domainID != nil && *domainID > 0 {
		if err := client.ValidateDomain(context.Background(), *domainID); err != nil {
			return standardReferenceError(err, "domain_not_found")
		}
	}
	if elementID != nil && *elementID > 0 {
		if err := client.ValidateElement(context.Background(), *elementID); err != nil {
			return standardReferenceError(err, "element_not_found")
		}
	}
	if hierarchyID != nil && *hierarchyID > 0 {
		if err := client.ValidateDimensionHierarchy(context.Background(), *hierarchyID); err != nil {
			return standardReferenceError(err, "hierarchy_not_found")
		}
	}
	return nil
}

func validateLogicalTableShape(tableType string, scdType int, grain string) error {
	if tableType != "entity" && tableType != "fact" && tableType != "dimension" {
		return apperrors.Validation("logical_table_shape_invalid", i18n.MsgValidationFailed)
	}
	if tableType == "dimension" {
		if scdType < 0 || scdType > 3 {
			return apperrors.Validation("logical_table_shape_invalid", i18n.MsgValidationFailed)
		}
	} else if scdType != 0 {
		return apperrors.Validation("logical_table_shape_invalid", i18n.MsgValidationFailed)
	}
	if tableType == "fact" && strings.TrimSpace(grain) == "" {
		return apperrors.Validation("logical_table_shape_invalid", i18n.MsgValidationFailed)
	}
	if tableType != "fact" && strings.TrimSpace(grain) != "" {
		return apperrors.Validation("logical_table_shape_invalid", i18n.MsgValidationFailed)
	}
	return nil
}

func NewLogicalTableService(
	repo *repository.LogicalTableRepository,
	entityRepo *repository.EntityRepository,
	dwLayerRepo *repository.DWLayerRepository,
) *LogicalTableService {
	return &LogicalTableService{repo: repo, entityRepo: entityRepo, dwLayerRepo: dwLayerRepo}
}

func (s *LogicalTableService) validateOwnedReferences(tenantID int64, entityID *int64, layer string) error {
	if entityID != nil {
		if *entityID <= 0 {
			return apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
		}
		if _, err := s.entityRepo.GetByID(*entityID, tenantID); err != nil {
			return apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
		}
	}
	exists, err := s.dwLayerRepo.ExistsByCode(layer, tenantID, 0)
	if err != nil {
		return err
	}
	if !exists {
		return apperrors.NotFound("dw_layer_not_found", i18n.MsgLayerNotFound)
	}
	return nil
}

func (s *LogicalTableService) CreateLogicalTable(req *models.CreateLogicalTableRequest, tenantID, userID int64) (*models.LogicalTable, error) {
	if err := validateCreateLogicalTableRequest(req); err != nil {
		return nil, err
	}
	if err := validateMaterializationKeys(req.Materialization); err != nil {
		return nil, apperrors.Wrap(apperrors.KindValidation, "materialization_invalid", i18n.MsgValidationFailed, err)
	}
	if err := validateLogicalTableShape(req.TableType, req.SCDType, req.GrainDescription); err != nil {
		return nil, err
	}
	if err := s.validateReferences(tenantID, req.DomainID, nil, nil); err != nil {
		return nil, err
	}
	table := &models.LogicalTable{
		TenantID:         tenantID,
		DomainID:         req.DomainID,
		EntityID:         req.EntityID,
		Name:             req.Name,
		Code:             req.Code,
		Description:      req.Description,
		TableType:        req.TableType,
		Layer:            req.Layer,
		GrainDescription: req.GrainDescription,
		SCDType:          req.SCDType,
		Status:           "draft",
		Version:          1,
		Materialization:  req.Materialization,
		CreatedBy:        userID,
	}

	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, standardReference(models.StandardResourceDomain, req.DomainID)); err != nil {
			return err
		}
		if req.EntityID != nil {
			if _, err := repository.LockEntity(tx, *req.EntityID, tenantID); err != nil {
				return apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
			}
		}
		if _, err := repository.LockDWLayerByCode(tx, req.Layer, tenantID); err != nil {
			return apperrors.NotFound("dw_layer_not_found", i18n.MsgLayerNotFound)
		}
		txRepo := repository.NewLogicalTableRepository(tx)
		exists, err := txRepo.ExistsByCode(req.Code, tenantID, 0)
		if err != nil {
			return err
		}
		if exists {
			return apperrors.Conflict("logical_table_code_conflict", i18n.MsgTableCodeConflict)
		}
		if err := txRepo.Create(table); err != nil {
			return modelResourceError(err, "logical_table_code", i18n.MsgTableCodeConflict)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return table, nil
}

func (s *LogicalTableService) GetLogicalTable(id, tenantID int64) (*models.LogicalTable, error) {
	table, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, modelResourceError(err, "logical_table_not_found", i18n.MsgTableNotFound)
	}
	return table, nil
}

func (s *LogicalTableService) ListLogicalTables(tenantID int64, opts repository.ListLogicalTableOptions) ([]models.LogicalTable, int64, error) {
	if !validOptionalID(opts.DomainID) || !validListStatus(opts.Status) ||
		opts.TableType != "" && !validValue(opts.TableType, "entity", "fact", "dimension") {
		return nil, 0, invalidRequest()
	}
	if opts.Layer != "" {
		exists, err := s.dwLayerRepo.ExistsByCode(opts.Layer, tenantID, 0)
		if err != nil {
			return nil, 0, err
		}
		if !exists {
			return nil, 0, invalidRequest()
		}
	}
	return s.repo.List(tenantID, opts)
}

func (s *LogicalTableService) UpdateLogicalTable(id, tenantID, userID int64, req *models.UpdateLogicalTableRequest) (*models.LogicalTable, error) {
	if req == nil || !validOptionalID(req.DomainID) || !validOptionalID(req.EntityID) ||
		!validRequiredString(req.Name, 200) || !validValue(req.TableType, "entity", "fact", "dimension") ||
		!validRequiredString(req.Layer, 20) || req.SCDType == nil || req.Materialization == nil {
		return nil, apperrors.Validation("invalid_request", i18n.MsgValidationFailed)
	}

	if err := s.validateReferences(tenantID, req.DomainID, nil, nil); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Layer) == "" {
		return nil, apperrors.Validation("logical_table_layer_required", i18n.MsgValidationFailed)
	}
	if err := validateMaterializationKeys(req.Materialization); err != nil {
		return nil, apperrors.Wrap(apperrors.KindValidation, "materialization_invalid", i18n.MsgValidationFailed, err)
	}
	if err := validateLogicalTableShape(req.TableType, *req.SCDType, req.GrainDescription); err != nil {
		return nil, err
	}
	var table *models.LogicalTable
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, standardReference(models.StandardResourceDomain, req.DomainID)); err != nil {
			return err
		}
		var err error
		table, err = repository.LockLogicalTable(tx, id, tenantID)
		if err != nil {
			return modelResourceError(err, "logical_table_not_found", i18n.MsgTableNotFound)
		}
		if err := requireVersion(table.Version, req.Version); err != nil {
			return err
		}
		if table.Status != "draft" {
			return apperrors.Conflict("logical_table_state_conflict", i18n.MsgTableStateConflict)
		}
		if req.EntityID != nil {
			if _, err := repository.LockEntity(tx, *req.EntityID, tenantID); err != nil {
				return apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
			}
		}
		if _, err := repository.LockDWLayerByCode(tx, req.Layer, tenantID); err != nil {
			return apperrors.NotFound("dw_layer_not_found", i18n.MsgLayerNotFound)
		}
		table.Name = req.Name
		table.DomainID = req.DomainID
		table.EntityID = req.EntityID
		table.Description = req.Description
		table.TableType = req.TableType
		table.Layer = req.Layer
		table.GrainDescription = req.GrainDescription
		table.SCDType = *req.SCDType
		table.Materialization = req.Materialization
		table.UpdatedBy = &userID
		return repository.NewLogicalTableRepository(tx).Update(table)
	})
	if err != nil {
		return nil, err
	}
	return table, nil
}

func (s *LogicalTableService) DeleteLogicalTable(id, tenantID, version int64) error {
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		table, err := repository.LockLogicalTable(tx, id, tenantID)
		if err != nil {
			return modelResourceError(err, "logical_table_not_found", i18n.MsgTableNotFound)
		}
		if err := requireVersion(table.Version, version); err != nil {
			return err
		}
		if table.Status != "draft" {
			return apperrors.Conflict("logical_table_state_conflict", i18n.MsgTableStateConflict)
		}
		relations, err := repository.NewTableRelationRepository(tx).ListByTable(id, tenantID)
		if err != nil {
			return err
		}
		otherTableIDs := make(map[int64]struct{}, len(relations))
		survivingFactIDs := make(map[int64]struct{}, len(relations))
		for _, relation := range relations {
			otherTableID := relation.SourceTable
			if otherTableID == id {
				otherTableID = relation.TargetTable
			}
			otherTableIDs[otherTableID] = struct{}{}
			if relation.SourceTable != id {
				survivingFactIDs[relation.SourceTable] = struct{}{}
			}
		}

		orderedOtherTableIDs := make([]int64, 0, len(otherTableIDs))
		for otherTableID := range otherTableIDs {
			orderedOtherTableIDs = append(orderedOtherTableIDs, otherTableID)
		}
		sort.Slice(orderedOtherTableIDs, func(i, j int) bool { return orderedOtherTableIDs[i] < orderedOtherTableIDs[j] })

		lockedVersions := make(map[int64]int64, len(orderedOtherTableIDs))
		for _, otherTableID := range orderedOtherTableIDs {
			otherTable, err := repository.LockLogicalTable(tx, otherTableID, tenantID)
			if err != nil {
				return err
			}
			if otherTable.Status != "draft" {
				return apperrors.Conflict("logical_table_relation_state_conflict", i18n.MsgTableRelationStateConflict)
			}
			lockedVersions[otherTableID] = otherTable.Version
		}
		if err := repository.NewLogicalTableRepository(tx).Delete(id, tenantID, version); err != nil {
			return err
		}
		for _, otherTableID := range orderedOtherTableIDs {
			if _, survivesAsFact := survivingFactIDs[otherTableID]; !survivesAsFact {
				continue
			}
			if _, err := repository.AdvanceLogicalTableVersion(tx, otherTableID, tenantID, lockedVersions[otherTableID]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *LogicalTableService) ApproveLogicalTable(id, tenantID, userID, version int64) (*models.LogicalTable, error) {
	return s.updateLogicalTableStatus(id, tenantID, userID, version, "draft", "approved", true)
}

func (s *LogicalTableService) ReopenLogicalTable(id, tenantID, userID, version int64) (*models.LogicalTable, error) {
	return s.updateLogicalTableStatus(id, tenantID, userID, version, "approved", "draft", false)
}

func (s *LogicalTableService) updateLogicalTableStatus(id, tenantID, userID, version int64, from, to string, validateApproval bool) (*models.LogicalTable, error) {
	var table *models.LogicalTable
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		var err error
		table, err = repository.LockLogicalTable(tx, id, tenantID)
		if err != nil {
			return modelResourceError(err, "logical_table_not_found", i18n.MsgTableNotFound)
		}
		if err := requireVersion(table.Version, version); err != nil {
			return err
		}
		if table.Status != from {
			return apperrors.Conflict("logical_table_state_conflict", i18n.MsgTableStateConflict)
		}
		if validateApproval {
			if err := validateLogicalTableShape(table.TableType, table.SCDType, table.GrainDescription); err != nil {
				return err
			}
			if strings.TrimSpace(table.Layer) == "" {
				return apperrors.Validation("logical_table_layer_required", i18n.MsgValidationFailed)
			}
			fields, err := repository.NewLogicalTableRepository(tx).GetFields(id)
			if err != nil {
				return err
			}
			if len(fields) == 0 {
				return apperrors.Validation("logical_table_approval_fields_required", i18n.MsgTableFieldsRequired)
			}
			hasPrimaryKey := false
			for _, field := range fields {
				hasPrimaryKey = hasPrimaryKey || field.IsPK
			}
			if !hasPrimaryKey {
				return apperrors.Validation("logical_table_approval_primary_key_required", i18n.MsgTablePrimaryKeyRequired)
			}
		}
		if err := repository.NewLogicalTableRepository(tx).UpdateStatus(id, tenantID, version, to, userID); err != nil {
			return err
		}
		table.Status = to
		table.Version++
		table.UpdatedBy = &userID
		return nil
	})
	return table, err
}

// GetFields 获取逻辑表字段列表
func (s *LogicalTableService) GetFields(tableID, tenantID int64) ([]models.LogicalField, error) {
	if _, err := s.repo.GetByID(tableID, tenantID); err != nil {
		return nil, apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
	}
	return s.repo.GetFields(tableID)
}

// CreateField 创建字段
func (s *LogicalTableService) CreateField(tableID, tenantID int64, req *models.CreateLogicalFieldRequest) (*models.LogicalFieldMutationResponse, error) {
	if err := validateCreateLogicalFieldRequest(req); err != nil {
		return nil, err
	}
	if err := s.validateReferences(tenantID, nil, req.ElementID, req.HierarchyID); err != nil {
		return nil, err
	}
	if req.IsPK && req.Nullable {
		return nil, apperrors.Validation("logical_field_invalid", i18n.MsgValidationFailed)
	}
	fieldRole := req.FieldRole
	if fieldRole == "" {
		fieldRole = "regular"
	}
	field := models.LogicalField{
		TableID:        tableID,
		ElementID:      req.ElementID,
		Name:           req.Name,
		ColumnName:     req.ColumnName,
		DataType:       req.DataType,
		Length:         req.Length,
		Nullable:       req.Nullable,
		IsPK:           req.IsPK,
		IsPartition:    req.IsPartition,
		DefaultValue:   req.DefaultValue,
		Description:    req.Description,
		SortOrder:      req.SortOrder,
		FieldRole:      fieldRole,
		HierarchyID:    req.HierarchyID,
		HierarchyLevel: req.HierarchyLevel,
	}

	response := &models.LogicalFieldMutationResponse{Field: field}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID,
			standardReference(models.StandardResourceElement, req.ElementID),
			standardReference(models.StandardResourceDimensionHierarchy, req.HierarchyID)); err != nil {
			return err
		}
		table, err := repository.LockLogicalTable(tx, tableID, tenantID)
		if err != nil {
			return apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
		}
		if err := requireVersion(table.Version, req.Version); err != nil {
			return err
		}
		if table.Status != "draft" {
			return apperrors.Conflict("logical_table_state_conflict", i18n.MsgTableStateConflict)
		}
		if table.TableType != "fact" && strings.HasPrefix(req.FieldRole, "measure_") {
			return apperrors.Validation("logical_field_invalid", i18n.MsgValidationFailed)
		}
		if err := repository.NewLogicalTableRepository(tx).CreateField(&response.Field); err != nil {
			return modelResourceError(err, "logical_field_column", i18n.MsgFieldColumnConflict)
		}
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, tableID, tenantID, req.Version)
		return err
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

// UpdateField 更新字段
func (s *LogicalTableService) UpdateField(fieldID, tableID, tenantID int64, req *models.UpdateLogicalFieldRequest) (*models.LogicalFieldMutationResponse, error) {
	if req == nil {
		return nil, apperrors.Validation("invalid_request", i18n.MsgValidationFailed)
	}

	if err := s.validateReferences(tenantID, nil, req.ElementID, req.HierarchyID); err != nil {
		return nil, err
	}
	if !validRequiredString(req.Name, 200) || !modelCodePattern.MatchString(req.ColumnName) || utf8.RuneCountInString(req.ColumnName) > 200 ||
		!validValue(req.DataType, modelDataTypes...) || !validValue(req.FieldRole, modelFieldRoles...) ||
		!validOptionalID(req.ElementID) || !validOptionalID(req.HierarchyID) ||
		req.Nullable == nil || req.IsPK == nil || req.IsPartition == nil || req.SortOrder == nil || *req.SortOrder < 0 ||
		(req.Length != nil && *req.Length <= 0) || !validHierarchy(req.HierarchyID, req.HierarchyLevel) {
		return nil, apperrors.Validation("invalid_request", i18n.MsgValidationFailed)
	}

	if *req.IsPK && *req.Nullable {
		return nil, apperrors.Validation("logical_field_invalid", i18n.MsgValidationFailed)
	}
	response := &models.LogicalFieldMutationResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID,
			standardReference(models.StandardResourceElement, req.ElementID),
			standardReference(models.StandardResourceDimensionHierarchy, req.HierarchyID)); err != nil {
			return err
		}
		table, err := repository.LockLogicalTable(tx, tableID, tenantID)
		if err != nil {
			return apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
		}
		if err := requireVersion(table.Version, req.Version); err != nil {
			return err
		}
		if table.Status != "draft" {
			return apperrors.Conflict("logical_table_state_conflict", i18n.MsgTableStateConflict)
		}
		if table.TableType != "fact" && strings.HasPrefix(req.FieldRole, "measure_") {
			return apperrors.Validation("logical_field_invalid", i18n.MsgValidationFailed)
		}
		txRepo := repository.NewLogicalTableRepository(tx)
		field, err := txRepo.GetFieldByID(fieldID, tableID)
		if err != nil {
			return apperrors.NotFound("logical_field_not_found", i18n.MsgFieldNotFound)
		}
		field.Name = req.Name
		field.ColumnName = req.ColumnName
		field.DataType = req.DataType
		field.ElementID = req.ElementID
		field.Length = req.Length
		field.Nullable = *req.Nullable
		field.IsPK = *req.IsPK
		field.IsPartition = *req.IsPartition
		field.DefaultValue = req.DefaultValue
		field.Description = req.Description
		field.SortOrder = *req.SortOrder
		field.FieldRole = req.FieldRole
		field.HierarchyID = req.HierarchyID
		field.HierarchyLevel = req.HierarchyLevel
		if err := txRepo.UpdateField(field); err != nil {
			return modelResourceError(err, "logical_field_column", i18n.MsgFieldColumnConflict)
		}
		response.Field = *field
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, tableID, tenantID, req.Version)
		return err
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

// DeleteField 删除字段
func (s *LogicalTableService) DeleteField(fieldID, tableID, tenantID, version int64) (*models.VersionResponse, error) {
	response := &models.VersionResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		table, err := repository.LockLogicalTable(tx, tableID, tenantID)
		if err != nil {
			return apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
		}
		if err := requireVersion(table.Version, version); err != nil {
			return err
		}
		if table.Status != "draft" {
			return apperrors.Conflict("logical_table_state_conflict", i18n.MsgTableStateConflict)
		}
		if err := repository.NewLogicalTableRepository(tx).DeleteField(fieldID, tableID); err != nil {
			return modelResourceError(err, "logical_field_not_found", i18n.MsgFieldNotFound)
		}
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, tableID, tenantID, version)
		return err
	})
	return response, err
}

// PreviewDDL 预览生成的 DDL（仅支持 PostgreSQL）
func (s *LogicalTableService) PreviewDDL(tableID, tenantID int64, materialization map[string]interface{}) (string, error) {
	table, err := s.repo.GetByID(tableID, tenantID)
	if err != nil {
		return "", modelResourceError(err, "logical_table_not_found", i18n.MsgTableNotFound)
	}

	fields, err := s.repo.GetFields(tableID)
	if err != nil {
		return "", err
	}

	if len(fields) == 0 {
		return "", apperrors.Validation("ddl_preview_invalid", i18n.MsgDDLPreviewInvalid)
	}
	previewTable := previewLogicalTableWithMaterialization(table, materialization)
	if err := validateMaterialization(previewTable, fields); err != nil {
		return "", apperrors.Wrap(apperrors.KindValidation, "ddl_preview_invalid", i18n.MsgDDLPreviewInvalid, err)
	}

	return s.generatePostgreSQLDDL(previewTable, fields), nil
}

func previewLogicalTableWithMaterialization(table *models.LogicalTable, materialization map[string]interface{}) *models.LogicalTable {
	previewTable := *table
	previewTable.Materialization = models.JSONB(materialization)
	return &previewTable
}

// generatePostgreSQLDDL 生成 PostgreSQL CREATE TABLE DDL
func (s *LogicalTableService) generatePostgreSQLDDL(table *models.LogicalTable, fields []models.LogicalField) string {
	var ddl strings.Builder

	// 1. 提取配置
	schemaName := "public"
	tableName := table.Code
	if table.Materialization != nil {
		if schema, ok := table.Materialization["schema_name"].(string); ok && schema != "" {
			schemaName = schema
		}
		if tname, ok := table.Materialization["table_name"].(string); ok && tname != "" {
			tableName = tname
		}
	}

	// 2. CREATE TABLE 头
	ddl.WriteString(fmt.Sprintf("CREATE TABLE %s.%s (\n", quoteIdentifier(schemaName), quoteIdentifier(tableName)))

	// 3. 生成字段定义
	var fieldDefs []string
	var pkFields []string

	for _, field := range fields {
		fieldDef := s.generateFieldDef(&field)
		fieldDefs = append(fieldDefs, fieldDef)

		if field.IsPK {
			pkFields = append(pkFields, field.ColumnName)
		}
	}

	ddl.WriteString(strings.Join(fieldDefs, ",\n"))

	// 4. 添加主键约束
	if len(pkFields) > 0 {
		ddl.WriteString(",\n  PRIMARY KEY (")
		quoted := make([]string, 0, len(pkFields))
		for _, name := range pkFields {
			quoted = append(quoted, quoteIdentifier(name))
		}
		ddl.WriteString(strings.Join(quoted, ", "))
		ddl.WriteString(")")
	}

	// 5. 闭合括号
	ddl.WriteString("\n)")

	// 6. 分区（可选）
	if table.Materialization != nil {
		if partitionBy, ok := table.Materialization["partition_by"].(string); ok && partitionBy != "" {
			partitionType := "RANGE"
			if pt, ok := table.Materialization["partition_type"].(string); ok && pt != "" {
				partitionType = strings.ToUpper(pt)
			}
			ddl.WriteString(fmt.Sprintf("\nPARTITION BY %s (%s)", partitionType, quoteIdentifier(partitionBy)))
		}
	}

	// 8. 结尾
	ddl.WriteString(";")

	return ddl.String()
}

// generateFieldDef 生成单个字段定义
func (s *LogicalTableService) generateFieldDef(field *models.LogicalField) string {
	var parts []string

	// 列名
	parts = append(parts, "  "+quoteIdentifier(field.ColumnName))

	// 数据类型映射
	pgType := s.mapDataTypeToPostgreSQL(field.DataType, field.Length)
	parts = append(parts, pgType)

	// NOT NULL
	if !field.Nullable {
		parts = append(parts, "NOT NULL")
	}

	// DEFAULT 值
	if field.DefaultValue != "" {
		parts = append(parts, "DEFAULT "+s.quoteDefault(field.DefaultValue, field.DataType))
	}

	return strings.Join(parts, " ")
}

// mapDataTypeToPostgreSQL 数据类型映射（Model → PostgreSQL）
func (s *LogicalTableService) mapDataTypeToPostgreSQL(dataType string, length *int) string {
	switch strings.ToLower(dataType) {
	case "string":
		if length != nil && *length > 0 {
			return fmt.Sprintf("VARCHAR(%d)", *length)
		}
		return "TEXT"
	case "int":
		return "INTEGER"
	case "bigint":
		return "BIGINT"
	case "float":
		return "DOUBLE PRECISION"
	case "decimal":
		return "NUMERIC"
	case "date":
		return "DATE"
	case "datetime":
		return "TIMESTAMP"
	case "bool":
		return "BOOLEAN"
	case "json":
		return "JSONB"
	case "text":
		return "TEXT"
	case "geometry":
		return "GEOMETRY"
	default:
		return ""
	}
}

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func quoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }

func validateMaterializationKeys(config map[string]interface{}) error {
	allowedKeys := map[string]struct{}{"schema_name": {}, "table_name": {}, "partition_by": {}, "partition_type": {}}
	for key := range config {
		if _, ok := allowedKeys[key]; !ok {
			return fmt.Errorf("不支持的物化配置字段: %s", key)
		}
	}
	return nil
}

func validateMaterialization(table *models.LogicalTable, fields []models.LogicalField) error {
	config := table.Materialization
	for _, value := range []string{table.Code} {
		if !identifierPattern.MatchString(value) {
			return fmt.Errorf("标识符无效: %s", value)
		}
	}
	if config == nil {
		return nil
	}
	if err := validateMaterializationKeys(map[string]interface{}(config)); err != nil {
		return err
	}
	for _, key := range []string{"schema_name", "table_name", "partition_by"} {
		if value, ok := config[key].(string); ok && value != "" && !identifierPattern.MatchString(value) {
			return fmt.Errorf("物化配置 %s 不是合法标识符", key)
		}
	}
	if value, ok := config["partition_type"].(string); ok && value != "" {
		value = strings.ToLower(value)
		if value != "range" && value != "list" && value != "hash" {
			return fmt.Errorf("不支持的分区类型: %s", value)
		}
	}
	if partition, ok := config["partition_by"].(string); ok && partition != "" {
		found := false
		for _, field := range fields {
			if field.ColumnName == partition {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("分区字段不存在: %s", partition)
		}
	}
	for _, field := range fields {
		if !identifierPattern.MatchString(field.ColumnName) {
			return fmt.Errorf("字段标识符无效: %s", field.ColumnName)
		}
		if field.DataType == "" || (&LogicalTableService{}).mapDataTypeToPostgreSQL(field.DataType, field.Length) == "" {
			return fmt.Errorf("不支持的字段类型: %s", field.DataType)
		}
	}
	return nil
}

// quoteDefault 处理默认值引号
func (s *LogicalTableService) quoteDefault(defaultValue, dataType string) string {
	// SQL 函数白名单（不加引号）
	sqlFunctions := []string{"CURRENT_TIMESTAMP", "NOW()", "CURRENT_DATE", "NULL"}
	upperDefault := strings.ToUpper(strings.TrimSpace(defaultValue))
	for _, fn := range sqlFunctions {
		if upperDefault == fn {
			return defaultValue
		}
	}

	// 布尔类型
	if dataType == "bool" || dataType == "boolean" {
		if upperDefault == "TRUE" || upperDefault == "FALSE" {
			return defaultValue
		}
	}

	// 数值类型（int, bigint, float, decimal）
	if dataType == "int" || dataType == "bigint" || dataType == "float" || dataType == "decimal" || dataType == "numeric" {
		// 简单判断：如果只包含数字、小数点、负号，则不加引号
		if strings.TrimLeft(defaultValue, "-0123456789.") == "" {
			return defaultValue
		}
	}

	// 字符串类型：加单引号
	return "'" + strings.ReplaceAll(defaultValue, "'", "''") + "'"
}
