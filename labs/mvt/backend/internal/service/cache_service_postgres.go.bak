package service

import (
    "bytes"
    "compress/gzip"
    "context"
    "database/sql"
    "fmt"
    "io"
    "log"
    "container/list"
    "time"

    "github.com/addp/mvt/internal/config"
    _ "github.com/jackc/pgx/v5/stdlib"
    "github.com/redis/go-redis/v9"
)

// CacheService 分层缓存：内存 LRU → Redis → Postgres
type CacheService struct {
    // in-memory LRU of gzipped tiles
    mem    *lruCache
    // Redis client (stores gzipped tiles)
    rdb    *redis.Client
    // Postgres connection for persistent cache
    pdb    *sql.DB
    // TTL for Redis
    ttl    time.Duration
}

const (
    defaultLRUSize          = 2048
    pgCacheTable            = "mvt_cache"
)

// 简易 LRU 实现（键为 string，值为 []byte）
type lruEntry struct {
    key   string
    value []byte
}

type lruCache struct {
    cap  int
    ll   *list.List
    dict map[string]*list.Element
}

func newLRU(capacity int) *lruCache {
    if capacity <= 0 { capacity = 1 }
    return &lruCache{cap: capacity, ll: list.New(), dict: make(map[string]*list.Element)}
}
func (c *lruCache) Get(key string) ([]byte, bool) {
    if ele, ok := c.dict[key]; ok {
        c.ll.MoveToFront(ele)
        return ele.Value.(lruEntry).value, true
    }
    return nil, false
}
func (c *lruCache) Add(key string, val []byte) {
    if ele, ok := c.dict[key]; ok {
        ele.Value = lruEntry{key: key, value: val}
        c.ll.MoveToFront(ele)
        return
    }
    ele := c.ll.PushFront(lruEntry{key: key, value: val})
    c.dict[key] = ele
    if c.ll.Len() > c.cap {
        last := c.ll.Back()
        if last != nil {
            kv := last.Value.(lruEntry)
            delete(c.dict, kv.key)
            c.ll.Remove(last)
        }
    }
}

// NewCacheService 创建缓存服务（含内存、Redis、PG，并确保表存在）
func NewCacheService(cfg *config.Config) (*CacheService, error) {
    // memory LRU
    mem := newLRU(defaultLRUSize)

    // Redis
    rdb := redis.NewClient(&redis.Options{
        Addr:     cfg.GetRedisAddr(),
        Password: cfg.Redis.Password,
        DB:       0,
    })

    // verify redis
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := rdb.Ping(ctx).Err(); err != nil {
        return nil, fmt.Errorf("failed to connect to redis: %w", err)
    }

    // Postgres for persistent cache (reuse same DSN)
    pdb, err := sql.Open("pgx", cfg.GetDSN())
    if err != nil {
        return nil, fmt.Errorf("failed to connect to database (cache): %w", err)
    }
    pdb.SetMaxOpenConns(10)
    pdb.SetMaxIdleConns(5)
    pdb.SetConnMaxLifetime(5 * time.Minute)
    if err := pdb.PingContext(ctx); err != nil {
        return nil, fmt.Errorf("failed to ping database (cache): %w", err)
    }

    svc := &CacheService{mem: mem, rdb: rdb, pdb: pdb, ttl: cfg.Redis.CacheTTL}
    if err := svc.ensurePGSchema(ctx); err != nil {
        return nil, err
    }
    return svc, nil
}

// ensurePGSchema 创建持久化缓存表（若不存在）
func (s *CacheService) ensurePGSchema(ctx context.Context) error {
    ddl := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    datasource TEXT NOT NULL,
    z SMALLINT NOT NULL,
    x INTEGER NOT NULL,
    y INTEGER NOT NULL,
    tile BYTEA NOT NULL,
    encoding TEXT NOT NULL DEFAULT 'gzip',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_accessed TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (datasource, z, x, y)
);
CREATE INDEX IF NOT EXISTS %s_last_accessed_idx ON %s (last_accessed);
`, pgCacheTable, pgCacheTable, pgCacheTable)
    if _, err := s.pdb.ExecContext(ctx, ddl); err != nil {
        return fmt.Errorf("failed to ensure cache table: %w", err)
    }
    return nil
}

// GetTile 从缓存获取（内存→Redis→PG），返回 gzip 压缩的 MVT
func (s *CacheService) GetTile(ctx context.Context, datasourceID string, z, x, y int) ([]byte, error) {
    key := s.buildKey(datasourceID, z, x, y)

    if v, ok := s.mem.Get(key); ok {
        return v, nil
    }

    // Redis
    data, err := s.rdb.Get(ctx, key).Bytes()
    if err == nil && len(data) > 0 {
    s.mem.Add(key, data)
        return data, nil
    }
    if err != nil && err != redis.Nil {
        return nil, fmt.Errorf("failed to get tile from redis: %w", err)
    }

    // Postgres persistent cache
    var gz []byte
    row := s.pdb.QueryRowContext(ctx, fmt.Sprintf("SELECT tile FROM %s WHERE datasource=$1 AND z=$2 AND x=$3 AND y=$4", pgCacheTable), datasourceID, z, x, y)
    if err := row.Scan(&gz); err != nil {
        if err == sql.ErrNoRows {
            return nil, nil
        }
        return nil, fmt.Errorf("failed to get tile from pg cache: %w", err)
    }

    // touch last_accessed (best-effort, async)
    go func() {
        _, _ = s.pdb.ExecContext(context.Background(), fmt.Sprintf("UPDATE %s SET last_accessed=now() WHERE datasource=$1 AND z=$2 AND x=$3 AND y=$4", pgCacheTable), datasourceID, z, x, y)
    }()

    // populate upper tiers
    s.mem.Add(key, gz)
    _ = s.rdb.Set(ctx, key, gz, s.ttl).Err()
    return gz, nil
}

// SetTileOptions 写入选项
type SetTileOptions struct {
    PersistToPG bool // 是否写入 PG 持久缓存
}

// SetTileWithOptions 将瓦片写入缓存（总是写内存与 Redis；可选 PG）
func (s *CacheService) SetTileWithOptions(ctx context.Context, datasourceID string, z, x, y int, gzipped []byte, opt SetTileOptions) error {
    key := s.buildKey(datasourceID, z, x, y)
    s.mem.Add(key, gzipped)
    if err := s.rdb.Set(ctx, key, gzipped, s.ttl).Err(); err != nil {
        return fmt.Errorf("failed to set tile to redis: %w", err)
    }
    if opt.PersistToPG {
        // upsert into PG
        _, err := s.pdb.ExecContext(ctx,
            fmt.Sprintf(`INSERT INTO %s (datasource, z, x, y, tile, encoding)
                         VALUES ($1,$2,$3,$4,$5,'gzip')
                         ON CONFLICT (datasource,z,x,y) DO UPDATE SET tile=EXCLUDED.tile, encoding='gzip', last_accessed=now()`, pgCacheTable),
            datasourceID, z, x, y, gzipped,
        )
        if err != nil {
            return fmt.Errorf("failed to upsert pg cache: %w", err)
        }
    }
    return nil
}

// SetTile 兼容旧接口：总是写内存与 Redis，并持久化到 PG
func (s *CacheService) SetTile(ctx context.Context, datasourceID string, z, x, y int, gzipped []byte) error {
    return s.SetTileWithOptions(ctx, datasourceID, z, x, y, gzipped, SetTileOptions{PersistToPG: true})
}

// Gzip 压缩帮助（输入原始 mvt，输出 gzip）
func (s *CacheService) Gzip(data []byte) ([]byte, error) {
    var buf bytes.Buffer
    gw := gzip.NewWriter(&buf)
    if _, err := gw.Write(data); err != nil {
        _ = gw.Close()
        return nil, err
    }
    if err := gw.Close(); err != nil {
        return nil, err
    }
    return io.ReadAll(bytes.NewReader(buf.Bytes()))
}

// ClearDataSource 清空指定数据源的所有缓存
func (s *CacheService) ClearDataSource(ctx context.Context, datasourceID string) error {
    // Redis
    pattern := fmt.Sprintf("mvt:%s:*", datasourceID)
    iter := s.rdb.Scan(ctx, 0, pattern, 0).Iterator()
    keys := []string{}
    for iter.Next(ctx) { keys = append(keys, iter.Val()) }
    if err := iter.Err(); err != nil { return fmt.Errorf("failed to scan keys: %w", err) }
    if len(keys) > 0 { if err := s.rdb.Del(ctx, keys...).Err(); err != nil { return fmt.Errorf("failed to delete keys: %w", err) } }

    // Memory
    // Best effort: purge matching keys by iterating keys snapshot (not exposed by lru). Easiest: recreate cache.
    s.mem = newLRU(defaultLRUSize)

    // PG
    if _, err := s.pdb.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE datasource=$1", pgCacheTable), datasourceID); err != nil {
        return fmt.Errorf("failed to clear pg cache: %w", err)
    }
    return nil
}

// ClearAll 清空所有缓存
func (s *CacheService) ClearAll(ctx context.Context) error {
    // Redis
    iter := s.rdb.Scan(ctx, 0, "mvt:*", 0).Iterator()
    keys := []string{}
    for iter.Next(ctx) { keys = append(keys, iter.Val()) }
    if err := iter.Err(); err != nil { return fmt.Errorf("failed to scan keys: %w", err) }
    if len(keys) > 0 { if err := s.rdb.Del(ctx, keys...).Err(); err != nil { return fmt.Errorf("failed to delete keys: %w", err) } }

    // Memory
    s.mem = newLRU(defaultLRUSize)

    // PG
    if _, err := s.pdb.ExecContext(ctx, fmt.Sprintf("TRUNCATE %s", pgCacheTable)); err != nil {
        return fmt.Errorf("failed to truncate pg cache: %w", err)
    }
    return nil
}

// GetStats 获取缓存统计信息
func (s *CacheService) GetStats(ctx context.Context) (map[string]interface{}, error) {
    info := s.rdb.Info(ctx, "stats", "memory")
    if info.Err() != nil {
        return nil, fmt.Errorf("failed to get redis info: %w", info.Err())
    }
    dbSize, err := s.rdb.DBSize(ctx).Result()
    if err != nil { return nil, fmt.Errorf("failed to get db size: %w", err) }

    // pg rows count (best-effort)
    var rows int64
    if err := s.pdb.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", pgCacheTable)).Scan(&rows); err != nil {
        log.Printf("[WARN] count pg cache failed: %v", err)
    }

    stats := map[string]interface{}{
        "redis_keys": dbSize,
        "redis_info": info.Val(),
        "pg_rows":    rows,
    }
    return stats, nil
}

// buildKey 构建缓存键（无查询参数/版本）
func (s *CacheService) buildKey(datasourceID string, z, x, y int) string {
    return fmt.Sprintf("mvt:%s:%d:%d:%d", datasourceID, z, x, y)
}

// Close 关闭连接
func (s *CacheService) Close() error {
    var err1, err2 error
    if s.rdb != nil { err1 = s.rdb.Close() }
    if s.pdb != nil { err2 = s.pdb.Close() }
    if err1 != nil { return err1 }
    return err2
}
