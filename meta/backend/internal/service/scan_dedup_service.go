package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

// ScanDedupService 扫描去重服务
type ScanDedupService struct {
	redis *redis.Client
}

// NewScanDedupService 创建扫描去重服务
func NewScanDedupService(redis *redis.Client) *ScanDedupService {
	return &ScanDedupService{redis: redis}
}

// GenerateExecutionLockKey 生成扫描执行范围锁。
// 锁粒度跟扫描 scope 对齐，避免同一 engine 下 item refresh、ref group 回扫和局部 catalog scan 互相误挡。
func (s *ScanDedupService) GenerateExecutionLockKey(tenantID, engineID uint, itemID uint, catalogPaths []string, refGroups []models.ScanRefGroup) string {
	mode := "engine"
	keyParts := []string{fmt.Sprintf("tenant:%d", tenantID), fmt.Sprintf("engine:%d", engineID)}
	if itemID > 0 {
		mode = "item"
		keyParts = append(keyParts, fmt.Sprintf("item:%d", itemID))
	} else if normalizedRefs := canonicalRefGroups(refGroups); len(normalizedRefs) > 0 {
		mode = "ref_groups"
		keyParts = append(keyParts, "refs:"+hashJSON(normalizedRefs))
	} else if paths := canonicalCatalogPaths(catalogPaths); len(paths) > 0 {
		mode = "catalog_paths"
		keyParts = append(keyParts, "paths:"+hashJSON(paths))
	}
	return fmt.Sprintf("meta:scan:lock:execution:%s:%s", mode, strings.Join(keyParts, ":"))
}

// TryAcquireLock 原子获取扫描锁。返回 false 表示锁已被其他执行持有。
func (s *ScanDedupService) TryAcquireLock(ctx context.Context, lockKey string, ttl time.Duration) (bool, error) {
	return s.redis.SetNX(ctx, lockKey, time.Now().Unix(), ttl).Result()
}

// TryAcquireOwnedLock 原子获取带 owner 的扫描锁。owner 通常使用 execution_id。
func (s *ScanDedupService) TryAcquireOwnedLock(ctx context.Context, lockKey string, owner string, ttl time.Duration) (bool, error) {
	return s.redis.SetNX(ctx, lockKey, owner, ttl).Result()
}

// ReleaseOwnedLock 只释放 owner 匹配的扫描锁，避免旧执行误删新执行的锁。
func (s *ScanDedupService) ReleaseOwnedLock(ctx context.Context, lockKey string, owner string) (bool, error) {
	const script = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`
	deleted, err := s.redis.Eval(ctx, script, []string{lockKey}, owner).Int()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

// ClearTask 清除任务标记
func (s *ScanDedupService) ClearTask(ctx context.Context, taskKey string) error {
	return s.redis.Del(ctx, taskKey).Err()
}

// GetLastScanTime 获取上次扫描时间
func (s *ScanDedupService) GetLastScanTime(ctx context.Context, engineID uint) (*time.Time, error) {
	key := fmt.Sprintf("meta:cache:scan_last_time:%d", engineID)
	val, err := s.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	timestamp, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return nil, err
	}
	return &timestamp, nil
}

// UpdateLastScanTime 更新上次扫描时间
func (s *ScanDedupService) UpdateLastScanTime(ctx context.Context, engineID uint) error {
	key := fmt.Sprintf("meta:cache:scan_last_time:%d", engineID)
	return s.redis.Set(ctx, key, time.Now().Format(time.RFC3339), 0).Err()
}

// GenerateNamespaceLockKey 生成命名空间级锁key
// 格式: meta:scan:lock:tenant:{tenant_id}:engine:{engine_id}:namespace:{namespace_name}
func (s *ScanDedupService) GenerateNamespaceLockKey(tenantID, engineID uint, namespaceName string) string {
	return fmt.Sprintf("meta:scan:lock:tenant:%d:engine:%d:namespace:%s",
		tenantID, engineID, namespaceName)
}

// GenerateBranchLockKey 生成 catalog branch 级锁 key。
// 格式: meta:scan:lock:tenant:{tenant_id}:engine:{engine_id}:branch:{branch_name}
func (s *ScanDedupService) GenerateBranchLockKey(tenantID, engineID uint, branchName string) string {
	return fmt.Sprintf("meta:scan:lock:tenant:%d:engine:%d:branch:%s",
		tenantID, engineID, branchName)
}

// GenerateBucketLockKey 生成Bucket级锁key
// 格式: meta:scan:lock:tenant:{tenant_id}:engine:{engine_id}:bucket:{bucket_path}
// 路径中的特殊字符会被清理（/ 和 : 替换为 _）
func (s *ScanDedupService) GenerateBucketLockKey(tenantID, engineID uint, bucketPath string) string {
	cleanPath := strings.NewReplacer("/", "_", ":", "_").Replace(bucketPath)
	return fmt.Sprintf("meta:scan:lock:tenant:%d:engine:%d:bucket:%s",
		tenantID, engineID, cleanPath)
}

func canonicalCatalogPaths(paths []string) []string {
	normalized := scanflow.UniqueNonEmpty(paths)
	sort.Strings(normalized)
	return normalized
}

type canonicalRefGroup struct {
	Primary string             `json:"primary"`
	Refs    []canonicalScanRef `json:"refs"`
}

type canonicalScanRef struct {
	Path     string `json:"path"`
	Role     string `json:"role"`
	Required bool   `json:"required"`
}

func canonicalRefGroups(groups []models.ScanRefGroup) []canonicalRefGroup {
	normalized := scanflow.NormalizeRefGroups(groups)
	canonical := make([]canonicalRefGroup, 0, len(normalized))
	for _, group := range normalized {
		refs := scanflow.NormalizedScanRefs(group)
		cRefs := make([]canonicalScanRef, 0, len(refs))
		for _, ref := range refs {
			cRefs = append(cRefs, canonicalScanRef{
				Path:     strings.TrimSpace(ref.Path),
				Role:     strings.TrimSpace(ref.Role),
				Required: ref.Required,
			})
		}
		sort.Slice(cRefs, func(i, j int) bool {
			if cRefs[i].Path != cRefs[j].Path {
				return cRefs[i].Path < cRefs[j].Path
			}
			if cRefs[i].Role != cRefs[j].Role {
				return cRefs[i].Role < cRefs[j].Role
			}
			return !cRefs[i].Required && cRefs[j].Required
		})
		canonical = append(canonical, canonicalRefGroup{
			Primary: strings.TrimSpace(group.Primary),
			Refs:    cRefs,
		})
	}
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].Primary != canonical[j].Primary {
			return canonical[i].Primary < canonical[j].Primary
		}
		left := hashJSON(canonical[i].Refs)
		right := hashJSON(canonical[j].Refs)
		return left < right
	})
	return canonical
}

func hashJSON(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte(fmt.Sprintf("%#v", value))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}
