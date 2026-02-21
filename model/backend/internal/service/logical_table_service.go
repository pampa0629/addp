package service

import (
	"fmt"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

type LogicalTableService struct {
	repo           *repository.LogicalTableRepository
	standardClient *commonClient.StandardClient
}

func NewLogicalTableService(repo *repository.LogicalTableRepository, standardURL, internalKey string) *LogicalTableService {
	var standardClient *commonClient.StandardClient
	if standardURL != "" {
		standardClient = commonClient.NewStandardClientWithInternalKey(standardURL, internalKey)
	}
	return &LogicalTableService{
		repo:           repo,
		standardClient: standardClient,
	}
}

func (s *LogicalTableService) CreateLogicalTable(req *models.CreateLogicalTableRequest, tenantID, userID int64) (*models.LogicalTable, error) {
	exists, err := s.repo.ExistsByCode(req.Code, tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("逻辑表编码 '%s' 已存在", req.Code)
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
		Materialization:  req.Materialization,
		CreatedBy:        userID,
	}

	if err := s.repo.Create(table); err != nil {
		return nil, err
	}
	return table, nil
}

func (s *LogicalTableService) GetLogicalTable(id, tenantID int64) (*models.LogicalTable, error) {
	return s.repo.GetByID(id, tenantID)
}

func (s *LogicalTableService) ListLogicalTables(tenantID int64, opts repository.ListLogicalTableOptions) ([]models.LogicalTable, int64, error) {
	return s.repo.List(tenantID, opts)
}

func (s *LogicalTableService) UpdateLogicalTable(id, tenantID, userID int64, req *models.UpdateLogicalTableRequest) (*models.LogicalTable, error) {
	table, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		table.Name = req.Name
	}
	if req.DomainID != nil {
		table.DomainID = req.DomainID
	}
	if req.EntityID != nil {
		table.EntityID = req.EntityID
	}
	table.Description = req.Description
	if req.TableType != "" {
		table.TableType = req.TableType
	}
	if req.Layer != "" {
		table.Layer = req.Layer
	}
	table.GrainDescription = req.GrainDescription
	table.SCDType = req.SCDType
	if req.Materialization != nil {
		table.Materialization = req.Materialization
	}
	table.UpdatedBy = &userID

	if err := s.repo.Update(table); err != nil {
		return nil, err
	}
	return table, nil
}

func (s *LogicalTableService) DeleteLogicalTable(id, tenantID int64) error {
	return s.repo.Delete(id, tenantID)
}

// GetFields 获取逻辑表字段列表
func (s *LogicalTableService) GetFields(tableID, tenantID int64) ([]models.LogicalField, error) {
	if _, err := s.repo.GetByID(tableID, tenantID); err != nil {
		return nil, fmt.Errorf("逻辑表不存在")
	}
	return s.repo.GetFields(tableID)
}

// CreateField 创建字段
func (s *LogicalTableService) CreateField(tableID, tenantID int64, req *models.CreateLogicalFieldRequest) (*models.LogicalField, error) {
	if _, err := s.repo.GetByID(tableID, tenantID); err != nil {
		return nil, fmt.Errorf("逻辑表不存在")
	}

	fieldRole := req.FieldRole
	if fieldRole == "" {
		fieldRole = "regular"
	}
	field := &models.LogicalField{
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

	if err := s.repo.CreateField(field); err != nil {
		return nil, err
	}
	return field, nil
}

// UpdateField 更新字段
func (s *LogicalTableService) UpdateField(fieldID, tableID, tenantID int64, req *models.UpdateLogicalFieldRequest) (*models.LogicalField, error) {
	if _, err := s.repo.GetByID(tableID, tenantID); err != nil {
		return nil, fmt.Errorf("逻辑表不存在")
	}

	field, err := s.repo.GetFieldByID(fieldID, tableID)
	if err != nil {
		return nil, fmt.Errorf("字段不存在")
	}

	if req.Name != "" {
		field.Name = req.Name
	}
	if req.ColumnName != "" {
		field.ColumnName = req.ColumnName
	}
	if req.DataType != "" {
		field.DataType = req.DataType
	}
	field.ElementID = req.ElementID
	field.Length = req.Length
	if req.Nullable != nil {
		field.Nullable = *req.Nullable
	}
	if req.IsPK != nil {
		field.IsPK = *req.IsPK
	}
	if req.IsPartition != nil {
		field.IsPartition = *req.IsPartition
	}
	field.DefaultValue = req.DefaultValue
	field.Description = req.Description
	if req.SortOrder != nil {
		field.SortOrder = *req.SortOrder
	}
	if req.FieldRole != "" {
		field.FieldRole = req.FieldRole
	}
	field.HierarchyID = req.HierarchyID
	field.HierarchyLevel = req.HierarchyLevel

	if err := s.repo.UpdateField(field); err != nil {
		return nil, err
	}
	return field, nil
}

// DeleteField 删除字段
func (s *LogicalTableService) DeleteField(fieldID, tableID, tenantID int64) error {
	if _, err := s.repo.GetByID(tableID, tenantID); err != nil {
		return fmt.Errorf("逻辑表不存在")
	}
	return s.repo.DeleteField(fieldID, tableID)
}

// PreviewDDL 预览生成的 DDL（仅支持 PostgreSQL）
func (s *LogicalTableService) PreviewDDL(tableID, tenantID int64) (string, error) {
	table, err := s.repo.GetByID(tableID, tenantID)
	if err != nil {
		return "", fmt.Errorf("逻辑表不存在")
	}

	fields, err := s.repo.GetFields(tableID)
	if err != nil {
		return "", err
	}

	if len(fields) == 0 {
		return "", fmt.Errorf("逻辑表还没有定义字段，无法生成 DDL")
	}

	return s.generatePostgreSQLDDL(table, fields), nil
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
	ddl.WriteString(fmt.Sprintf("CREATE TABLE %s.%s (\n", schemaName, tableName))

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
		ddl.WriteString(strings.Join(pkFields, ", "))
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
			ddl.WriteString(fmt.Sprintf("\nPARTITION BY %s (%s)", partitionType, partitionBy))
		}

		// 7. 额外选项（可选）
		if extraOpts, ok := table.Materialization["extra_options"].(string); ok && extraOpts != "" {
			ddl.WriteString("\n")
			ddl.WriteString(extraOpts)
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
	parts = append(parts, "  "+field.ColumnName)

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
	case "decimal", "numeric":
		return "NUMERIC"
	case "date":
		return "DATE"
	case "datetime", "timestamp":
		return "TIMESTAMP"
	case "bool", "boolean":
		return "BOOLEAN"
	case "json", "jsonb":
		return "JSONB"
	case "text":
		return "TEXT"
	case "geometry":
		return "GEOMETRY"
	default:
		return "TEXT"
	}
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
