package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
)

func main() {
	// 方案1: 测试明文密码连接
	connStr1 := "host=localhost port=5433 user=business password=business_password dbname=business sslmode=disable"

	fmt.Println("=== 测试方案1: 明文密码连接 ===")
	testConnection(connStr1)

	// 方案2: 测试解密后的密码
	encryptedPassword := os.Getenv("ENCRYPTED_PASSWORD")
	if encryptedPassword != "" {
		fmt.Println("\n=== 测试方案2: 解密密码连接 ===")
		fmt.Printf("加密的密码: %s\n", encryptedPassword)
		// TODO: 添加解密逻辑
		fmt.Println("需要添加密码解密逻辑")
	}
}

func testConnection(connStr string) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Printf("❌ 连接失败: %v\n", err)
		return
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Printf("❌ Ping失败: %v\n", err)
		return
	}

	fmt.Println("✅ 数据库连接成功")

	// 测试查询 schemas
	rows, err := db.Query(`
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY schema_name
	`)
	if err != nil {
		log.Printf("❌ 查询schemas失败: %v\n", err)
		return
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var schema string
		rows.Scan(&schema)
		schemas = append(schemas, schema)
	}

	fmt.Printf("✅ 找到 %d 个schemas: %v\n", len(schemas), schemas)

	// 对第一个schema测试查询表
	if len(schemas) > 0 {
		testSchema := schemas[0]
		tableRows, err := db.Query(`
			SELECT
				t.table_name,
				t.table_type
			FROM information_schema.tables t
			WHERE t.table_schema = $1
			ORDER BY t.table_name
		`, testSchema)
		if err != nil {
			log.Printf("❌ 查询表失败: %v\n", err)
			return
		}

		var tables []string
		for tableRows.Next() {
			var tableName, tableType string
			tableRows.Scan(&tableName, &tableType)
			tables = append(tables, fmt.Sprintf("%s(%s)", tableName, tableType))
		}
		tableRows.Close()

		fmt.Printf("✅ Schema '%s' 有 %d 张表: %v\n", testSchema, len(tables), tables)
	}
}
