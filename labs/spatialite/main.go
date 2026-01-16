package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/mattn/go-sqlite3"
)

func init() {
	// 注册一个允许扩展加载的SQLite驱动
	sql.Register("sqlite3_with_extensions",
		&sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				// 在连接时加载 SpatiaLite 扩展
				return conn.LoadExtension("/opt/homebrew/lib/mod_spatialite", "sqlite3_modspatialite_init")
			},
		})
}

func main() {
	dbPath := "/Users/pampa/Documents/data/bigdata/dltb1000w.sqlite"

	fmt.Println("正在打开数据库:", dbPath)

	// 使用支持扩展的驱动打开数据库
	db, err := sql.Open("sqlite3_with_extensions", dbPath)
	if err != nil {
		log.Fatal("打开数据库失败:", err)
	}
	defer db.Close()

	fmt.Println("SpatiaLite 扩展已在连接时自动加载")

	// 初始化 SpatiaLite 环境（加载空间函数）
	fmt.Println("正在初始化 SpatiaLite 环境...")
	_, err = db.Exec("SELECT InitSpatialMetadata(1)")
	if err != nil {
		// 如果已经初始化过，会报错，但可以忽略
		fmt.Printf("InitSpatialMetadata 提示: %v (可能已初始化，继续执行)\n", err)
	}

	// 测试 SpatiaLite 函数是否可用
	var version string
	err = db.QueryRow("SELECT spatialite_version()").Scan(&version)
	if err != nil {
		log.Fatal("SpatiaLite 函数不可用:", err)
	}
	fmt.Printf("SpatiaLite 版本: %s\n", version)

	// 检查表的记录数
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM DLTB").Scan(&count)
	if err != nil {
		log.Fatal("查询记录数失败:", err)
	}
	fmt.Printf("DLTB 表共有 %d 条记录\n", count)

	// 步骤1: 更新 geometry_columns 表中的 SRID 定义
	fmt.Println("\n步骤1: 更新 geometry_columns 表中的 SRID 定义...")
	updateGeomColSQL := `UPDATE geometry_columns SET srid = 2360 WHERE f_table_name = 'dltb' AND f_geometry_column = 'smgeometry'`
	result, err := db.Exec(updateGeomColSQL)
	if err != nil {
		log.Fatal("更新 geometry_columns 失败:", err)
	}
	rowsAffected, _ := result.RowsAffected()
	fmt.Printf("geometry_columns 更新成功，影响行数: %d\n", rowsAffected)

	// 步骤2: 执行 UPDATE 语句更新实际的几何数据
	// SpatiaLite使用SetSRID而不是ST_SetSRID
	updateSQL := `UPDATE DLTB SET SmGeometry = SetSRID(SmGeometry, 2360)`
	fmt.Println("\n步骤2: 正在执行 SQL:", updateSQL)

	startTime := time.Now()
	result, err = db.Exec(updateSQL)
	if err != nil {
		log.Fatal("执行 UPDATE 失败:", err)
	}
	elapsed := time.Since(startTime)

	// 获取影响的行数
	rowsAffected, err = result.RowsAffected()
	if err != nil {
		log.Fatal("获取影响行数失败:", err)
	}

	fmt.Printf("\n执行成功！\n")
	fmt.Printf("影响行数: %d\n", rowsAffected)
	fmt.Printf("执行耗时: %s\n", elapsed)

	// 验证更新结果 - 检查 SRID
	var srid int
	err = db.QueryRow("SELECT SRID(SmGeometry) FROM DLTB LIMIT 1").Scan(&srid)
	if err != nil {
		log.Printf("验证 SRID 失败: %v\n", err)
	} else {
		fmt.Printf("验证: SRID 已更新为 %d\n", srid)
	}
}
