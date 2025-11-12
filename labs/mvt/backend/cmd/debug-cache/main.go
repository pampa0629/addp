package main

import (
    "database/sql"
    "fmt"
    "log"

    "github.com/addp/mvt/internal/config"
    _ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
    cfg, err := config.Load()
    if err != nil { log.Fatalf("load config: %v", err) }
    db, err := sql.Open("pgx", cfg.GetDSN())
    if err != nil { log.Fatalf("open db: %v", err) }
    defer db.Close()

    rows, err := db.Query(`SELECT datasource,z,x,y,octet_length(tile) AS bytes FROM mvt_cache WHERE z IN (6,7) ORDER BY z,x,y LIMIT 20`)
    if err != nil { log.Fatalf("query: %v", err) }
    defer rows.Close()
    for rows.Next() {
        var ds string
        var z, x, y int
        var bytes int
        if err := rows.Scan(&ds, &z, &x, &y, &bytes); err != nil { log.Fatalf("scan: %v", err) }
        fmt.Printf("%s %d/%d/%d bytes=%d\n", ds, z, x, y, bytes)
    }
    if err := rows.Err(); err != nil { log.Fatalf("rows: %v", err) }
}

