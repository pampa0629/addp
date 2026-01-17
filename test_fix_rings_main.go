package main

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/addp/common/spatial"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// 连接SpatiaLite数据库，启用扩展加载
	db, err := sql.Open("sqlite3", "/Users/pampa/Documents/data/yanshi.sqlite?_load_extension=1")
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	// 加载SpatiaLite扩展
	_, err = db.Exec("SELECT load_extension('/opt/homebrew/lib/mod_spatialite.dylib', 'sqlite3_modspatialite_init')")
	if err != nil {
		log.Fatalf("加载SpatiaLite扩展失败: %v", err)
	}

	fmt.Println(string(make([]rune, 100)))
	for i := 0; i < 100; i++ {
		fmt.Print("=")
	}
	fmt.Println()
	fmt.Println("测试FixInvalidRings函数修复3点环")
	for i := 0; i < 100; i++ {
		fmt.Print("=")
	}
	fmt.Println()

	// 查询问题记录
	query := `
		SELECT ROWID, SmID, ST_AsBinary(SmGeometry) as wkb
		FROM '全市规划用地_1'
		WHERE ROWID IN (3683, 3684)
		ORDER BY ROWID
	`

	rows, err := db.Query(query)
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}
	defer rows.Close()

	testCount := 0
	successCount := 0

	for rows.Next() {
		var rowid, smid int64
		var wkb []byte

		if err := rows.Scan(&rowid, &smid, &wkb); err != nil {
			log.Printf("扫描失败: %v", err)
			continue
		}

		testCount++

		fmt.Printf("\n测试记录 %d/2:\n", testCount)
		fmt.Printf("  ROWID: %d, SmID: %d\n", rowid, smid)
		fmt.Printf("  原始WKB长度: %d bytes\n", len(wkb))

		// 1. 先转换为标准WKB
		standardWKB, err := spatial.ConvertToStandardWKB(wkb)
		if err != nil {
			log.Printf("  ❌ 转换为标准WKB失败: %v", err)
			continue
		}
		fmt.Printf("  转换后WKB长度: %d bytes\n", len(standardWKB))

		// 2. 修复无效的3点环
		fixedWKB, err := spatial.FixInvalidRings(standardWKB)
		if err != nil {
			log.Printf("  ❌ 修复环失败: %v", err)
			continue
		}

		fmt.Printf("  修复后WKB长度: %d bytes\n", len(fixedWKB))
		fmt.Printf("  长度变化: %+d bytes\n", len(fixedWKB)-len(wkb))

		if len(fixedWKB) > len(standardWKB) {
			successCount++
			fmt.Printf("  ✅ 修复成功！WKB已增大，说明添加了闭合点\n")
		} else {
			fmt.Printf("  ⚠️  WKB长度未变化，可能没有需要修复的环\n")
		}
	}

	fmt.Println()
	for i := 0; i < 100; i++ {
		fmt.Print("=")
	}
	fmt.Println()
	fmt.Printf("测试完成: 共 %d 条记录，成功修复 %d 条\n", testCount, successCount)
	for i := 0; i < 100; i++ {
		fmt.Print("=")
	}
	fmt.Println()

	if successCount == testCount && testCount > 0 {
		fmt.Println("✅ 所有测试记录的无效环都已成功修复！")
	} else {
		fmt.Println("⚠️  部分记录未能修复，请检查日志")
	}
}
