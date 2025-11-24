package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// 连接 SQLite 数据库
	db, err := sql.Open("sqlite3", "/Users/pampa/code/addp/labs/orch/lineage-demo/worker/lineage.db")
	if err != nil {
		log.Fatal("❌ 连接数据库失败:", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("❌ 数据库连接测试失败:", err)
	}

	fmt.Println("✅ 数据库连接成功 (SQLite)")

	// 插入测试节点
	items := []struct {
		ID         int
		Name       string
		Type       string
		Schema     string
		Attributes string
	}{
		// 源数据层
		{1, "customers", "source_table", "crm", `{"database": "source_db", "description": "客户主数据"}`},
		{2, "orders", "source_table", "sales", `{"database": "source_db", "description": "订单数据"}`},
		{3, "products", "source_table", "inventory", `{"database": "source_db", "description": "产品数据"}`},
		{4, "user_behavior.json", "file", "", `{"path": "/data/raw/user_behavior.json", "format": "json"}`},
		{5, "weather_api", "api", "", `{"endpoint": "https://api.weather.com", "type": "REST"}`},

		// Staging层
		{10, "stg_customers", "staging_table", "staging", `{"database": "dwh", "description": "客户暂存表"}`},
		{11, "stg_orders", "staging_table", "staging", `{"database": "dwh", "description": "订单暂存表"}`},
		{12, "stg_products", "staging_table", "staging", `{"database": "dwh", "description": "产品暂存表"}`},
		{13, "stg_user_behavior", "staging_table", "staging", `{"database": "dwh", "description": "用户行为暂存表"}`},
		{14, "stg_weather", "staging_table", "staging", `{"database": "dwh", "description": "天气数据暂存表"}`},

		// 数据集市层
		{20, "dim_customer", "target_table", "dw", `{"database": "dwh", "description": "客户维度表"}`},
		{21, "dim_product", "target_table", "dw", `{"database": "dwh", "description": "产品维度表"}`},
		{22, "fact_order", "target_table", "dw", `{"database": "dwh", "description": "订单事实表"}`},
		{23, "fact_user_behavior", "target_table", "dw", `{"database": "dwh", "description": "用户行为事实表"}`},

		// 报表层
		{30, "v_customer_360", "view", "reports", `{"database": "dwh", "description": "客户360视图"}`},
		{31, "v_sales_report", "view", "reports", `{"database": "dwh", "description": "销售报表"}`},
		{32, "rpt_monthly_sales", "target_table", "reports", `{"database": "dwh", "description": "月度销售报表"}`},
	}

	fmt.Println("📝 插入节点数据...")
	for _, item := range items {
		_, err := db.Exec(`
			INSERT OR REPLACE INTO items (item_id, name, type, schema_name, attributes, tenant_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 1, datetime('now'), datetime('now'))
		`, item.ID, item.Name, item.Type, item.Schema, item.Attributes)
		if err != nil {
			log.Printf("⚠️  插入节点 %d 失败: %v", item.ID, err)
		} else {
			fmt.Printf("  ✓ 节点 %d: %s\n", item.ID, item.Name)
		}
	}

	// 插入血缘关系
	fmt.Println("\n🔗 插入血缘关系...")
	now := time.Now()

	relations := []struct {
		WorkflowID   string
		Engine       string
		SourceID     int
		SourceType   string
		SourcePath   string
		TargetID     int
		TargetType   string
		TargetPath   string
		LineageType  string
		TransformCfg string
		Records      int
	}{
		// 源 -> Staging
		{"etl-daily", "airflow", 1, "table", "source_db.crm.customers", 10, "table", "dwh.staging.stg_customers", "transform", `{"operation": "extract", "type": "full_load"}`, 10000},
		{"etl-daily", "airflow", 2, "table", "source_db.sales.orders", 11, "table", "dwh.staging.stg_orders", "transform", `{"operation": "extract", "type": "incremental"}`, 50000},
		{"etl-daily", "airflow", 3, "table", "source_db.inventory.products", 12, "table", "dwh.staging.stg_products", "transform", `{"operation": "extract", "type": "full_load"}`, 5000},
		{"etl-daily", "airflow", 4, "file", "/data/raw/user_behavior.json", 13, "table", "dwh.staging.stg_user_behavior", "transform", `{"operation": "parse_json", "type": "batch"}`, 100000},
		{"etl-daily", "airflow", 5, "api", "weather_api", 14, "table", "dwh.staging.stg_weather", "transform", `{"operation": "api_fetch", "type": "rest"}`, 1000},

		// Staging -> 数据集市
		{"dw-transform", "airflow", 10, "table", "dwh.staging.stg_customers", 20, "table", "dwh.dw.dim_customer", "transform", `{"operation": "scd_type2", "business_key": "customer_id"}`, 10000},
		{"dw-transform", "airflow", 12, "table", "dwh.staging.stg_products", 21, "table", "dwh.dw.dim_product", "transform", `{"operation": "scd_type1", "business_key": "product_id"}`, 5000},
		{"dw-transform", "airflow", 11, "table", "dwh.staging.stg_orders", 22, "table", "dwh.dw.fact_order", "transform", `{"operation": "fact_load", "grain": "order_line"}`, 50000},
		{"dw-transform", "airflow", 13, "table", "dwh.staging.stg_user_behavior", 23, "table", "dwh.dw.fact_user_behavior", "transform", `{"operation": "fact_load", "grain": "event"}`, 100000},

		// 数据集市 -> 视图/报表
		{"report-build", "dbt", 20, "table", "dwh.dw.dim_customer", 30, "view", "dwh.reports.v_customer_360", "reference", `{"operation": "join", "type": "view"}`, 10000},
		{"report-build", "dbt", 22, "table", "dwh.dw.fact_order", 30, "view", "dwh.reports.v_customer_360", "reference", `{"operation": "join", "type": "view"}`, 50000},
		{"report-build", "dbt", 22, "table", "dwh.dw.fact_order", 31, "view", "dwh.reports.v_sales_report", "reference", `{"operation": "aggregate", "type": "view"}`, 50000},
		{"report-build", "dbt", 21, "table", "dwh.dw.dim_product", 31, "view", "dwh.reports.v_sales_report", "reference", `{"operation": "join", "type": "view"}`, 5000},
		{"report-daily", "airflow", 31, "view", "dwh.reports.v_sales_report", 32, "table", "dwh.reports.rpt_monthly_sales", "transform", `{"operation": "materialize", "type": "snapshot"}`, 1000},
	}

	for i, rel := range relations {
		_, err := db.Exec(`
			INSERT INTO lineage_relations (
				external_workflow_id, workflow_engine,
				source_item_id, source_type, source_path,
				target_item_id, target_type, target_path,
				lineage_type, transform_config, status,
				start_time, end_time, duration_ms, records_processed, bytes_written,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, rel.WorkflowID, rel.Engine, rel.SourceID, rel.SourceType, rel.SourcePath,
			rel.TargetID, rel.TargetType, rel.TargetPath, rel.LineageType, rel.TransformCfg,
			"success",
			now.Add(-1*time.Hour).Format(time.RFC3339),
			now.Add(-50*time.Minute).Format(time.RFC3339),
			600000, rel.Records, rel.Records*50,
			now.Format(time.RFC3339), now.Format(time.RFC3339))
		if err != nil {
			log.Printf("⚠️  插入血缘关系 %d 失败 (%d -> %d): %v", i+1, rel.SourceID, rel.TargetID, err)
		} else {
			fmt.Printf("  ✓ 血缘 %d -> %d (%s)\n", rel.SourceID, rel.TargetID, rel.WorkflowID)
		}
	}

	// 统计
	var nodeCount, relationCount int
	db.QueryRow("SELECT COUNT(*) FROM items").Scan(&nodeCount)
	db.QueryRow("SELECT COUNT(*) FROM lineage_relations").Scan(&relationCount)

	fmt.Println("\n✅ 测试数据插入完成！")
	fmt.Printf("📊 节点数: %d\n", nodeCount)
	fmt.Printf("🔗 血缘关系数: %d\n", relationCount)

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📋 可用的节点ID列表：")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("源数据层：")
	fmt.Println("  1  - customers (客户表)")
	fmt.Println("  2  - orders (订单表)")
	fmt.Println("  3  - products (产品表)")
	fmt.Println("  4  - user_behavior.json (用户行为文件)")
	fmt.Println("  5  - weather_api (天气API)")
	fmt.Println()
	fmt.Println("Staging层：")
	fmt.Println("  10 - stg_customers")
	fmt.Println("  11 - stg_orders")
	fmt.Println("  12 - stg_products")
	fmt.Println("  13 - stg_user_behavior")
	fmt.Println("  14 - stg_weather")
	fmt.Println()
	fmt.Println("数据集市层：")
	fmt.Println("  20 - dim_customer (客户维度)")
	fmt.Println("  21 - dim_product (产品维度)")
	fmt.Println("  22 - fact_order (订单事实)")
	fmt.Println("  23 - fact_user_behavior (用户行为事实)")
	fmt.Println()
	fmt.Println("报表层：")
	fmt.Println("  30 - v_customer_360 (客户360视图)")
	fmt.Println("  31 - v_sales_report (销售报表视图)")
	fmt.Println("  32 - rpt_monthly_sales (月度销售报表)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("💡 使用建议：")
	fmt.Println("  1. 在界面输入任意节点ID（如：1, 10, 20, 30）")
	fmt.Println("  2. 点击「查询血缘图谱」查看完整血缘关系")
	fmt.Println("  3. 点击「查看全部」查看所有血缘")
	fmt.Println()
	fmt.Println("🌐 访问前端: http://localhost:5180")
}
