package mvt

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PreparationService 准备阶段服务
type PreparationService struct {
	managerDB       *gorm.DB       // Manager 数据库连接（用于 QuickView 记录）
	resourceService ResourceService // 资源服务（用于获取引擎配置）
}

// NewPreparationService 创建准备阶段服务
func NewPreparationService(managerDB *gorm.DB, resourceService ResourceService) *PreparationService {
	return &PreparationService{
		managerDB:       managerDB,
		resourceService: resourceService,
	}
}

// getEngineDB 获取引擎数据库连接
func (ps *PreparationService) getEngineDB(ctx context.Context, engineID, tenantID uint) (*gorm.DB, error) {
	// 1. 获取引擎配置
	engine, err := ps.resourceService.GetEngine(engineID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 2. 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(engine)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 3. 创建数据库连接
	sqlDB, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 4. 配置连接参数
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// 5. 验证连接
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// 6. 创建 GORM DB 实例
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: sqlDB,
	}), &gorm.Config{})
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to create gorm db: %w", err)
	}

	return gormDB, nil
}

// RunPreparationChecks 运行所有准备检查
// geomColumn: 实际的空间字段名（从Meta模块获取）
func (ps *PreparationService) RunPreparationChecks(ctx context.Context, tenantID, engineID uint, schema, table, geomColumn string) (*models.PreparationStatus, error) {
	// 获取引擎数据库连接
	engineDB, err := ps.getEngineDB(ctx, engineID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine db: %w", err)
	}

	// 确保在函数结束时关闭连接
	defer func() {
		sqlDB, _ := engineDB.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}()

	status := &models.PreparationStatus{
		Version:     "1.0",
		Checks:      []models.PreparationCheck{},
		CompletedAt: time.Now().UTC(),
	}

	// 检查 1: 物化视图
	mvStatus := ps.checkAndCreateMaterializedView(ctx, engineDB, schema, table, geomColumn)
	status.Checks = append(status.Checks, mvStatus)

	// 检查 2: 空间索引
	indexStatus := ps.checkAndCreateSpatialIndex(ctx, engineDB, schema, table, geomColumn)
	status.Checks = append(status.Checks, indexStatus)

	// 检查 3: ANALYZE
	analyzeStatus := ps.checkAndAnalyze(ctx, engineDB, schema, table)
	status.Checks = append(status.Checks, analyzeStatus)

	// 计算总体状态
	allPassed := true
	for _, check := range status.Checks {
		if check.Status == "failed" {
			allPassed = false
			break
		}
	}

	if allPassed {
		status.OverallStatus = "passed"
		status.Summary = "准备阶段全部通过，可以开始生成瓦片"
	} else {
		status.OverallStatus = "failed"
		status.Summary = "准备阶段检查有失败项，需要手动处理"
	}

	return status, nil
}

// checkAndCreateMaterializedView 检查物化视图（仅检查，不创建 - 快速诊断）
// geomColumn: 实际的空间字段名（从Meta模块获取）
// 注意：实际的物化视图创建应该在 PrepareForCreateMVT 阶段执行，这里仅做检查
func (ps *PreparationService) checkAndCreateMaterializedView(ctx context.Context, engineDB *gorm.DB, schema, table, geomColumn string) models.PreparationCheck {
	check := models.PreparationCheck{
		Name:      "materialized_view",
		Details:   make(map[string]interface{}),
		CheckedAt: time.Now().UTC(),
	}

	// 检查表名是否已经是物化视图格式 (xxx_mv3857)
	if isAlreadyMaterializedView(table) {
		// 当前表已经是物化视图，直接检查是否存在
		var mvExists int
		mvCheckQuery := `SELECT COUNT(*) FROM pg_matviews WHERE schemaname = $1 AND matviewname = $2`
		if err := engineDB.WithContext(ctx).Raw(mvCheckQuery, schema, table).Scan(&mvExists).Error; err != nil {
			check.Status = "failed"
			check.Message = fmt.Sprintf("检查物化视图失败: %v", err)
			return check
		}

		if mvExists > 0 {
			check.Status = "passed"
			check.Message = fmt.Sprintf("物化视图 %s.%s 已存在", schema, table)
			check.Details["view_name"] = table
			check.Details["source_srid"] = 3857 // 物化视图已经是 3857
			check.Details["target_srid"] = 3857
			check.Details["action_required"] = false
			return check
		}

		// 物化视图不存在，但表名是物化视图格式，说明有问题
		check.Status = "failed"
		check.Message = fmt.Sprintf("物化视图 %s.%s 不存在（表名格式为物化视图但未找到）", schema, table)
		check.Details["view_name"] = table
		check.Details["action_required"] = true
		return check
	}

	// 快速检查：获取源表的 SRID（从 geometry_columns 查询）
	var sourceSRID int
	query := fmt.Sprintf(
		`SELECT srid FROM geometry_columns
		 WHERE f_table_schema = $1 AND f_table_name = $2
		 LIMIT 1`,
	)
	err := engineDB.WithContext(ctx).Raw(query, schema, table).Row().Scan(&sourceSRID)
	if err != nil {
		check.Status = "failed"
		check.Message = fmt.Sprintf("获取源表 SRID 失败: %v", err)
		return check
	}

	if geomColumn == "" {
		check.Status = "failed"
		check.Message = "几何列名称不能为空"
		return check
	}

	// 快速检查：获取源表的主键
	pkColumn, err := ps.getPrimaryKeyColumn(ctx, engineDB, schema, table)
	if err != nil {
		// 查询主键时的错误不应该阻止物化视图创建
		check.Details["pk_check_warning"] = fmt.Sprintf("查询主键失败: %v，将使用 row_number() 生成ID", err)
	}
	check.Details["primary_key"] = pkColumn
	if pkColumn == "" {
		check.Details["primary_key_status"] = "无主键，将生成临时ID"
	}

	// 如果源表已经是 3857，跳过物化视图
	if sourceSRID == 3857 {
		check.Status = "skipped"
		check.Message = "源表已经是 3857 坐标系，无需物化视图"
		check.Details["source_srid"] = sourceSRID
		check.Details["target_srid"] = 3857
		check.Details["action_required"] = false
		return check
	}

	// 物化视图名称
	mvName := fmt.Sprintf("%s_mv3857", table)

	// 快速检查：物化视图是否已存在（仅检查，不创建）
	var mvExists int
	mvCheckQuery := fmt.Sprintf(
		`SELECT COUNT(*) FROM pg_matviews
		 WHERE schemaname = $1 AND matviewname = $2`,
	)
	if err := engineDB.WithContext(ctx).Raw(mvCheckQuery, schema, mvName).Scan(&mvExists).Error; err != nil {
		// 查询失败，假设物化视图不存在（可能是权限问题，但继续）
		check.Details["check_warning"] = fmt.Sprintf("检查物化视图存在性失败: %v", err)
		mvExists = 0
	}

	if mvExists > 0 {
		// 物化视图已存在，检查通过
		check.Status = "passed"
		check.Message = fmt.Sprintf("物化视图 %s.%s 已存在", schema, mvName)
		check.Details["view_name"] = mvName
		check.Details["source_srid"] = sourceSRID
		check.Details["target_srid"] = 3857
		check.Details["action_required"] = false
		return check
	}

	// 物化视图不存在，标记为待创建（由准备阶段处理）
	check.Status = "failed"
	check.Message = fmt.Sprintf("物化视图 %s.%s 不存在，需要在准备阶段创建", schema, mvName)
	check.Details["view_name"] = mvName
	check.Details["source_srid"] = sourceSRID
	check.Details["target_srid"] = 3857
	check.Details["action_required"] = true
	check.Details["expected_time_seconds"] = 30 // 估计创建时间（根据表大小可能更长）
	return check
}

// checkAndCreateSpatialIndex 检查空间索引（仅检查，不创建 - 快速诊断）
// geomColumn: 实际的空间字段名（从Meta模块获取）
// 注意：实际的索引创建应该在 PrepareForCreateMVT 阶段执行，这里仅做检查
func (ps *PreparationService) checkAndCreateSpatialIndex(ctx context.Context, engineDB *gorm.DB, schema, table, geomColumn string) models.PreparationCheck {
	check := models.PreparationCheck{
		Name:      "spatial_index",
		Details:   make(map[string]interface{}),
		CheckedAt: time.Now().UTC(),
	}

	// ✅ 新增：检查表名是否已经是物化视图格式 (xxx_mv3857)
	if isAlreadyMaterializedView(table) {
		// 当前表已经是物化视图，直接在物化视图上检查索引
		indexGeomColumn := "geom_3857" // 物化视图的几何列固定为 geom_3857
		indexTable := table
		indexName := fmt.Sprintf("idx_%s_%s_gist", indexTable, indexGeomColumn)

		// 检查索引是否存在且有效
		var indexValid bool
		indexCheckErr := engineDB.WithContext(ctx).Raw(
			`SELECT i.indisvalid
			 FROM pg_indexes idx
			 JOIN pg_class c ON c.relname = idx.indexname
			 JOIN pg_index i ON i.indexrelid = c.oid
			 WHERE idx.schemaname = $1 AND idx.tablename = $2 AND idx.indexname = $3`,
			schema, indexTable, indexName,
		).Scan(&indexValid).Error

		if indexCheckErr == nil && indexValid {
			// 索引已存在且有效
			check.Status = "passed"
			check.Message = fmt.Sprintf("空间索引 %s 已存在", indexName)
			check.Details["index_name"] = indexName
			check.Details["table"] = fmt.Sprintf("%s.%s", schema, indexTable)
			check.Details["column"] = indexGeomColumn
			check.Details["action_required"] = false
			return check
		}

		// 索引不存在或无效
		if indexCheckErr == nil && !indexValid {
			// 索引存在但无效，先删除
			dropSQL := fmt.Sprintf("DROP INDEX IF EXISTS %s.%s", schema, indexName)
			engineDB.WithContext(ctx).Exec(dropSQL)
		}

		check.Status = "failed"
		check.Message = fmt.Sprintf("空间索引 %s 不存在，需要在准备阶段创建", indexName)
		check.Details["index_name"] = indexName
		check.Details["table"] = fmt.Sprintf("%s.%s", schema, indexTable)
		check.Details["column"] = indexGeomColumn
		check.Details["action_required"] = true
		check.Details["expected_time_seconds"] = 60
		return check
	}

	// 快速检查：获取源表的 SRID
	var sourceSRID int
	sridQuery := fmt.Sprintf(
		`SELECT srid FROM geometry_columns
		 WHERE f_table_schema = $1 AND f_table_name = $2
		 LIMIT 1`,
	)
	err := engineDB.WithContext(ctx).Raw(sridQuery, schema, table).Row().Scan(&sourceSRID)
	if err != nil {
		check.Status = "failed"
		check.Message = fmt.Sprintf("获取源表 SRID 失败: %v", err)
		check.Details["error"] = err.Error()
		return check
	}

	// 根据 SRID 决定在哪个表上建立索引
	var indexGeomColumn string
	var indexTable string

	mvName := fmt.Sprintf("%s_mv3857", table)

	if sourceSRID != 3857 {
		// 源表不是 3857，应该在物化视图上建立索引

		// 快速检查：物化视图是否存在
		var mvExists int
		mvCheckErr := engineDB.WithContext(ctx).Raw(
			`SELECT COUNT(*) FROM pg_matviews
			 WHERE schemaname = $1 AND matviewname = $2`,
			schema, mvName,
		).Scan(&mvExists).Error

		if mvCheckErr != nil || mvExists == 0 {
			// 物化视图不存在，标记为待处理
			check.Status = "failed"
			check.Message = fmt.Sprintf("物化视图 %s.%s 不存在，需要先创建物化视图", schema, mvName)
			check.Details["expected_mv"] = fmt.Sprintf("%s.%s", schema, mvName)
			check.Details["source_srid"] = sourceSRID
			check.Details["action_required"] = true
			return check
		}

		// 物化视图存在，设置索引参数
		indexGeomColumn = "geom_3857"
		indexTable = mvName
	} else {
		// 源表已经是 3857，直接在源表上建立索引
		indexGeomColumn = geomColumn
		indexTable = table
	}

	// 索引名称
	indexName := fmt.Sprintf("idx_%s_%s_gist", indexTable, indexGeomColumn)

	// 快速检查：索引是否已存在且有效（检查 indisvalid 字段）
	var indexValid bool
	indexCheckErr := engineDB.WithContext(ctx).Raw(
		`SELECT i.indisvalid
		 FROM pg_indexes idx
		 JOIN pg_class c ON c.relname = idx.indexname
		 JOIN pg_index i ON i.indexrelid = c.oid
		 WHERE idx.schemaname = $1 AND idx.tablename = $2 AND idx.indexname = $3`,
		schema, indexTable, indexName,
	).Scan(&indexValid).Error

	if indexCheckErr == nil && indexValid {
		// 索引已存在且有效，检查通过
		check.Status = "passed"
		check.Message = fmt.Sprintf("空间索引 %s.%s (%s) 已存在", schema, indexTable, indexGeomColumn)
		check.Details["index_name"] = indexName
		check.Details["table"] = fmt.Sprintf("%s.%s", schema, indexTable)
		check.Details["column"] = indexGeomColumn
		check.Details["source_srid"] = sourceSRID
		check.Details["action_required"] = false
		return check
	}

	// 如果索引存在但无效（INVALID），先删除
	if indexCheckErr == nil && !indexValid {
		dropSQL := fmt.Sprintf("DROP INDEX IF EXISTS %s.%s", schema, indexName)
		engineDB.WithContext(ctx).Exec(dropSQL)
	}

	// 索引不存在，标记为待创建（由准备阶段处理）
	check.Status = "failed"
	check.Message = fmt.Sprintf("空间索引 %s.%s (%s) 不存在，需要在准备阶段创建", schema, indexTable, indexGeomColumn)
	check.Details["index_name"] = indexName
	check.Details["table"] = fmt.Sprintf("%s.%s", schema, indexTable)
	check.Details["column"] = indexGeomColumn
	check.Details["source_srid"] = sourceSRID
	check.Details["action_required"] = true
	check.Details["expected_time_seconds"] = 60 // 估计创建时间（取决于表大小）
	return check
}

// checkAndAnalyze 检查统计信息（仅检查，不执行 ANALYZE - 快速诊断）
// 注意：实际的 ANALYZE 执行应该在 PrepareForCreateMVT 阶段执行，这里仅做检查
func (ps *PreparationService) checkAndAnalyze(ctx context.Context, engineDB *gorm.DB, schema, table string) models.PreparationCheck {
	check := models.PreparationCheck{
		Name:      "analyze",
		Details:   make(map[string]interface{}),
		CheckedAt: time.Now().UTC(),
	}

	// ✅ 确定检查目标表
	var targetTable string

	// 如果表名已经是物化视图格式，直接使用该表
	if isAlreadyMaterializedView(table) {
		targetTable = table
	} else {
		// 否则检查是否存在对应的物化视图
		mvName := fmt.Sprintf("%s_mv3857", table)
		var mvExists int
		engineDB.WithContext(ctx).Raw(
			`SELECT COUNT(*) FROM pg_matviews
			 WHERE schemaname = $1 AND matviewname = $2`,
			schema, mvName,
		).Scan(&mvExists)

		if mvExists > 0 {
			targetTable = mvName
		} else {
			targetTable = table
		}
	}

	// 快速检查：统计信息是否已更新
	var lastAnalyzeTime struct {
		LastAnalyze     *time.Time
		LastAutoAnalyze *time.Time
	}
	engineDB.WithContext(ctx).Raw(
		`SELECT last_analyze as last_analyze, last_autoanalyze as last_autoanalyze
		 FROM pg_stat_user_tables
		 WHERE schemaname = $1 AND relname = $2`,
		schema, targetTable,
	).Scan(&lastAnalyzeTime)

	// 如果已经执行过 ANALYZE（手动或自动），检查通过
	if lastAnalyzeTime.LastAnalyze != nil || lastAnalyzeTime.LastAutoAnalyze != nil {
		check.Status = "passed"
		check.Message = "统计信息已更新"
		check.Details["last_analyze"] = lastAnalyzeTime.LastAnalyze
		check.Details["last_autoanalyze"] = lastAnalyzeTime.LastAutoAnalyze
		check.Details["action_required"] = false
		return check
	}

	// 统计信息不存在，标记为待处理（由准备阶段执行）
	check.Status = "failed"
	check.Message = fmt.Sprintf("表 %s.%s 缺少统计信息，需要在准备阶段执行 ANALYZE", schema, targetTable)
	check.Details["target_table"] = targetTable
	check.Details["action_required"] = true
	check.Details["expected_time_seconds"] = 10 // 估计执行时间（取决于表大小）
	return check
}

// GetMaterializedViewName 根据源表 SRID 决定是否需要物化视图
func (ps *PreparationService) GetMaterializedViewName(ctx context.Context, engineID, tenantID uint, schema, table string) (string, error) {
	// 获取引擎数据库连接
	engineDB, err := ps.getEngineDB(ctx, engineID, tenantID)
	if err != nil {
		return "", fmt.Errorf("failed to get engine db: %w", err)
	}
	defer func() {
		sqlDB, _ := engineDB.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}()

	// 获取源表的 SRID
	var sourceSRID int
	query := fmt.Sprintf(
		`SELECT srid FROM geometry_columns
		 WHERE f_table_schema = $1 AND f_table_name = $2
		 LIMIT 1`,
	)
	err = engineDB.WithContext(ctx).Raw(query, schema, table).Scan(&sourceSRID).Error
	if err != nil {
		return "", err
	}

	// 如果源表已经是 3857，使用源表
	if sourceSRID == 3857 {
		return fmt.Sprintf("%s.%s", schema, table), nil
	}

	// 返回物化视图名称
	return fmt.Sprintf("%s.%s_mv3857", schema, table), nil
}

// RefreshMaterializedView 刷新物化视图
func (ps *PreparationService) RefreshMaterializedView(ctx context.Context, engineID, tenantID uint, schema, table string) error {
	// 获取引擎数据库连接
	engineDB, err := ps.getEngineDB(ctx, engineID, tenantID)
	if err != nil {
		return fmt.Errorf("failed to get engine db: %w", err)
	}
	defer func() {
		sqlDB, _ := engineDB.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}()

	mvName := fmt.Sprintf("%s_mv3857", table)
	refreshSQL := fmt.Sprintf(`REFRESH MATERIALIZED VIEW CONCURRENTLY %s.%s`, schema, mvName)
	return engineDB.WithContext(ctx).Exec(refreshSQL).Error
}

// getPrimaryKeyColumn 获取表的主键列名
// 返回主键列名，如果没有主键返回空字符串
func (ps *PreparationService) getPrimaryKeyColumn(ctx context.Context, engineDB *gorm.DB, schema, table string) (string, error) {
	var pkColumn string
	query := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = ($1 || '.' || $2)::regclass
		  AND i.indisprimary
		LIMIT 1
	`
	err := engineDB.WithContext(ctx).Raw(query, schema, table).Scan(&pkColumn).Error
	if err != nil {
		// 如果查询失败（可能是没有主键），返回空字符串而不是错误
		return "", nil
	}
	return pkColumn, nil
}

// ExecutePreparation 执行准备工作，在检查之后实际执行必要的操作
func (ps *PreparationService) ExecutePreparation(ctx context.Context, tenantID, engineID uint, schema, table, geomColumn string, prepStatus *models.PreparationStatus) error {
	// 获取引擎数据库连接
	engineDB, err := ps.getEngineDB(ctx, engineID, tenantID)
	if err != nil {
		return fmt.Errorf("failed to get engine db: %w", err)
	}

	// 确保在函数结束时关闭连接
	defer func() {
		sqlDB, _ := engineDB.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}()

	// 遍历所有检查项，对于 action_required=true 的项目执行相应操作
	for i, check := range prepStatus.Checks {
		actionRequired := false
		if v, ok := check.Details["action_required"]; ok {
			if b, ok := v.(bool); ok {
				actionRequired = b
			}
		}

		if !actionRequired {
			continue
		}

		switch check.Name {
		case "materialized_view":
			// 执行物化视图创建
			if check.Status == "failed" {
				mvName := fmt.Sprintf("%s_mv3857", table)
				mvFullName := fmt.Sprintf("%s.%s", schema, mvName)

				// 查询主键列名
				pkColumn, err := ps.getPrimaryKeyColumn(ctx, engineDB, schema, table)
				if err != nil {
					prepStatus.Checks[i].Status = "failed"
					prepStatus.Checks[i].Message = fmt.Sprintf("查询主键失败: %v", err)
					return fmt.Errorf("failed to get primary key: %w", err)
				}

				// 构建SELECT子句
				var selectClause string
				if pkColumn != "" {
					// 有主键，保留原始主键列名（不重命名，用双引号包裹以保留大小写）
					selectClause = fmt.Sprintf(`"%s"`, pkColumn)
					prepStatus.Checks[i].Details["primary_key"] = pkColumn
				} else {
					// 没有主键，生成临时ID
					selectClause = "row_number() OVER () AS id"
					prepStatus.Checks[i].Details["primary_key"] = "id"  // 生成的ID列名为 id
					prepStatus.Checks[i].Details["warning"] = "源表无主键，使用 row_number() 生成临时ID"
				}

				// 创建物化视图（转换为 3857）
				// 注意：列名用双引号包裹以保留大小写（PostgreSQL 区分大小写）
				createMVSQL := fmt.Sprintf(`
					CREATE MATERIALIZED VIEW %s AS
					SELECT
						%s,
						ST_Transform("%s", 3857) AS geom_3857
					FROM "%s"."%s"
					WHERE "%s" IS NOT NULL
				`, mvFullName, selectClause, geomColumn, schema, table, geomColumn)

				if err := engineDB.WithContext(ctx).Exec(createMVSQL).Error; err != nil {
					prepStatus.Checks[i].Status = "failed"
					prepStatus.Checks[i].Message = fmt.Sprintf("物化视图创建失败: %v", err)
					return fmt.Errorf("failed to create materialized view: %w", err)
				}

				// 创建成功
				prepStatus.Checks[i].Status = "passed"
				if pkColumn != "" {
					prepStatus.Checks[i].Message = fmt.Sprintf("物化视图 %s 创建成功（主键: %s）", mvFullName, pkColumn)
				} else {
					prepStatus.Checks[i].Message = fmt.Sprintf("物化视图 %s 创建成功（生成临时ID）", mvFullName)
				}
				prepStatus.Checks[i].Details["action_required"] = false
			}

		case "spatial_index":
			// 执行空间索引创建
			if check.Status == "failed" {
				// 从 Details 中获取索引信息
				indexName, _ := check.Details["index_name"].(string)
				indexTableFull, _ := check.Details["table"].(string) // 格式: schema.table
				indexColumn, _ := check.Details["column"].(string)

				if indexName == "" || indexTableFull == "" || indexColumn == "" {
					prepStatus.Checks[i].Status = "failed"
					prepStatus.Checks[i].Message = "索引信息不完整，无法创建"
					continue
				}

				// indexTableFull 格式为 "schema.table"，需要分离并使用物化视图名称
				mvName := fmt.Sprintf("%s_mv3857", table)

				// 创建 GIST 空间索引（使用 CONCURRENTLY 避免锁表）
				createIndexSQL := fmt.Sprintf(`
					CREATE INDEX CONCURRENTLY "%s" ON "%s"."%s" USING GIST ("%s")
				`, indexName, schema, mvName, indexColumn)

				if err := engineDB.WithContext(ctx).Exec(createIndexSQL).Error; err != nil {
					prepStatus.Checks[i].Status = "failed"
					prepStatus.Checks[i].Message = fmt.Sprintf("空间索引创建失败: %v", err)
					return fmt.Errorf("failed to create spatial index: %w", err)
				}

				// 创建成功
				prepStatus.Checks[i].Status = "passed"
				prepStatus.Checks[i].Message = fmt.Sprintf("空间索引 %s 创建成功", indexName)
				prepStatus.Checks[i].Details["action_required"] = false
			}

		case "analyze":
			// 执行 ANALYZE（如果需要）
			if check.Status == "failed" {
				targetTable := table
				// 获取目标表名
				if v, ok := check.Details["target_table"]; ok {
					if s, ok := v.(string); ok {
						targetTable = s
					}
				}

				analyzeSQL := fmt.Sprintf("ANALYZE \"%s\".\"%s\"", schema, targetTable)
				if err := engineDB.WithContext(ctx).Exec(analyzeSQL).Error; err != nil {
					// ANALYZE失败但不中断流程
					prepStatus.Checks[i].Status = "warning"
					prepStatus.Checks[i].Message = fmt.Sprintf("ANALYZE 执行失败: %v，继续进行", err)
				} else {
					// ANALYZE成功
					prepStatus.Checks[i].Status = "passed"
					prepStatus.Checks[i].Message = fmt.Sprintf("统计信息已更新")
					prepStatus.Checks[i].Details["action_required"] = false
				}
			}
		}
	}

	// 更新总体状态
	allPassed := true
	for _, check := range prepStatus.Checks {
		if check.Status == "failed" {
			allPassed = false
			break
		}
	}

	if allPassed {
		prepStatus.OverallStatus = "passed"
		prepStatus.Summary = "准备工作全部完成，可以开始生成瓦片"
	} else {
		prepStatus.OverallStatus = "failed"
		prepStatus.Summary = "准备工作有失败项，请检查详情"
	}

	return nil
}
