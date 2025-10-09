package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/addp/common/utils"
	"github.com/addp/manager/internal/models"
	pq "github.com/lib/pq"
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
			ResourceID:  resourceID,
			SchemaName:  schemaName,
			TableName:   tableName,
			FullName:    fullName,
			IsManaged:   false,
			TableType:   tableType,
			LastScanned: &now,
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

// ListScannedNodesAndItems 获取已扫描的顶层节点、子节点和条目
func (r *MetadataRepository) ListScannedNodesAndItems() ([]models.MetaNodeLite, []models.MetaNodeLite, []models.MetaItemLite, error) {
	var topNodes []models.MetaNodeLite
	if err := r.db.Table("metadata.meta_node AS n").
		Select("n.id, r.resource_id, n.res_id, n.parent_node_id, n.node_type, n.name, n.full_name, n.path, n.depth, n.last_scan_at, n.item_count, n.total_size_bytes, n.attributes").
		Joins("JOIN metadata.meta_resource AS r ON r.id = n.res_id").
		Where("n.parent_node_id IS NULL AND n.scan_status = ?", "已扫描").
		Order("r.resource_id, n.name").
		Scan(&topNodes).Error; err != nil {
		return nil, nil, nil, fmt.Errorf("failed to query top-level nodes: %w", err)
	}

	if len(topNodes) == 0 {
		return []models.MetaNodeLite{}, []models.MetaNodeLite{}, []models.MetaItemLite{}, nil
	}

	metaResIDs := make([]uint, 0, len(topNodes))
	for _, node := range topNodes {
		metaResIDs = append(metaResIDs, node.ResID)
	}

	var childNodes []models.MetaNodeLite
	if err := r.db.Table("metadata.meta_node AS n").
		Select("n.id, r.resource_id, n.res_id, n.parent_node_id, n.node_type, n.name, n.full_name, n.path, n.depth, n.last_scan_at, n.item_count, n.total_size_bytes, n.attributes").
		Joins("JOIN metadata.meta_resource AS r ON r.id = n.res_id").
		Where("n.parent_node_id IS NOT NULL").
		Where("n.res_id IN ?", metaResIDs).
		Order("n.depth, n.name").
		Scan(&childNodes).Error; err != nil {
		return nil, nil, nil, fmt.Errorf("failed to query descendant nodes: %w", err)
	}

	var items []models.MetaItemLite
	if err := r.db.Table("metadata.meta_item AS i").
		Select("i.id, r.resource_id, i.res_id, i.node_id, i.item_type, i.name, i.full_name, i.row_count, i.size_bytes, i.object_size_bytes, i.last_modified_at, i.attributes").
		Joins("JOIN metadata.meta_resource AS r ON r.id = i.res_id").
		Where("i.res_id IN ?", metaResIDs).
		Scan(&items).Error; err != nil {
		return nil, nil, nil, fmt.Errorf("failed to query meta items: %w", err)
	}

	return topNodes, childNodes, items, nil
}

// GetObjectMetadataItem 获取对象存储路径对应的元数据项记录
func (r *MetadataRepository) GetObjectMetadataItem(resourceID uint, bucketName, objectPath string) (*models.MetaItemLite, error) {
	var item models.MetaItemLite
	err := r.db.Table("metadata.meta_item AS i").
	Select("i.id, r.resource_id, i.res_id, i.node_id, i.item_type, i.name, i.full_name, i.row_count, i.size_bytes, i.object_size_bytes, i.last_modified_at, i.attributes").
	Joins("JOIN metadata.meta_resource AS r ON r.id = i.res_id").
		Where("r.resource_id = ?", resourceID).
		Where("i.item_type = ?", "object").
		Where("(i.attributes ->> 'bucket') = ?", bucketName).
		Where("(i.attributes ->> 'path') = ? OR (i.attributes ->> 'relative_path') = ?", objectPath, objectPath).
		First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, fmt.Errorf("failed to query object metadata: %w", err)
	}
	return &item, nil
}

// GetObjectMetadataNode 获取对象存储节点（bucket/prefix）的元数据
func (r *MetadataRepository) GetObjectMetadataNode(resourceID uint, bucketName, relativePath string) (*models.MetaNodeLite, error) {
	var node models.MetaNodeLite
 query := r.db.Table("metadata.meta_node AS n").
	Select("n.id, r.resource_id, n.res_id, n.parent_node_id, n.node_type, n.name, n.full_name, n.path, n.depth, n.last_scan_at, n.item_count, n.total_size_bytes, n.attributes").
	Joins("JOIN metadata.meta_resource AS r ON r.id = n.res_id").
		Where("r.resource_id = ?", resourceID)

	cleanPath := strings.Trim(relativePath, "/")
	if cleanPath == "" {
		query = query.Where("n.parent_node_id IS NULL").Where("n.name = ?", bucketName)
	} else {
		query = query.Where("n.node_type IN ('bucket','prefix')").
			Where("(n.attributes ->> 'path') = ? OR n.full_name = ?", cleanPath, cleanPath)
	}

	if err := query.First(&node).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, fmt.Errorf("failed to query object metadata node: %w", err)
	}

	return &node, nil
}

// QueryTablePreview 查询表数据预览
func (r *MetadataRepository) QueryTablePreview(resource *models.Resource, schemaName, tableName string, page, pageSize, maxRows int) ([]string, []map[string]interface{}, int, []string, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}

	// 处理 tableName 可能包含 schema 前缀的情况
	// 例如: tableName = "products.categories", schemaName = "products"
	// 需要提取出真正的表名 "categories"
	if strings.Contains(tableName, ".") {
		parts := strings.Split(tableName, ".")
		// 取最后一部分作为实际表名
		tableName = parts[len(parts)-1]
	}

	decryptedConnInfo, err := r.decryptSensitiveFields(resource.ConnectionInfo)
	if err != nil {
		return nil, nil, 0, nil, fmt.Errorf("解密连接信息失败: %w", err)
	}

	host, _ := decryptedConnInfo["host"].(string)
	if host == "localhost" || host == "127.0.0.1" {
		if alias := os.Getenv("RESOURCE_LOCALHOST_ALIAS"); alias != "" {
			host = alias
		} else {
			host = "127.0.0.1"
		}
	}

	database, _ := decryptedConnInfo["database"].(string)
	password, _ := decryptedConnInfo["password"].(string)

	username, ok := decryptedConnInfo["username"].(string)
	if !ok {
		username, _ = decryptedConnInfo["user"].(string)
	}

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
		return nil, nil, 0, nil, err
	}
	defer db.Close()

	columnsQuery := `SELECT column_name, udt_name FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position`
	colsRows, err := db.Query(columnsQuery, schemaName, tableName)
	if err != nil {
		return nil, nil, 0, nil, fmt.Errorf("failed to query columns: %w", err)
	}

	type columnInfo struct {
		name string
		udt  string
	}

	var columnInfos []columnInfo
	var columns []string
	var geometryColumns []string
	for colsRows.Next() {
		var col, udt string
		if err := colsRows.Scan(&col, &udt); err != nil {
			colsRows.Close()
			return nil, nil, 0, nil, err
		}
		columns = append(columns, col)
		columnInfos = append(columnInfos, columnInfo{name: col, udt: udt})
		if udt == "geometry" || udt == "geography" {
			geometryColumns = append(geometryColumns, col)
		}
	}
	colsRows.Close()

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", pq.QuoteIdentifier(schemaName), pq.QuoteIdentifier(tableName))
	var totalCount int64
	if err := db.QueryRow(countQuery).Scan(&totalCount); err != nil {
		return columns, nil, 0, nil, fmt.Errorf("failed to count rows: %w", err)
	}
	if totalCount > int64(maxRows) {
		totalCount = int64(maxRows)
	}

	offset := (page - 1) * pageSize
	if offset >= int(totalCount) {
		return columns, []map[string]interface{}{}, int(totalCount), geometryColumns, nil
	}

	limit := pageSize
	if offset+limit > int(totalCount) {
		limit = int(totalCount) - offset
	}

	selectColumns := make([]string, len(columnInfos))
	for i, info := range columnInfos {
		identifier := pq.QuoteIdentifier(info.name)
		if info.udt == "geometry" || info.udt == "geography" {
			selectColumns[i] = fmt.Sprintf("ST_AsGeoJSON(%s) AS %s", identifier, identifier)
		} else {
			selectColumns[i] = identifier
		}
	}

	dataQuery := fmt.Sprintf("SELECT %s FROM %s.%s LIMIT %d OFFSET %d", strings.Join(selectColumns, ", "), pq.QuoteIdentifier(schemaName), pq.QuoteIdentifier(tableName), limit, offset)
	dataRows, err := db.Query(dataQuery)
	if err != nil {
		return columns, nil, 0, nil, fmt.Errorf("failed to query data: %w", err)
	}
	defer dataRows.Close()

	queryColumns := columns
	if len(queryColumns) == 0 {
		queryColumns, err = dataRows.Columns()
		if err != nil {
			return columns, nil, 0, nil, err
		}
	}

	var rows []map[string]interface{}
	for dataRows.Next() {
		values := make([]interface{}, len(queryColumns))
		valuePtrs := make([]interface{}, len(queryColumns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := dataRows.Scan(valuePtrs...); err != nil {
			return columns, nil, 0, nil, err
		}

		row := make(map[string]interface{})
		for i, name := range queryColumns {
			val := values[i]
			if val == nil {
				row[name] = nil
				continue
			}
			switch v := val.(type) {
			case []byte:
				row[name] = string(v)
			case time.Time:
				row[name] = v.Format(time.RFC3339)
			default:
				row[name] = v
			}
		}
		rows = append(rows, row)
	}

	return columns, rows, int(totalCount), geometryColumns, nil
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

// DecryptConnectionInfo 对外暴露的连接信息解密方法
func (r *MetadataRepository) DecryptConnectionInfo(connInfo models.ConnectionInfo) (models.ConnectionInfo, error) {
	return r.decryptSensitiveFields(connInfo)
}

// GetNodeByName 根据资源ID和节点名称获取节点信息
func (r *MetadataRepository) GetNodeByName(resourceID uint, nodeName string) (*models.MetaNodeLite, error) {
	var node models.MetaNodeLite
	err := r.db.Table("metadata.meta_node AS n").
		Select("n.id, r.resource_id, n.res_id, n.parent_node_id, n.node_type, n.name, n.full_name, n.path, n.depth, n.last_scan_at, n.item_count, n.total_size_bytes, n.attributes").
		Joins("JOIN metadata.meta_resource AS r ON r.id = n.res_id").
		Where("r.resource_id = ? AND n.name = ? AND n.parent_node_id IS NULL", resourceID, nodeName).
		First(&node).Error
	if err != nil {
		return nil, err
	}
	return &node, nil
}

// GetChildNodes 获取节点的直接子节点
func (r *MetadataRepository) GetChildNodes(parentNodeID uint) ([]models.MetaNodeLite, error) {
	var nodes []models.MetaNodeLite
	err := r.db.Table("metadata.meta_node AS n").
		Select("n.id, 0 as resource_id, n.res_id, n.parent_node_id, n.node_type, n.name, n.full_name, n.path, n.depth, n.last_scan_at, n.item_count, n.total_size_bytes, n.attributes").
		Where("n.parent_node_id = ?", parentNodeID).
		Order("n.name").
		Find(&nodes).Error
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

// GetNodeItems 获取节点下的所有子项（表/对象）
func (r *MetadataRepository) GetNodeItems(nodeID uint) ([]models.MetaItemLite, error) {
	var items []models.MetaItemLite
	err := r.db.Table("metadata.meta_item AS i").
		Select("i.id, 0 as resource_id, i.res_id, i.node_id, i.item_type, i.name, i.full_name, i.row_count, i.size_bytes, i.object_size_bytes, i.last_modified_at, i.attributes").
		Where("i.node_id = ?", nodeID).
		Order("i.name").
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}
