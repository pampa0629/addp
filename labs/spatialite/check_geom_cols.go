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

	// 查询所有 geometry_columns 表的内容
	fmt.Println("查询 geometry_columns 表的所有内容:")
	rows, err := db.Query(`SELECT * FROM geometry_columns`)
	if err != nil {
		log.Fatal("查询失败:", err)
	}
	defer rows.Close()

	// 获取列名
	cols, _ := rows.Columns()
	fmt.Printf("列: %v\n\n", cols)

	// 显示所有记录
	count := 0
	for rows.Next() {
		count++
		// 创建一个切片来存储值
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		rows.Scan(valuePtrs...)

		fmt.Printf("记录 %d:\n", count)
		for i, col := range cols {
			fmt.Printf("  %s: %v\n", col, values[i])
		}
		fmt.Println()
	}

	if count == 0 {
		fmt.Println("geometry_columns 表为空!")
	}
}
