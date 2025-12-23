package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	commonModels "github.com/addp/common/models"
)

// OperatorDiscoveryService 跨模块算子发现服务
// 负责从各个模块（Meta、Transfer、Manager、GeoPandas、Spark）获取算子列表并合并缓存
type OperatorDiscoveryService struct {
	geopandasEngineURL string
	sparkEngineURL     string // 新增: Spark Sedona Engine URL
	metaServiceURL     string
	transferServiceURL string
	managerServiceURL  string
	httpClient         *http.Client

	// 缓存
	cachedOperators []commonModels.OperatorMetadata
	cacheTime       time.Time
	cacheTTL        time.Duration
	mu              sync.RWMutex
}

// NewOperatorDiscoveryService 创建算子发现服务
func NewOperatorDiscoveryService(
	geopandasEngineURL string,
	sparkEngineURL string, // 新增: Spark Engine URL
	metaServiceURL string,
	transferServiceURL string,
	managerServiceURL string,
) *OperatorDiscoveryService {
	return &OperatorDiscoveryService{
		geopandasEngineURL: geopandasEngineURL,
		sparkEngineURL:     sparkEngineURL,
		metaServiceURL:     metaServiceURL,
		transferServiceURL: transferServiceURL,
		managerServiceURL:  managerServiceURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cacheTTL: 5 * time.Minute, // 5分钟缓存
	}
}

// DiscoverAllOperators 发现所有模块的算子
func (s *OperatorDiscoveryService) DiscoverAllOperators(ctx context.Context) ([]commonModels.OperatorMetadata, error) {
	// 检查缓存
	s.mu.RLock()
	if time.Since(s.cacheTime) < s.cacheTTL && len(s.cachedOperators) > 0 {
		log.Printf("✅ [OperatorDiscovery] 使用缓存的算子列表 (count=%d)", len(s.cachedOperators))
		operators := s.cachedOperators
		s.mu.RUnlock()
		return operators, nil
	}
	s.mu.RUnlock()

	log.Printf("🔍 [OperatorDiscovery] 开始发现所有模块的算子...")

	// 并发获取各模块的算子
	var wg sync.WaitGroup
	results := make(chan []commonModels.OperatorMetadata, 5) // 增加到5个模块
	errors := make(chan error, 5)

	// GeoPandas Engine
	wg.Add(1)
	go func() {
		defer wg.Done()
		operators, err := s.fetchOperatorsFromModule(ctx, "geopandas", s.geopandasEngineURL)
		if err != nil {
			log.Printf("⚠️ [OperatorDiscovery] GeoPandas Engine 获取失败: %v", err)
			errors <- err
		} else {
			results <- operators
		}
	}()

	// Spark Sedona Engine (新增)
	wg.Add(1)
	go func() {
		defer wg.Done()
		operators, err := s.fetchOperatorsFromModule(ctx, "spark", s.sparkEngineURL)
		if err != nil {
			log.Printf("⚠️ [OperatorDiscovery] Spark Sedona Engine 获取失败: %v", err)
			errors <- err
		} else {
			results <- operators
		}
	}()

	// Meta Service
	wg.Add(1)
	go func() {
		defer wg.Done()
		operators, err := s.fetchOperatorsFromModule(ctx, "meta", s.metaServiceURL)
		if err != nil {
			log.Printf("⚠️ [OperatorDiscovery] Meta Service 获取失败: %v", err)
			errors <- err
		} else {
			results <- operators
		}
	}()

	// Transfer Service
	wg.Add(1)
	go func() {
		defer wg.Done()
		operators, err := s.fetchOperatorsFromModule(ctx, "transfer", s.transferServiceURL)
		if err != nil {
			log.Printf("⚠️ [OperatorDiscovery] Transfer Service 获取失败: %v", err)
			errors <- err
		} else {
			results <- operators
		}
	}()

	// Manager Service
	wg.Add(1)
	go func() {
		defer wg.Done()
		operators, err := s.fetchOperatorsFromModule(ctx, "manager", s.managerServiceURL)
		if err != nil {
			log.Printf("⚠️ [OperatorDiscovery] Manager Service 获取失败: %v", err)
			errors <- err
		} else {
			results <- operators
		}
	}()

	// 等待所有请求完成
	wg.Wait()
	close(results)
	close(errors)

	// 合并所有算子
	var allOperators []commonModels.OperatorMetadata
	for operators := range results {
		allOperators = append(allOperators, operators...)
	}

	// 记录错误（但不中断）
	errorCount := 0
	for err := range errors {
		if err != nil {
			errorCount++
		}
	}

	if errorCount > 0 {
		log.Printf("⚠️ [OperatorDiscovery] %d 个模块获取失败，但其他模块成功", errorCount)
	}

	// 更新缓存
	s.mu.Lock()
	s.cachedOperators = allOperators
	s.cacheTime = time.Now()
	s.mu.Unlock()

	log.Printf("✅ [OperatorDiscovery] 发现算子完成 (total=%d)", len(allOperators))
	return allOperators, nil
}

// GetOperatorsByModule 获取指定模块的算子
func (s *OperatorDiscoveryService) GetOperatorsByModule(ctx context.Context, module string) ([]commonModels.OperatorMetadata, error) {
	var url string
	switch module {
	case "geopandas":
		url = s.geopandasEngineURL
	case "spark": // 新增
		url = s.sparkEngineURL
	case "meta":
		url = s.metaServiceURL
	case "transfer":
		url = s.transferServiceURL
	case "manager":
		url = s.managerServiceURL
	default:
		return nil, fmt.Errorf("未知的模块: %s", module)
	}

	return s.fetchOperatorsFromModule(ctx, module, url)
}

// GetOperatorsByEngineType 根据引擎类型过滤算子
// 支持的引擎类型: geopandas, spark_sedona
func (s *OperatorDiscoveryService) GetOperatorsByEngineType(ctx context.Context, engineType string) ([]commonModels.OperatorMetadata, error) {
	// 映射引擎类型到模块
	moduleMapping := map[string]string{
		"api.geopandas":    "geopandas",
		"api.spark_sedona": "spark",
	}

	module, ok := moduleMapping[engineType]
	if !ok {
		return nil, fmt.Errorf("不支持的引擎类型: %s", engineType)
	}

	// 获取指定模块的算子
	return s.GetOperatorsByModule(ctx, module)
}

// GetOperatorDetail 获取算子详情（从缓存中查找）
func (s *OperatorDiscoveryService) GetOperatorDetail(ctx context.Context, operatorName string) (*commonModels.OperatorMetadata, error) {
	// 先获取所有算子（会使用缓存）
	operators, err := s.DiscoverAllOperators(ctx)
	if err != nil {
		return nil, err
	}

	// 查找指定算子
	for _, op := range operators {
		if op.Name == operatorName {
			return &op, nil
		}
	}

	return nil, fmt.Errorf("算子不存在: %s", operatorName)
}

// fetchOperatorsFromModule 从指定模块获取算子列表
func (s *OperatorDiscoveryService) fetchOperatorsFromModule(ctx context.Context, module string, baseURL string) ([]commonModels.OperatorMetadata, error) {
	// 构建URL
	url := fmt.Sprintf("%s/api/operators", baseURL)

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 执行请求
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch operators: %w", err)
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		// 如果是404或503，说明模块不支持算子API，返回空列表而不是错误
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusServiceUnavailable {
			log.Printf("ℹ️ [OperatorDiscovery] %s 模块不支持算子API", module)
			return []commonModels.OperatorMetadata{}, nil
		}
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 解析响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response commonModels.OperatorsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	log.Printf("✅ [OperatorDiscovery] %s 模块返回 %d 个算子", module, len(response.Operators))
	return response.Operators, nil
}

// RefreshCache 强制刷新缓存
func (s *OperatorDiscoveryService) RefreshCache(ctx context.Context) error {
	s.mu.Lock()
	s.cacheTime = time.Time{} // 清空缓存时间
	s.mu.Unlock()

	_, err := s.DiscoverAllOperators(ctx)
	return err
}

// GetCacheInfo 获取缓存信息
func (s *OperatorDiscoveryService) GetCacheInfo() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"cached_count": len(s.cachedOperators),
		"cache_time":   s.cacheTime,
		"cache_age":    time.Since(s.cacheTime).Seconds(),
		"cache_ttl":    s.cacheTTL.Seconds(),
		"is_valid":     time.Since(s.cacheTime) < s.cacheTTL,
	}
}
