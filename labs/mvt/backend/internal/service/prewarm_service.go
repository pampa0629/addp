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
// 预热阶段：所有瓦片强制持久化到 PG（无大小/时间限制）
// 使用独立的主连接池，避免影响浏览请求性能
func StartPrewarm(cfg *appcfg.Config, tiles *TileService, cache *CacheService) {
    maxZoom := cfg.Prewarm.MaxZoom
    if maxZoom < 0 { maxZoom = 0 }
    conc := cfg.Prewarm.Concurrency
    if conc <= 0 { conc = 1 }

    datasources := tiles.ListDataSources()
    if len(datasources) == 0 { return }

    log.Printf("[PREWARM] Start prewarming %d datasources, z=0..%d, concurrency=%d (using dedicated connection pool)", len(datasources), maxZoom, conc)

    // worker pool
    type job struct{ dsID string; z, x, y int }
    jobs := make(chan job, 1024)
    var wg sync.WaitGroup

    // 速率限制器：每个 worker 在处理完一个任务后稍作休息，避免过度占用资源
    rateLimitDelay := 10 * time.Millisecond  // 每个瓦片之间暂停 10ms

    // workers
    for i := 0; i < conc; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := range jobs {
                // 预热阶段不设置生成超时限制（允许复杂查询完成）
                ctx := context.Background()
                start := time.Now()
                raw, err := tiles.GenerateTilePrewarm(ctx, j.dsID, j.z, j.x, j.y)  // 使用预热专用方法（主连接池）
                if err != nil {
                    log.Printf("[PREWARM] Generate failed for %s z=%d x=%d y=%d: %v", j.dsID, j.z, j.x, j.y, err)
                    continue
                }
                if len(raw) == 0 { continue }

                // 预热阶段：强制持久化所有瓦片到 PG（无大小/时间限制）
                gz, err := cache.Gzip(raw)
                if err != nil {
                    log.Printf("[PREWARM] Gzip failed for %s z=%d x=%d y=%d: %v", j.dsID, j.z, j.x, j.y, err)
                    continue
                }

                // 写缓存：不设置超时（确保持久化成功）
                cctx := context.Background()
                if err := cache.SetTileWithOptions(cctx, j.dsID, j.z, j.x, j.y, gz, SetTileOptions{PersistToPG: true}); err != nil {
                    log.Printf("[PREWARM] Cache failed for %s z=%d x=%d y=%d: %v", j.dsID, j.z, j.x, j.y, err)
                } else {
                    dur := time.Since(start)
                    log.Printf("[PREWARM] Cached %s z=%d x=%d y=%d (size=%d bytes, took=%s)", j.dsID, j.z, j.x, j.y, len(gz), dur)
                }

                // 速率限制：暂停一小段时间，避免过度占用资源
                time.Sleep(rateLimitDelay)
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
