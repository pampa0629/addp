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

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonModels "github.com/addp/common/models"
)

// OperatorDiscoveryService 跨模块算子发现服务
// 负责从各个模块（Meta、Transfer、Manager）和所有注册的工作流引擎获取算子列表并合并缓存
type OperatorDiscoveryService struct {
	systemClient       *commonClient.SystemClient // 新增：System 客户端（动态发现引擎）
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
	systemClient *commonClient.SystemClient, // 新增：System 客户端
	metaServiceURL string,
	transferServiceURL string,
	managerServiceURL string,
) *OperatorDiscoveryService {
	return &OperatorDiscoveryService{
		systemClient:       systemClient,
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

	// ✅ 第1步：从 System Backend 查询所有支持 workflow 的引擎
	workflowEngines, err := s.systemClient.ListWorkflowEngines(1) // tenantID=1 (默认租户)
	if err != nil {
		log.Printf("⚠️ [OperatorDiscovery] 查询工作流引擎失败: %v", err)
		// 即使失败也继续获取任务提供者的算子
		workflowEngines = []commonModels.Engine{}
	}

	log.Printf("✅ [OperatorDiscovery] 发现 %d 个工作流引擎", len(workflowEngines))

	// ✅ 第2步：并发获取所有引擎的算子
	totalModules := len(workflowEngines) + 3 // workflow 引擎 + Meta + Transfer + Manager
	var wg sync.WaitGroup
	results := make(chan []commonModels.OperatorMetadata, totalModules)
	errors := make(chan error, totalModules)

	// 遍历所有工作流引擎
	for _, engine := range workflowEngines {
		wg.Add(1)
		go func(eng commonModels.Engine) {
			defer wg.Done()

			operators, err := dbbridge.ListWorkflowOperators(ctx, &eng)
			if err != nil {
				log.Printf("⚠️ [OperatorDiscovery] 引擎 %s 获取失败: %v", eng.Name, err)
				errors <- err
			} else {
				log.Printf("✅ [OperatorDiscovery] 工作流引擎 %s 返回 %d 个算子", eng.Name, len(operators))
				results <- operators
			}
		}(engine)
	}

	// Meta Service（任务提供者）
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

	// Transfer Service（任务提供者）
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

	// Manager Service（任务提供者）
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

// GetOperatorsByEngineType 根据引擎类型过滤算子（动态查询 System Backend）
func (s *OperatorDiscoveryService) GetOperatorsByEngineType(ctx context.Context, engineType string) ([]commonModels.OperatorMetadata, error) {
	// 从 System Backend 查询指定类型的引擎
	engines, err := s.systemClient.ListEngines(engineType, 1) // tenantID=1
	if err != nil {
		return nil, fmt.Errorf("查询引擎失败: %w", err)
	}

	if len(engines) == 0 {
		return nil, fmt.Errorf("未找到类型为 %s 的引擎", engineType)
	}

	// 使用第一个激活的引擎
	var targetEngine *commonModels.Engine
	for _, eng := range engines {
		if eng.IsActive {
			targetEngine = &eng
			break
		}
	}

	if targetEngine == nil {
		return nil, fmt.Errorf("类型为 %s 的引擎均未激活", engineType)
	}

	operators, err := dbbridge.ListWorkflowOperators(ctx, targetEngine)
	if err != nil {
		return nil, fmt.Errorf("引擎 %s 获取算子失败: %w", targetEngine.Name, err)
	}

	return operators, nil
}

// GetOperatorsByModule 获取指定模块的算子（兼容旧接口，支持静态模块和动态引擎）
func (s *OperatorDiscoveryService) GetOperatorsByModule(ctx context.Context, module string) ([]commonModels.OperatorMetadata, error) {
	// 静态模块（任务提供者）
	staticModules := map[string]string{
		"meta":     s.metaServiceURL,
		"transfer": s.transferServiceURL,
		"manager":  s.managerServiceURL,
	}

	// 如果是静态模块，直接调用
	if url, ok := staticModules[module]; ok {
		return s.fetchOperatorsFromModule(ctx, module, url)
	}

	// 动态工作流引擎
	// 从缓存的算子列表中过滤
	allOperators, err := s.DiscoverAllOperators(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取算子列表失败: %w", err)
	}

	// 按 module 字段过滤
	var filteredOperators []commonModels.OperatorMetadata
	for _, op := range allOperators {
		if op.Module == module {
			filteredOperators = append(filteredOperators, op)
		}
	}

	if len(filteredOperators) == 0 {
		return nil, fmt.Errorf("未找到模块 %s 的算子", module)
	}

	return filteredOperators, nil
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
