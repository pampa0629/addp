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

	// 快速检查：索引是否已存在（仅检查，不创建）
	var indexExists int
	engineDB.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM pg_indexes
		 WHERE schemaname = $1 AND tablename = $2 AND indexname = $3`,
		schema, indexTable, indexName,
	).Scan(&indexExists)

	if indexExists > 0 {
		// 索引已存在，检查通过
		check.Status = "passed"
		check.Message = fmt.Sprintf("空间索引 %s.%s (%s) 已存在", schema, indexTable, indexGeomColumn)
		check.Details["index_name"] = indexName
		check.Details["table"] = fmt.Sprintf("%s.%s", schema, indexTable)
		check.Details["column"] = indexGeomColumn
		check.Details["source_srid"] = sourceSRID
		check.Details["action_required"] = false
		return check
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

	// 快速检查：确定检查目标表（物化视图优先）
	mvName := fmt.Sprintf("%s_mv3857", table)
	var mvExists int
	engineDB.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM pg_matviews
		 WHERE schemaname = $1 AND matviewname = $2`,
		schema, mvName,
	).Scan(&mvExists)

	targetTable := table
	if mvExists > 0 {
		targetTable = mvName
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

				analyzeSQL := fmt.Sprintf("ANALYZE %s.%s", schema, targetTable)
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

	return nil
}
