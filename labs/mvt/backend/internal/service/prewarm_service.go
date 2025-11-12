package service

import (
    "context"
    "log"
    "math"
    "sync"
    "time"

    appcfg "github.com/addp/mvt/internal/config"
)

// StartPrewarm 在服务启动后异步预热低缩放级别瓦片
// 遍历各数据源的空间范围，在 [0, MaxZoom] 内按范围计算 x/y 范围并生成瓦片。
// 是否持久化到 PG 由配置阈值（耗时/大小）决定。
func StartPrewarm(cfg *appcfg.Config, tiles *TileService, cache *CacheService) {
    maxZoom := cfg.Prewarm.MaxZoom
    if maxZoom < 0 { maxZoom = 0 }
    conc := cfg.Prewarm.Concurrency
    if conc <= 0 { conc = 1 }

    datasources := tiles.ListDataSources()
    if len(datasources) == 0 { return }

    log.Printf("[PREWARM] Start prewarming %d datasources, z=0..%d, concurrency=%d", len(datasources), maxZoom, conc)

    // worker pool
    type job struct{ dsID string; z, x, y int }
    jobs := make(chan job, 1024)
    var wg sync.WaitGroup

    // workers
    for i := 0; i < conc; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := range jobs {
                genTO := cfg.Prewarm.GenerateTimeout
                if genTO <= 0 { genTO = 200 * time.Second }
                ctx, cancel := context.WithTimeout(context.Background(), genTO)
                start := time.Now()
                raw, err := tiles.GenerateTile(ctx, j.dsID, j.z, j.x, j.y)
                cancel()
                if err != nil { continue }
                if len(raw) == 0 { continue }

                // 阈值规则（按原始 MVT 大小判断）
                minDur := cfg.CachePolicy.PersistMinDuration
                if minDur <= 0 { minDur = 3 * time.Second }
                minKB := cfg.CachePolicy.PersistMinRawKB
                if minKB <= 0 { minKB = 100 }
                persist := time.Since(start) >= minDur || len(raw) >= minKB*1024
                // 压缩
                gz, err := cache.Gzip(raw)
                if err != nil { continue }

                // 写缓存
                cacheTO := cfg.Prewarm.CacheTimeout
                if cacheTO <= 0 { cacheTO = 20 * time.Second }
                cctx, ccancel := context.WithTimeout(context.Background(), cacheTO)
                _ = cache.SetTileWithOptions(cctx, j.dsID, j.z, j.x, j.y, gz, SetTileOptions{PersistToPG: persist})
                ccancel()
            }
        }()
    }

    // 枚举任务
    for _, ds := range datasources {
        // 获取范围（WGS84）
        extent, err := tiles.GetDataSourceExtent(ds.ID)
        if err != nil { continue }
        minLng, minLat, maxLng, maxLat := extent[0], extent[1], extent[2], extent[3]
        // clamp lat
        minLat = math.Max(minLat, -85.05112878)
        maxLat = math.Min(maxLat, 85.05112878)

        for z := 0; z <= maxZoom; z++ {
            minX, minY := lonLatToTileXY(minLng, maxLat, z) // 注意：Y 轴向下
            maxX, maxY := lonLatToTileXY(maxLng, minLat, z)
            if minX > maxX { minX, maxX = maxX, minX }
            if minY > maxY { minY, maxY = maxY, minY }

            // 保护：限制单层最大任务量（避免极端范围过大）
            // 大致上限定 500k 个任务
            if int64(maxX-minX+1)*int64(maxY-minY+1) > 500_000 {
                log.Printf("[PREWARM] Skip z=%d for %s due to huge range", z, ds.ID)
                continue
            }

            for x := minX; x <= maxX; x++ {
                for y := minY; y <= maxY; y++ {
                    select {
                    case jobs <- job{dsID: ds.ID, z: z, x: x, y: y}:
                    default:
                        jobs <- job{dsID: ds.ID, z: z, x: x, y: y}
                    }
                }
            }
        }
    }
    close(jobs)
    wg.Wait()
    log.Printf("[PREWARM] Done")
}

func lonLatToTileXY(lon, lat float64, z int) (int, int) {
    lat = math.Max(math.Min(lat, 85.05112878), -85.05112878)
    n := math.Exp2(float64(z))
    x := int(math.Floor((lon + 180.0) / 360.0 * n))
    rad := lat * math.Pi / 180.0
    y := int(math.Floor((1.0 - math.Log(math.Tan(rad)+1.0/math.Cos(rad))/math.Pi) / 2.0 * n))
    if x < 0 { x = 0 }
    if y < 0 { y = 0 }
    return x, y
}
