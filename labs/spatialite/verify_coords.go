package main

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/mattn/go-sqlite3"
)

func init() {
	sql.Register("sqlite3_verify",
		&sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				return conn.LoadExtension("/opt/homebrew/lib/mod_spatialite", "sqlite3_modspatialite_init")
			},
		})
}

func main() {
	dbPath := "/Users/pampa/Documents/data/bigdata/dltb1000w.sqlite"

	db, err := sql.Open("sqlite3_verify", dbPath)
	if err != nil {
		log.Fatal("打开数据库失败:", err)
	}
	defer db.Close()

	fmt.Println("验证：查询前3条记录的几何信息\n")

	// 查询几条记录的 SRID 和坐标范围
	rows, err := db.Query(`
		SELECT
			SmID,
			SRID(SmGeometry) as srid,
			ST_MinX(SmGeometry) as min_x,
			ST_MinY(SmGeometry) as min_y,
			ST_MaxX(SmGeometry) as max_x,
			ST_MaxY(SmGeometry) as max_y
		FROM DLTB
		LIMIT 3
	`)
	if err != nil {
		log.Fatal("查询失败:", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, srid int
		var minX, minY, maxX, maxY float64
		err := rows.Scan(&id, &srid, &minX, &minY, &maxX, &maxY)
		if err != nil {
			log.Fatal("读取失败:", err)
		}

		fmt.Printf("记录 ID: %d\n", id)
		fmt.Printf("  SRID: %d\n", srid)
		fmt.Printf("  X 范围: %.6f ~ %.6f\n", minX, maxX)
		fmt.Printf("  Y 范围: %.6f ~ %.6f\n", minY, maxY)

		// 判断坐标值的范围
		if minX > -180 && maxX < 180 && minY > -90 && maxY < 90 {
			fmt.Printf("  ✅ 坐标值在经纬度范围内 (4326坐标系的值)\n")
		} else {
			fmt.Printf("  ⚠️  坐标值超出经纬度范围\n")
		}
		fmt.Println()
	}

	fmt.Println("说明:")
	fmt.Println("- 如果坐标值仍在 -180~180, -90~90 范围内，说明坐标值未被修改")
	fmt.Println("- SRID 是 2360，但坐标值仍是 4326 的值（经纬度）")
	fmt.Println("- SetSRID 只改变了标签，没有进行坐标转换")
}
