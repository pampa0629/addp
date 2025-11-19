package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("元数据扫描问题诊断")
	fmt.Println("========================================\n")

	// 测试1: 连接业务数据库
	fmt.Println("【测试1】连接业务数据库...")
	businessDB, err := connectBusinessDB()
	if err != nil {
		log.Fatalf("❌ 连接业务数据库失败: %v", err)
	}
	fmt.Println("✅ 业务数据库连接成功")
	defer businessDB.Close()

	// 测试2: 查询业务数据库的表
	fmt.Println("\n【测试2】查询业务数据库的表...")
	tables, err := scanTables(businessDB, "public")
	if err != nil {
		log.Fatalf("❌ 查询表失败: %v", err)
	}
	fmt.Printf("✅ 查询成功，找到 %d 张表:\n", len(tables))
	for i, table := range tables {
		fmt.Printf("   %d. %s (%s)\n", i+1, table.Name, table.Type)
	}

	// 测试3: 连接系统数据库
	fmt.Println("\n【测试3】连接系统数据库（ADDP元数据库）...")
	systemDB, err := connectSystemDB()
	if err != nil {
		log.Fatalf("❌ 连接系统数据库失败: %v", err)
	}
	fmt.Println("✅ 系统数据库连接成功")
	defer systemDB.Close()

	// 测试4: 写入测试数据到 meta_node
	fmt.Println("\n【测试4】测试写入 meta_node...")
	nodeID, err := insertTestNode(systemDB)
	if err != nil {
		log.Fatalf("❌ 写入 meta_node 失败: %v", err)
	}
	fmt.Printf("✅ 成功写入 meta_node，节点ID: %d\n", nodeID)

	// 测试5: 写入测试数据到 meta_item
	fmt.Println("\n【测试5】测试写入 meta_item...")
	itemID, err := insertTestItem(systemDB, nodeID)
	if err != nil {
		log.Fatalf("❌ 写入 meta_item 失败: %v", err)
	}
	fmt.Printf("✅ 成功写入 meta_item，表ID: %d\n", itemID)

	// 测试6: 验证写入的数据
	fmt.Println("\n【测试6】验证写入的数据...")
	count, err := countTestItems(systemDB, nodeID)
	if err != nil {
		log.Fatalf("❌ 查询验证失败: %v", err)
	}
	fmt.Printf("✅ 验证成功，节点下有 %d 条记录\n", count)

	// 清理测试数据
	fmt.Println("\n【清理】删除测试数据...")
	if err := cleanupTestData(systemDB, nodeID, itemID); err != nil {
		log.Printf("⚠️  清理失败: %v", err)
	} else {
		fmt.Println("✅ 测试数据已清理")
	}

	fmt.Println("\n========================================")
	fmt.Println("诊断结论:")
	fmt.Println("所有测试通过，说明:")
	fmt.Println("  ✅ 业务数据库连接正常")
	fmt.Println("  ✅ 表查询正常")
	fmt.Println("  ✅ meta_node 写入正常")
	fmt.Println("  ✅ meta_item 写入正常")
	fmt.Println("")
	fmt.Println("问题可能在于:")
	fmt.Println("  1. Meta扫描服务在实际扫描时使用了错误的连接字符串")
	fmt.Println("  2. 扫描过程中的错误被静默忽略")
	fmt.Println("  3. 事务未提交或回滚")
	fmt.Println("")
	fmt.Println("建议: 在扫描服务中添加详细日志查看实际执行情况")
	fmt.Println("========================================")
}

type TableInfo struct {
	Name string
	Type string
}

func connectBusinessDB() (*sql.DB, error) {
	connStr := "host=localhost port=5433 user=business password=business_password dbname=business sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

func connectSystemDB() (*sql.DB, error) {
	connStr := "host=localhost port=5432 user=addp password=addp_password dbname=addp sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

func scanTables(db *sql.DB, schemaName string) ([]TableInfo, error) {
	query := `
		SELECT
			t.table_name,
			t.table_type
		FROM information_schema.tables t
		WHERE t.table_schema = $1
		ORDER BY t.table_name
	`

	rows, err := db.Query(query, schemaName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var table TableInfo
		if err := rows.Scan(&table.Name, &table.Type); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}

	return tables, rows.Err()
}

func insertTestNode(db *sql.DB) (uint, error) {
	query := `
		INSERT INTO metadata.meta_node
		(tenant_id, res_id, node_type, name, full_name, depth, status, scan_status, path, attributes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`

	var id uint
	err := db.QueryRow(
		query,
		1,           // tenant_id
		999,         // res_id (测试用)
		"schema",    // node_type
		"test_diag", // name
		"test_diag", // full_name
		1,           // depth
		"active",    // status
		"已扫描",      // scan_status
		"999",       // path
		"{}",        // attributes
	).Scan(&id)

	return id, err
}

func insertTestItem(db *sql.DB, nodeID uint) (uint, error) {
	query := `
		INSERT INTO metadata.meta_item
		(tenant_id, res_id, node_id, item_type, name, full_name, fingerprint, status, meta_schema_version, attributes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`

	var id uint
	err := db.QueryRow(
		query,
		1,                            // tenant_id
		999,                          // res_id
		nodeID,                       // node_id
		"table",                      // item_type
		"test_table",                 // name
		"test_diag.test_table",       // full_name
		"test_fingerprint_12345",     // fingerprint
		"active",                     // status
		1,                            // meta_schema_version
		`{"test": true}`,             // attributes
	).Scan(&id)

	return id, err
}

func countTestItems(db *sql.DB, nodeID uint) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM metadata.meta_item WHERE node_id = $1", nodeID).Scan(&count)
	return count, err
}

func cleanupTestData(db *sql.DB, nodeID, itemID uint) error {
	_, err := db.Exec("DELETE FROM metadata.meta_item WHERE id = $1", itemID)
	if err != nil {
		return err
	}
	_, err = db.Exec("DELETE FROM metadata.meta_node WHERE id = $1", nodeID)
	return err
}
