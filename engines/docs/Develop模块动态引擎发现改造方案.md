# Develop 模块动态引擎发现改造方案

**目标**: 将 Develop 模块的算子发现服务从硬编码引擎 URL 改为通过 System Backend 动态查询。

**当前状态**: ❌ 硬编码（`operator_discovery_service.go` 第 19-23 行）**目标状态**: ✅ 动态发现（查询 System Backend 获取所有支持 `workflow` 的引擎）

---

## 📋 改造清单

### 1. 添加 System Client 依赖

**文件**: `develop/backend/internal/service/operator_discovery_service.go`

**当前代码** (第 16-31 行):
```go
type OperatorDiscoveryService struct {
	pythonWorkflowEngineURL string  // ❌ 硬编码
	sparkWorkflowEngineURL  string  // ❌ 硬编码
	metaServiceURL          string
	transferServiceURL      string
	managerServiceURL       string
	httpClient              *http.Client

	// 缓存
	cachedOperators []commonModels.OperatorMetadata
	cacheTime       time.Time
	cacheTTL        time.Duration
	mu              sync.RWMutex
}
```

**修改为**:
```go
import (
	"github.com/addp/common/client"  // 新增导入
	// ...
)

type OperatorDiscoveryService struct {
	systemClient       *client.SystemClient  // ✅ 新增：System 客户端
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
```

### 2. 修改构造函数

**当前代码** (第 33-52 行):
```go
func NewOperatorDiscoveryService(
	pythonWorkflowEngineURL string,  // ❌ 移除
	sparkWorkflowEngineURL string,   // ❌ 移除
	metaServiceURL string,
	transferServiceURL string,
	managerServiceURL string,
) *OperatorDiscoveryService {
	return &OperatorDiscoveryService{
		pythonWorkflowEngineURL: pythonWorkflowEngineURL,
		sparkWorkflowEngineURL:  sparkWorkflowEngineURL,
		// ...
	}
}
```

**修改为**:
```go
func NewOperatorDiscoveryService(
	systemServiceURL string,       // ✅ 新增：System Backend URL
	metaServiceURL string,
	transferServiceURL string,
	managerServiceURL string,
) *OperatorDiscoveryService {
	return &OperatorDiscoveryService{
		systemClient: client.NewSystemClient(systemServiceURL),  // ✅ 创建 System 客户端
		metaServiceURL:     metaServiceURL,
		transferServiceURL: transferServiceURL,
		managerServiceURL:  managerServiceURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cacheTTL: 5 * time.Minute,
	}
}
```

### 3. 修改 DiscoverAllOperators 方法

**当前代码** (第 54-169 行):
```go
func (s *OperatorDiscoveryService) DiscoverAllOperators(ctx context.Context) ([]commonModels.OperatorMetadata, error) {
	// ... 缓存检查

	// 并发获取各模块的算子
	var wg sync.WaitGroup
	results := make(chan []commonModels.OperatorMetadata, 5)
	errors := make(chan error, 5)

	// ❌ 硬编码调用 Python Workflow Engine
	wg.Add(1)
	go func() {
		defer wg.Done()
		operators, err := s.fetchOperatorsFromModule(ctx, "python", s.pythonWorkflowEngineURL)
		// ...
	}()

	// ❌ 硬编码调用 Spark 工作流引擎
	wg.Add(1)
	go func() {
		defer wg.Done()
		operators, err := s.fetchOperatorsFromModule(ctx, "spark", s.sparkWorkflowEngineURL)
		// ...
	}()

	// ... Meta、Transfer、Manager
}
```

**修改为**:
```go
func (s *OperatorDiscoveryService) DiscoverAllOperators(ctx context.Context) ([]commonModels.OperatorMetadata, error) {
	// ... 缓存检查

	log.Printf("🔍 [OperatorDiscovery] 开始发现所有模块的算子...")

	// ✅ 第1步：从 System Backend 查询所有支持 workflow 的引擎
	workflowEngines, err := s.systemClient.ListWorkflowEngines(ctx)
	if err != nil {
		log.Printf("⚠️ [OperatorDiscovery] 查询工作流引擎失败: %v", err)
		// 即使失败也继续获取任务提供者的算子
		workflowEngines = []commonModels.Engine{}
	}

	log.Printf("✅ [OperatorDiscovery] 发现 %d 个工作流引擎", len(workflowEngines))

	// ✅ 第2步：并发获取所有引擎的算子
	totalModules := len(workflowEngines) + 3  // workflow 引擎 + Meta + Transfer + Manager
	var wg sync.WaitGroup
	results := make(chan []commonModels.OperatorMetadata, totalModules)
	errors := make(chan error, totalModules)

	// 遍历所有工作流引擎
	for _, engine := range workflowEngines {
		wg.Add(1)
		go func(eng commonModels.Engine) {
			defer wg.Done()

			// 从引擎的 connection_config 中提取 API URL
			baseURL, ok := eng.ConnectionConfig["base_url"].(string)
			if !ok {
				log.Printf("⚠️ [OperatorDiscovery] 引擎 %s 缺少 base_url 配置", eng.Name)
				errors <- fmt.Errorf("引擎 %s 缺少 base_url", eng.Name)
				return
			}

			operators, err := s.fetchOperatorsFromModule(ctx, eng.Name, baseURL)
			if err != nil {
				log.Printf("⚠️ [OperatorDiscovery] 引擎 %s 获取失败: %v", eng.Name, err)
				errors <- err
			} else {
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
```

### 4. 移除或重构 GetOperatorsByModule 方法

**当前代码** (第 171-190 行):
```go
func (s *OperatorDiscoveryService) GetOperatorsByModule(ctx context.Context, module string) ([]commonModels.OperatorMetadata, error) {
	var url string
	switch module {
	case "python":   // ❌ 硬编码
		url = s.pythonWorkflowEngineURL
	case "spark":    // ❌ 硬编码
		url = s.sparkWorkflowEngineURL
	// ...
	}
}
```

**修改为**:
```go
// GetOperatorsByEngine 根据引擎类型获取算子
func (s *OperatorDiscoveryService) GetOperatorsByEngine(ctx context.Context, engineType string) ([]commonModels.OperatorMetadata, error) {
	// 查询指定引擎
	engine, err := s.systemClient.GetEngine(ctx, engineType)
	if err != nil {
		return nil, fmt.Errorf("查询引擎失败: %w", err)
	}

	baseURL, ok := engine.ConnectionConfig["base_url"].(string)
	if !ok {
		return nil, fmt.Errorf("引擎 %s 缺少 base_url 配置", engineType)
	}

	return s.fetchOperatorsFromModule(ctx, engine.Name, baseURL)
}
```

### 5. 更新 main.go 初始化代码

**文件**: `develop/backend/cmd/server/main.go`

**当前代码**:
```go
operatorDiscoveryService := service.NewOperatorDiscoveryService(
	os.Getenv("PYTHON_WORKFLOW_ENGINE_URL"),  // ❌ 移除
	os.Getenv("SPARK_WORKFLOW_ENGINE_URL"),   // ❌ 移除
	os.Getenv("META_SERVICE_URL"),
	os.Getenv("TRANSFER_SERVICE_URL"),
	os.Getenv("MANAGER_SERVICE_URL"),
)
```

**修改为**:
```go
operatorDiscoveryService := service.NewOperatorDiscoveryService(
	os.Getenv("SYSTEM_SERVICE_URL"),  // ✅ 新增
	os.Getenv("META_SERVICE_URL"),
	os.Getenv("TRANSFER_SERVICE_URL"),
	os.Getenv("MANAGER_SERVICE_URL"),
)
```

### 6. 更新环境变量配置

**文件**: `develop/backend/.env`

**移除**:
```bash
PYTHON_WORKFLOW_ENGINE_URL=http://python-workflow-engine:8090  # ❌ 移除
SPARK_WORKFLOW_ENGINE_URL=http://spark-workflow-engine:8098    # ❌ 移除
```

**添加**:
```bash
SYSTEM_SERVICE_URL=http://system-backend:8080  # ✅ 新增
```

---

## 🧪 测试计划

### 测试 1: Math Workflow 引擎自动发现

1. 启动 Math Workflow 引擎（自动注册到 System）
2. 启动 Develop Backend
3. 调用 `GET /api/develop/operators`
4. **预期**: 返回的算子列表中包含 Math Workflow 的 5 个算子

### 测试 2: 多引擎并存

1. 启动 Python Workflow、Spark Workflow、Math Workflow 三个引擎
2. 调用 `GET /api/develop/operators`
3. **预期**: 返回约 70 个算子（Python 42 + Spark 21 + Math 5 + Meta/Transfer/Manager）

### 测试 3: 引擎动态上下线

1. 初始状态：仅 Python Workflow 运行
2. 启动 Math Workflow
3. 等待缓存过期（5 分钟）或调用刷新接口
4. **预期**: Math Workflow 的算子自动出现在列表中

---

## 📚 相关文件

- **需要修改的文件**:
  - `develop/backend/internal/service/operator_discovery_service.go` - 主要改造文件
  - `develop/backend/cmd/server/main.go` - 初始化代码
  - `develop/backend/.env` - 环境变量配置

- **依赖的 Common 模块**:
  - `common/client/system.go` - System Client（可能需要添加 `ListWorkflowEngines` 方法）
  - `common/models/engine.go` - Engine 模型
  - `common/utils/capability_filter.go` - 能力过滤函数

---

## ✅ 验收标准

- [ ] Develop Backend 不再硬编码引擎 URL
- [ ] 可以动态发现所有注册的工作流引擎
- [ ] Math Workflow 引擎的 5 个算子自动出现在算子面板
- [ ] Python Workflow 和 Spark Workflow 的算子正常显示
- [ ] 新注册的引擎可以被自动发现（5 分钟缓存后）
- [ ] 所有现有功能正常工作（工作流执行、算子测试等）

---

**版本**: v1.0
**创建时间**: 2025-12-31
**状态**: 待实施
