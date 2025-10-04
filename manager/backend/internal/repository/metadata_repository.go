package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/pkg/utils"
	"gorm.io/gorm"
)

type MetadataRepository struct {
	db            *gorm.DB
	encryptionKey []byte
}

func NewMetadataRepository(db *gorm.DB, encryptionKey []byte) *MetadataRepository {
	return &MetadataRepository{
		db:            db,
		encryptionKey: encryptionKey,
	}
}

// ScanDatabaseTables 扫描数据库中的所有表（轻量级元数据）
func (r *MetadataRepository) ScanDatabaseTables(resourceID uint, connInfo models.ConnectionInfo) ([]models.ManagedTable, error) {
	resourceType, ok := connInfo["resource_type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing resource_type in connection info")
	}

	if resourceType != "postgresql" {
		return nil, fmt.Errorf("unsupported resource type: %s", resourceType)
	}

	// 解密连接信息中的密码
	decryptedConnInfo, err := r.decryptSensitiveFields(connInfo)
	if err != nil {
		return nil, fmt.Errorf("解密连接信息失败: %w", err)
	}

	// 构建数据库连接
	host, _ := decryptedConnInfo["host"].(string)
	database, _ := decryptedConnInfo["database"].(string)
	password, _ := decryptedConnInfo["password"].(string)

	// 处理 username 字段（可能是"user"或"username"）
	username, ok := decryptedConnInfo["username"].(string)
	if !ok {
		username, _ = decryptedConnInfo["user"].(string)
	}

	// 处理 port 字段（可能是字符串或数字）
	var port string
	if portNum, ok := decryptedConnInfo["port"].(float64); ok {
		port = fmt.Sprintf("%.0f", portNum)
	} else {
		port, _ = decryptedConnInfo["port"].(string)
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, username, password, database,
	)

	// 连接到目标数据库
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// 查询所有表的轻量级元数据
	query := `
		SELECT
			table_schema,
			table_name,
			table_type,
			pg_total_relation_size(quote_ident(table_schema) || '.' || quote_ident(table_name)) as table_size,
			obj_description((quote_ident(table_schema) || '.' || quote_ident(table_name))::regclass) as comment
		FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY table_schema, table_name
	`

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []models.ManagedTable
	now := time.Now()

	for rows.Next() {
		var schemaName, tableName, tableType string
		var tableSize sql.NullInt64
		var comment sql.NullString

		if err := rows.Scan(&schemaName, &tableName, &tableType, &tableSize, &comment); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		fullName := fmt.Sprintf("%s.%s", schemaName, tableName)

		table := models.ManagedTable{
			ResourceID:   resourceID,
			SchemaName:   schemaName,
			TableName:    tableName,
			FullName:     fullName,
			IsManaged:    false,
			TableType:    tableType,
			LastScanned:  &now,
		}

		if tableSize.Valid {
			size := tableSize.Int64
			table.TableSize = &size
		}

		if comment.Valid {
			table.Comment = comment.String
		}

		tables = append(tables, table)
	}

	return tables, nil
}

// SaveOrUpdateTables 保存或更新表元数据
func (r *MetadataRepository) SaveOrUpdateTables(tables []models.ManagedTable) error {
	for _, table := range tables {
		var existing models.ManagedTable
		err := r.db.Where("resource_id = ? AND schema_name = ? AND table_name = ?",
			table.ResourceID, table.SchemaName, table.TableName).First(&existing).Error

		if err == gorm.ErrRecordNotFound {
			// 新表，创建记录
			if err := r.db.Create(&table).Error; err != nil {
				return fmt.Errorf("failed to create table record: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to query existing table: %w", err)
		} else {
			// 已存在的表，更新轻量级元数据（不覆盖IsManaged和深度元数据）
			updates := map[string]interface{}{
				"table_size":   table.TableSize,
				"table_type":   table.TableType,
				"comment":      table.Comment,
				"last_scanned": table.LastScanned,
			}
			if err := r.db.Model(&existing).Updates(updates).Error; err != nil {
				return fmt.Errorf("failed to update table record: %w", err)
			}
		}
	}
	return nil
}

// GetManagedTables 获取已纳管的表列表
func (r *MetadataRepository) GetManagedTables(resourceID uint, isManaged *bool) ([]models.ManagedTable, error) {
	var tables []models.ManagedTable
	query := r.db.Where("resource_id = ?", resourceID)

	if isManaged != nil {
		query = query.Where("is_managed = ?", *isManaged)
	}

	if err := query.Order("schema_name, table_name").Find(&tables).Error; err != nil {
		return nil, fmt.Errorf("failed to get tables: %w", err)
	}

	return tables, nil
}

// GetManagedTableByID 根据ID获取单个表
func (r *MetadataRepository) GetManagedTableByID(tableID uint) (*models.ManagedTable, error) {
	var table models.ManagedTable
	if err := r.db.First(&table, tableID).Error; err != nil {
		return nil, fmt.Errorf("failed to find table: %w", err)
	}
	return &table, nil
}

// MarkTableAsManaged 标记表为已纳管，并提取详细元数据
func (r *MetadataRepository) MarkTableAsManaged(tableID uint, connInfo models.ConnectionInfo) error {
	var table models.ManagedTable
	if err := r.db.First(&table, tableID).Error; err != nil {
		return fmt.Errorf("failed to find table: %w", err)
	}

	// 连接到数据库提取详细元数据
	schema, sampleData, rowCount, err := r.extractTableMetadata(table, connInfo)
	if err != nil {
		return fmt.Errorf("failed to extract metadata: %w", err)
	}

	now := time.Now()
	updates := map[string]interface{}{
		"is_managed":   true,
		"schema":       schema,
		"sample_data":  sampleData,
		"row_count":    rowCount,
		"last_managed": &now,
	}

	if err := r.db.Model(&table).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to mark table as managed: %w", err)
	}

	return nil
}

// extractTableMetadata 提取表的详细元数据（仅在纳管时调用）
func (r *MetadataRepository) extractTableMetadata(table models.ManagedTable, connInfo models.ConnectionInfo) (json.RawMessage, json.RawMessage, *int64, error) {
	// 解密连接信息中的密码
	decryptedConnInfo, err := r.decryptSensitiveFields(connInfo)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("解密连接信息失败: %w", err)
	}

	host, _ := decryptedConnInfo["host"].(string)
	database, _ := decryptedConnInfo["database"].(string)
	password, _ := decryptedConnInfo["password"].(string)

	// 处理 username 字段（可能是"user"或"username"）
	username, ok := decryptedConnInfo["username"].(string)
	if !ok {
		username, _ = decryptedConnInfo["user"].(string)
	}

	// 处理 port 字段（可能是字符串或数字）
	var port string
	if portNum, ok := decryptedConnInfo["port"].(float64); ok {
		port = fmt.Sprintf("%.0f", portNum)
	} else {
		port, _ = decryptedConnInfo["port"].(string)
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, username, password, database,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, nil, nil, err
	}
	defer db.Close()

	// 1. 提取字段schema
	schemaQuery := `
		SELECT
			column_name,
			data_type,
			is_nullable = 'YES' as is_nullable,
			column_default,
			(SELECT COUNT(*) > 0 FROM information_schema.table_constraints tc
				JOIN information_schema.key_column_usage kcu
				ON tc.constraint_name = kcu.constraint_name
				WHERE tc.table_schema = $1
				AND tc.table_name = $2
				AND kcu.column_name = c.column_name
				AND tc.constraint_type = 'PRIMARY KEY') as is_primary_key
		FROM information_schema.columns c
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`

	rows, err := db.Query(schemaQuery, table.SchemaName, table.TableName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer rows.Close()

	var columns []models.TableColumn
	for rows.Next() {
		var col models.TableColumn
		var defaultValue sql.NullString

		if err := rows.Scan(&col.Name, &col.DataType, &col.IsNullable, &defaultValue, &col.IsPrimaryKey); err != nil {
			return nil, nil, nil, err
		}

		if defaultValue.Valid {
			col.DefaultValue = defaultValue.String
		}

		columns = append(columns, col)
	}

	schemaJSON, err := json.Marshal(columns)
	if err != nil {
		return nil, nil, nil, err
	}

	// 2. 获取行数
	var rowCount int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", table.SchemaName, table.TableName)
	if err := db.QueryRow(countQuery).Scan(&rowCount); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to count rows: %w", err)
	}

	// 3. 采样数据（前10行）
	sampleQuery := fmt.Sprintf("SELECT * FROM %s.%s LIMIT 10", table.SchemaName, table.TableName)
	sampleRows, err := db.Query(sampleQuery)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to query sample data: %w", err)
	}
	defer sampleRows.Close()

	columnNames, err := sampleRows.Columns()
	if err != nil {
		return nil, nil, nil, err
	}

	var sampleData []map[string]interface{}
	for sampleRows.Next() {
		values := make([]interface{}, len(columnNames))
		valuePtrs := make([]interface{}, len(columnNames))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := sampleRows.Scan(valuePtrs...); err != nil {
			return nil, nil, nil, err
		}

		row := make(map[string]interface{})
		for i, name := range columnNames {
			row[name] = values[i]
		}
		sampleData = append(sampleData, row)
	}

	sampleJSON, err := json.Marshal(sampleData)
	if err != nil {
		return nil, nil, nil, err
	}

	return schemaJSON, sampleJSON, &rowCount, nil
}

// UnmarkTableAsManaged 取消表的纳管状态
func (r *MetadataRepository) UnmarkTableAsManaged(tableID uint) error {
	updates := map[string]interface{}{
		"is_managed":   false,
		"schema":       nil,
		"sample_data":  nil,
		"last_managed": nil,
	}

	if err := r.db.Model(&models.ManagedTable{}).Where("id = ?", tableID).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to unmark table: %w", err)
	}

	return nil
}

// decryptSensitiveFields 解密连接信息中的敏感字段
func (r *MetadataRepository) decryptSensitiveFields(connInfo models.ConnectionInfo) (models.ConnectionInfo, error) {
	decrypted := make(models.ConnectionInfo)
	for k, v := range connInfo {
		decrypted[k] = v
	}

	// 定义需要解密的敏感字段
	sensitiveFields := []string{"password", "access_key", "secret_key", "token", "api_key"}

	for _, field := range sensitiveFields {
		if val, exists := connInfo[field]; exists {
			if strVal, ok := val.(string); ok && strVal != "" {
				decryptedVal, err := utils.Decrypt(strVal, r.encryptionKey)
				if err != nil {
					// 如果解密失败，可能是未加密的旧数据，保持原值
					decrypted[field] = strVal
					continue
				}
				decrypted[field] = decryptedVal
			}
		}
	}

	return decrypted, nil
}
