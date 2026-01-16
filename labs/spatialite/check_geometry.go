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

	// 查询 geometry_columns 表
	fmt.Println("查询 SmGeometry 列的定义:")
	rows, err := db.Query(`
		SELECT f_table_name, f_geometry_column, geometry_type, coord_dimension, srid, spatial_index_enabled
		FROM geometry_columns
		WHERE f_table_name = 'DLTB'
	`)
	if err != nil {
		log.Fatal("查询 geometry_columns 失败:", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tableName, geomColumn string
		var geomType string
		var coordDim, srid, spatialIndex int
		err := rows.Scan(&tableName, &geomColumn, &geomType, &coordDim, &srid, &spatialIndex)
		if err != nil {
			log.Fatal("读取行失败:", err)
		}
		fmt.Printf("表名: %s\n", tableName)
		fmt.Printf("几何列: %s\n", geomColumn)
		fmt.Printf("几何类型: %s\n", geomType)
		fmt.Printf("坐标维度: %d\n", coordDim)
		fmt.Printf("当前SRID: %d\n", srid)
		fmt.Printf("空间索引: %d\n", spatialIndex)
	}

	// 查询第一条记录的 SRID
	fmt.Println("\n查询第一条记录的实际 SRID:")
	var srid int
	err = db.QueryRow("SELECT SRID(SmGeometry) FROM DLTB LIMIT 1").Scan(&srid)
	if err != nil {
		log.Fatal("查询 SRID 失败:", err)
	}
	fmt.Printf("实际 SRID: %d\n", srid)
}
