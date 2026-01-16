package main

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/mattn/go-sqlite3"
)

func init() {
	sql.Register("sqlite3_check",
		&sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				return conn.LoadExtension("/opt/homebrew/lib/mod_spatialite", "sqlite3_modspatialite_init")
			},
		})
}

func main() {
	dbPath := "/Users/pampa/Documents/data/bigdata/dltb1000w.sqlite"

	db, err := sql.Open("sqlite3_check", dbPath)
	if err != nil {
		log.Fatal("打开数据库失败:", err)
	}
	defer db.Close()

	// 查询表的创建语句
	fmt.Println("查询 DLTB 表的创建语句:")
	var createSQL string
	err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='DLTB'").Scan(&createSQL)
	if err != nil {
		log.Fatal("查询失败:", err)
	}
	fmt.Println(createSQL)

	// 查询几何列约束
	fmt.Println("\n查询几何列约束:")
	rows, err := db.Query(`
		SELECT type, name, sql
		FROM sqlite_master
		WHERE tbl_name = 'DLTB' AND (type = 'trigger' OR sql LIKE '%SmGeometry%')
	`)
	if err != nil {
		log.Fatal("查询失败:", err)
	}
	defer rows.Close()

	for rows.Next() {
		var typ, name, sql string
		rows.Scan(&typ, &name, &sql)
		fmt.Printf("\n类型: %s\n名称: %s\nSQL: %s\n", typ, name, sql)
	}
}
