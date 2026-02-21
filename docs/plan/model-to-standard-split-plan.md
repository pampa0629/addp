# Model 模块拆分为 Standard 和 Model 实施计划

## Context（背景和目标）

### 为什么要拆分

当前 `model` 模块包含两类职责不同的功能：

1. **数据标准管理**：定义企业级数据规范
   - Domain（业务域分类体系）
   - Glossary（业务术语词典）
   - Element（数据元标准，含质量规则）
   - CodeSet（码值集/枚举值标准）

2. **数据建模设计**：应用标准进行数据库设计
   - Entity（业务实体 E-R 建模）
   - LogicalTable（逻辑表设计）
   - DWLayer（数仓分层定义）

这种混合导致模块职责不清晰，且不符合数据治理的分层思想。

### 拆分目标

**创建 Standard 模块**（新模块）：
- 定位：企业级**数据标准规范层**
- 包含：Domain、Glossary、Element、CodeSet
- 端口：后端 8110 / 前端 5181
- 数据库：`standard` schema

**调整 Model 模块**（保留）：
- 定位：**数据建模应用层**
- 包含：Entity、LogicalTable、DWLayer
- 端口：后端 8181 / 前端 5181（保持不变）
- 数据库：`model` schema

### 架构收益

1. ✅ **职责清晰**：Standard 制定标准，Model 应用标准
2. ✅ **复用性好**：Standard 可被多个模块引用（Model、Quality、Meta）
3. ✅ **符合最佳实践**：参考 DAMA DMBOK 和业界产品（DataWorks、WeData）的模块划分
4. ✅ **降低复杂度**：每个模块聚焦单一职责，易于维护和扩展

---

## 关键设计决策

### 1. 跨模块引用处理

**问题**：LogicalField 和 EntityAttribute 需要引用 Element

**解决方案**：
- 删除数据库层面的跨 schema 外键约束
- Model 模块通过 `common/client/standard.go` 调用 Standard API 验证 element_id
- 前端通过 Gateway 调用 `/api/standard/elements` 获取数据元列表

### 2. 迁移策略

- **数据库**：移动 6 个表从 `model` schema 到 `standard` schema
- **代码**：复制 model 代码框架，删除不需要的部分
- **无需考虑向后兼容**：直接修改表结构和 API 路径（符合 ADDP 开发原则）

---

## 实施步骤

### 阶段 1：数据库迁移（1 小时）

#### 1.1 备份现有数据

```bash
# 停止所有服务
bash scripts/dev/stop.sh

# 备份 model schema
pg_dump -h localhost -p 15432 -U addp -d addp -n model -F c \
  -f /tmp/model_schema_backup_$(date +%Y%m%d_%H%M%S).dump
```

#### 1.2 执行迁移脚本

创建 `scripts/migrations/split_model_to_standard.sql`：

```sql
-- Model 模块拆分迁移脚本
BEGIN;

-- 创建 standard schema
CREATE SCHEMA IF NOT EXISTS standard;
COMMENT ON SCHEMA standard IS 'Standard 模块：数据标准管理';

-- 移动 6 个表到 standard schema
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = 'model') THEN
        -- 移动表
        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = 'model' AND table_name = 'domains') THEN
            ALTER TABLE model.domains SET SCHEMA standard;
            RAISE NOTICE '✅ 已移动 domains';
        END IF;

        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = 'model' AND table_name = 'glossaries') THEN
            ALTER TABLE model.glossaries SET SCHEMA standard;
            RAISE NOTICE '✅ 已移动 glossaries';
        END IF;

        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = 'model' AND table_name = 'glossary_element_mappings') THEN
            ALTER TABLE model.glossary_element_mappings SET SCHEMA standard;
            RAISE NOTICE '✅ 已移动 glossary_element_mappings';
        END IF;

        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = 'model' AND table_name = 'elements') THEN
            ALTER TABLE model.elements SET SCHEMA standard;
            RAISE NOTICE '✅ 已移动 elements';
        END IF;

        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = 'model' AND table_name = 'code_sets') THEN
            ALTER TABLE model.code_sets SET SCHEMA standard;
            RAISE NOTICE '✅ 已移动 code_sets';
        END IF;

        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = 'model' AND table_name = 'code_items') THEN
            ALTER TABLE model.code_items SET SCHEMA standard;
            RAISE NOTICE '✅ 已移动 code_items';
        END IF;

        -- 删除跨 schema 外键约束
        ALTER TABLE model.logical_fields DROP CONSTRAINT IF EXISTS fk_logical_fields_element;
        ALTER TABLE model.entity_attributes DROP CONSTRAINT IF EXISTS fk_entity_attributes_element;
        RAISE NOTICE '✅ 已删除跨 schema 外键约束';
    END IF;
END $$;

COMMIT;
```

执行：

```bash
psql -h localhost -p 15432 -U addp -d addp -f scripts/migrations/split_model_to_standard.sql
```

#### 1.3 验证迁移结果

```bash
# 检查 standard schema 的表
psql -h localhost -p 15432 -U addp -d addp -c "\dt standard.*"

# 应该看到 6 个表：
# - domains
# - glossaries
# - glossary_element_mappings
# - elements
# - code_sets
# - code_items

# 检查数据完整性
psql -h localhost -p 15432 -U addp -d addp -c "SELECT COUNT(*) FROM standard.elements;"
```

---

### 阶段 2：创建 Standard 模块后端（3 小时）

#### 2.1 创建目录结构

```bash
mkdir -p standard/backend/{cmd/server,internal/{api,config,models,repository,service}}
mkdir -p standard/docs
```

#### 2.2 从 model 复制代码框架

```bash
# 复制整个后端目录
cp -r model/backend/* standard/backend/

# 初始化 go.mod
cd standard/backend
go mod init github.com/addp/standard
go mod edit -replace github.com/addp/common=../../common
```

#### 2.3 修改关键文件

**文件 1**: `standard/backend/internal/config/config.go`

```go
package config

import (
	"fmt"
	"os"
	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig
	Port          string
	DBSchema      string
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int
	SystemURL     string
	InternalAPIKey string
}

func LoadConfig() (*Config, error) {
	commonConfig.LoadEnv()
	cfg := &Config{
		Port:          commonConfig.GetEnv("STANDARD_BACKEND_PORT", "8110"),
		DBSchema:      "standard",
		RedisHost:     commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:     commonConfig.GetEnv("REDIS_PORT", "16379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       commonConfig.GetEnvInt("REDIS_DB", 0),
		SystemURL:     commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		InternalAPIKey: os.Getenv("INTERNAL_API_KEY"),
	}

	enableIntegration := commonConfig.GetEnvBool("ENABLE_SERVICE_INTEGRATION", true)
	if err := commonConfig.LoadServiceConfiguration(commonConfig.ServiceConfigLoader{
		SystemServiceURL:      cfg.SystemURL,
		EnableIntegration:     enableIntegration,
		InternalAPIKey:        cfg.InternalAPIKey,
		BaseConfigDestination: &cfg.BaseConfig,
	}); err != nil {
		return nil, fmt.Errorf("failed to load service configuration: %w", err)
	}

	cfg.BaseConfig.SystemServiceURL = cfg.SystemURL
	cfg.BaseConfig.EnableIntegration = enableIntegration
	return cfg, nil
}

func (c *Config) GetDatabaseDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSchema,
	)
}
```

**文件 2**: `standard/backend/cmd/server/main.go`

```go
package main

import (
	"fmt"
	"log"
	"time"
	commonClient "github.com/addp/common/client"
	"github.com/addp/standard/internal/api"
	"github.com/addp/standard/internal/config"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// 确保 standard schema 存在
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", cfg.DBSchema)).Error; err != nil {
		log.Fatalf("Failed to create schema: %v", err)
	}

	// 自动迁移（6个表）
	if err := db.AutoMigrate(
		&models.Domain{},
		&models.Glossary{},
		&models.GlossaryElementMapping{},
		&models.Element{},
		&models.CodeSet{},
		&models.CodeItem{},
	); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
	}

	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemURL, cfg.InternalAPIKey)

	// 创建 Repositories
	domainRepo := repository.NewDomainRepository(db)
	glossaryRepo := repository.NewGlossaryRepository(db)
	elementRepo := repository.NewElementRepository(db)
	codeSetRepo := repository.NewCodeSetRepository(db)

	// 创建 Services
	domainSvc := service.NewDomainService(domainRepo)
	glossarySvc := service.NewGlossaryService(glossaryRepo)
	elementSvc := service.NewElementService(elementRepo, codeSetRepo)
	codeSetSvc := service.NewCodeSetService(codeSetRepo)

	router := api.SetupRouter(domainSvc, glossarySvc, elementSvc, codeSetSvc, cfg.SystemURL, redisClient)

	addr := ":" + cfg.Port
	log.Printf("Standard service starting on %s", addr)

	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 模块注册和心跳
	go func() {
		time.Sleep(2 * time.Second)
		serviceURL := fmt.Sprintf("http://localhost:%s", cfg.Port)
		registrationReq := &commonClient.ModuleRegistrationRequest{
			ModuleName:     "standard",
			ModuleURL:      serviceURL,
			RoutePrefix:    "/standard",
			HealthCheckURL: serviceURL + "/health",
			Metadata:       map[string]interface{}{"module": "standard"},
		}

		maxRetries := 3
		for attempt := 1; attempt <= maxRetries; attempt++ {
			if err := systemClient.RegisterModule(registrationReq); err != nil {
				log.Printf("⚠️  Standard 模块注册失败 (尝试 %d/%d): %v", attempt, maxRetries, err)
				time.Sleep(time.Duration(attempt*5) * time.Second)
				continue
			}
			log.Printf("✅ Standard 模块注册成功: %s", serviceURL)
			break
		}

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := systemClient.SendHeartbeat("standard"); err != nil {
				log.Printf("⚠️  Standard 心跳失败: %v", err)
			}
		}
	}()

	select {}
}
```

#### 2.4 删除不需要的文件

```bash
cd standard/backend/internal

# 删除 Model 相关的文件（保留 Standard 相关的 4 类）
rm -f models/entity.go
rm -f models/logical_table.go
rm -f models/dw_layer.go

rm -f api/entity_handler.go
rm -f api/logical_table_handler.go
rm -f api/dw_layer_handler.go

rm -f service/entity_service.go
rm -f service/logical_table_service.go
rm -f service/dw_layer_service.go

rm -f repository/entity_repository.go
rm -f repository/logical_table_repository.go
rm -f repository/dw_layer_repository.go
```

#### 2.5 修改所有模型的 TableName

在 `standard/backend/internal/models/` 下的所有文件中修改 `TableName()` 方法：

```go
// domain.go
func (Domain) TableName() string {
	return "standard.domains"
}

// glossary.go
func (Glossary) TableName() string {
	return "standard.glossaries"
}
func (GlossaryElementMapping) TableName() string {
	return "standard.glossary_element_mappings"
}

// element.go
func (Element) TableName() string {
	return "standard.elements"
}

// code_set.go
func (CodeSet) TableName() string {
	return "standard.code_sets"
}
func (CodeItem) TableName() string {
	return "standard.code_items"
}
```

#### 2.6 修改 API 路由

编辑 `standard/backend/internal/api/router.go`，将路由前缀改为 `/api/standard`：

```go
api := router.Group("/api/standard")
api.Use(authMiddleware.TenantMiddleware())
{
	// Domain routes
	domains := api.Group("/domains")
	{
		domains.GET("", domainHandler.List)
		domains.POST("", domainHandler.Create)
		domains.GET("/:id", domainHandler.Get)
		domains.PUT("/:id", domainHandler.Update)
		domains.DELETE("/:id", domainHandler.Delete)
	}

	// Glossary routes
	glossaries := api.Group("/glossaries")
	// ...

	// Element routes
	elements := api.Group("/elements")
	// ...

	// CodeSet routes
	codeSets := api.Group("/code-sets")
	// ...
}
```

#### 2.7 编译验证

```bash
cd standard/backend
go mod tidy
go build -o ../../.dev-bins/addp-standard cmd/server/main.go
```

---

### 阶段 3：创建 Standard 模块前端（2 小时）

#### 3.1 复制代码框架

```bash
# 复制整个前端目录
cp -r model/frontend/* standard/frontend/
cd standard/frontend
```

#### 3.2 修改 package.json

```json
{
  "name": "standard-frontend",
  "version": "0.1.0",
  "scripts": {
    "dev": "vite --port 5181",
    "build": "vite build",
    "preview": "vite preview"
  }
}
```

#### 3.3 修改 vite.config.js

```js
export default defineConfig({
  server: {
    port: 5181,
    proxy: {
      '/api': {
        target: 'http://localhost:8000',
        changeOrigin: true,
      },
    },
  },
})
```

#### 3.4 删除不需要的页面

```bash
cd standard/frontend/src/views
rm -f EntityList.vue EntityDetail.vue
rm -f LogicalTableList.vue LogicalTableDetail.vue
rm -f DWLayerList.vue
```

保留的页面（8 个）：
- Login.vue
- DomainList.vue
- GlossaryList.vue
- ElementList.vue
- ElementDetail.vue
- CodeSetList.vue
- CodeSetDetail.vue

#### 3.5 修改 API 路径

创建 `standard/frontend/src/api/standard.js`：

```js
import client from './client'

// Domain API
export const getDomains = (params) => client.get('/api/standard/domains', { params })
export const createDomain = (data) => client.post('/api/standard/domains', data)
export const getDomain = (id) => client.get(`/api/standard/domains/${id}`)
export const updateDomain = (id, data) => client.put(`/api/standard/domains/${id}`, data)
export const deleteDomain = (id) => client.delete(`/api/standard/domains/${id}`)

// Glossary API
export const getGlossaries = (params) => client.get('/api/standard/glossaries', { params })
export const createGlossary = (data) => client.post('/api/standard/glossaries', data)
export const getGlossary = (id) => client.get(`/api/standard/glossaries/${id}`)
export const updateGlossary = (id, data) => client.put(`/api/standard/glossaries/${id}`, data)
export const deleteGlossary = (id) => client.delete(`/api/standard/glossaries/${id}`)
export const approveGlossary = (id) => client.post(`/api/standard/glossaries/${id}/approve`)
export const deprecateGlossary = (id) => client.post(`/api/standard/glossaries/${id}/deprecate`)

// Element API
export const getElements = (params) => client.get('/api/standard/elements', { params })
export const createElement = (data) => client.post('/api/standard/elements', data)
export const getElement = (id) => client.get(`/api/standard/elements/${id}`)
export const updateElement = (id, data) => client.put(`/api/standard/elements/${id}`, data)
export const deleteElement = (id) => client.delete(`/api/standard/elements/${id}`)
export const approveElement = (id) => client.post(`/api/standard/elements/${id}/approve`)
export const getQualityRules = (id) => client.get(`/api/standard/elements/${id}/quality-rules`)

// CodeSet API
export const getCodeSets = (params) => client.get('/api/standard/code-sets', { params })
export const createCodeSet = (data) => client.post('/api/standard/code-sets', data)
export const getCodeSet = (id) => client.get(`/api/standard/code-sets/${id}`)
export const updateCodeSet = (id, data) => client.put(`/api/standard/code-sets/${id}`, data)
export const deleteCodeSet = (id) => client.delete(`/api/standard/code-sets/${id}`)

export const getCodeItems = (codeSetId) => client.get(`/api/standard/code-sets/${codeSetId}/items`)
export const createCodeItem = (codeSetId, data) => client.post(`/api/standard/code-sets/${codeSetId}/items`, data)
export const updateCodeItem = (codeSetId, itemId, data) => client.put(`/api/standard/code-sets/${codeSetId}/items/${itemId}`, data)
export const deleteCodeItem = (codeSetId, itemId) => client.delete(`/api/standard/code-sets/${codeSetId}/items/${itemId}`)
```

#### 3.6 修改路由

编辑 `standard/frontend/src/router/index.js`，删除 Entity、LogicalTable、DWLayer 的路由。

#### 3.7 安装依赖和构建

```bash
cd standard/frontend
npm install
npm run build
```

---

### 阶段 4：调整 Model 模块（3 小时）

#### 4.1 删除移动到 Standard 的代码

```bash
cd model/backend/internal

# 删除 Standard 相关的文件
rm -f models/domain.go
rm -f models/glossary.go
rm -f models/element.go
rm -f models/code_set.go

rm -f api/domain_handler.go
rm -f api/glossary_handler.go
rm -f api/element_handler.go
rm -f api/code_set_handler.go

rm -f service/domain_service.go
rm -f service/glossary_service.go
rm -f service/element_service.go
rm -f service/code_set_service.go

rm -f repository/domain_repository.go
rm -f repository/glossary_repository.go
rm -f repository/element_repository.go
rm -f repository/code_set_repository.go
```

#### 4.2 创建 StandardClient

创建 `common/client/standard.go`：

```go
package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type StandardClient struct {
	baseURL     string
	httpClient  *http.Client
	authToken   string
	internalKey string
}

func NewStandardClient(baseURL string) *StandardClient {
	return &StandardClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func NewStandardClientWithInternalKey(baseURL, internalKey string) *StandardClient {
	return &StandardClient{
		baseURL:     baseURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		internalKey: internalKey,
	}
}

func (c *StandardClient) addAuth(req *http.Request) {
	if c.internalKey != "" {
		req.Header.Set("X-Internal-API-Key", c.internalKey)
	} else if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
}

type ElementResponse struct {
	ID           int64                  `json:"id"`
	TenantID     int64                  `json:"tenant_id"`
	Name         string                 `json:"name"`
	Code         string                 `json:"code"`
	DataType     string                 `json:"data_type"`
	CodeSetID    *int64                 `json:"code_set_id"`
	QualityRules map[string]interface{} `json:"quality_rules"`
}

// ValidateElement 验证数据元是否存在（用于跨模块引用验证）
func (c *StandardClient) ValidateElement(elementID int64, tenantID int64) error {
	url := fmt.Sprintf("%s/api/standard/elements/%d", c.baseURL, elementID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("element_id %d not found", elementID)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("standard api returned status %d: %s", resp.StatusCode, string(body))
	}

	var element ElementResponse
	if err := json.NewDecoder(resp.Body).Decode(&element); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if element.TenantID != tenantID {
		return fmt.Errorf("element_id %d belongs to tenant %d, not %d", elementID, element.TenantID, tenantID)
	}

	return nil
}

// GetElement 获取数据元详情
func (c *StandardClient) GetElement(elementID int64, tenantID int64) (*ElementResponse, error) {
	url := fmt.Sprintf("%s/api/standard/elements/%d", c.baseURL, elementID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("standard api returned status %d: %s", resp.StatusCode, string(body))
	}

	var element ElementResponse
	if err := json.NewDecoder(resp.Body).Decode(&element); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &element, nil
}
```

#### 4.3 修改 Model Service 添加验证

编辑 `model/backend/internal/service/logical_table_service.go`：

```go
package service

import (
	"fmt"
	commonClient "github.com/addp/common/client"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

type LogicalTableService struct {
	repo           *repository.LogicalTableRepository
	standardClient *commonClient.StandardClient
}

func NewLogicalTableService(repo *repository.LogicalTableRepository, standardURL, internalKey string) *LogicalTableService {
	return &LogicalTableService{
		repo:           repo,
		standardClient: commonClient.NewStandardClientWithInternalKey(standardURL, internalKey),
	}
}

// CreateField 创建逻辑表字段（验证 element_id）
func (s *LogicalTableService) CreateField(tableID int64, req *models.CreateLogicalFieldRequest, tenantID int64) (*models.LogicalField, error) {
	// 验证 element_id（如果提供）
	if req.ElementID != nil {
		if err := s.standardClient.ValidateElement(*req.ElementID, tenantID); err != nil {
			return nil, fmt.Errorf("invalid element_id: %w", err)
		}
	}

	field := &models.LogicalField{
		TableID:      tableID,
		ElementID:    req.ElementID,
		Name:         req.Name,
		ColumnName:   req.ColumnName,
		DataType:     req.DataType,
		Length:       req.Length,
		Precision:    req.Precision,
		Scale:        req.Scale,
		Nullable:     req.Nullable,
		IsPrimaryKey: req.IsPrimaryKey,
		IsPartition:  req.IsPartition,
		DefaultValue: req.DefaultValue,
		Description:  req.Description,
		SortOrder:    req.SortOrder,
	}

	if err := s.repo.CreateField(field); err != nil {
		return nil, err
	}
	return field, nil
}

// UpdateField 更新逻辑表字段（验证 element_id）
func (s *LogicalTableService) UpdateField(fieldID int64, req *models.UpdateLogicalFieldRequest, tenantID int64) (*models.LogicalField, error) {
	// 验证 element_id（如果提供）
	if req.ElementID != nil {
		if err := s.standardClient.ValidateElement(*req.ElementID, tenantID); err != nil {
			return nil, fmt.Errorf("invalid element_id: %w", err)
		}
	}

	field, err := s.repo.GetFieldByID(fieldID)
	if err != nil {
		return nil, err
	}

	// 更新字段
	if req.ElementID != nil {
		field.ElementID = req.ElementID
	}
	if req.Name != "" {
		field.Name = req.Name
	}
	// ... 其他字段更新

	if err := s.repo.UpdateField(field); err != nil {
		return nil, err
	}
	return field, nil
}
```

类似地修改 `model/backend/internal/service/entity_service.go`。

#### 4.4 修改 main.go 初始化 Service

编辑 `model/backend/cmd/server/main.go`：

```go
// 获取 Standard 服务 URL
standardURL := commonConfig.GetEnv("STANDARD_URL", "http://localhost:8110")
internalKey := os.Getenv("INTERNAL_API_KEY")

// 创建 Services（传入 standardURL）
entitySvc := service.NewEntityService(entityRepo, standardURL, internalKey)
logicalTableSvc := service.NewLogicalTableService(logicalTableRepo, standardURL, internalKey)
dwLayerSvc := service.NewDWLayerService(dwLayerRepo)
```

#### 4.5 修改前端删除 Standard 相关页面

```bash
cd model/frontend/src/views
rm -f DomainList.vue
rm -f GlossaryList.vue
rm -f ElementList.vue ElementDetail.vue
rm -f CodeSetList.vue CodeSetDetail.vue
```

#### 4.6 修改前端调用 Standard API

在需要数据元列表的页面（如 `LogicalTableDetail.vue`），调用 Standard API：

```vue
<script setup>
import { ref, onMounted } from 'vue'
import client from '@/api/client'

const elements = ref([])

const loadElements = async () => {
  try {
    const response = await client.get('/api/standard/elements', {
      params: { tenant_id: getTenantId() }
    })
    elements.value = response.data
  } catch (error) {
    console.error('Failed to load elements:', error)
  }
}

onMounted(() => {
  loadElements()
})
</script>

<template>
  <el-select v-model="field.element_id" placeholder="选择数据元（可选）">
    <el-option
      v-for="element in elements"
      :key="element.id"
      :value="element.id"
      :label="`${element.name} (${element.code})`"
    />
  </el-select>
</template>
```

---

### 阶段 5：配置和脚本集成（2 小时）

#### 5.1 更新 .env 配置

在项目根目录的 `.env` 文件中添加：

```bash
# Standard 模块配置
STANDARD_BACKEND_PORT=8110
STANDARD_FRONTEND_PORT=5181
STANDARD_URL=http://localhost:8110
```

#### 5.2 修改 docker-compose.yml

添加 Standard 模块的服务定义：

```yaml
services:
  # Standard 后端
  standard-backend:
    build:
      context: ./standard/backend
      dockerfile: Dockerfile
    container_name: addp-standard-backend
    ports:
      - "${STANDARD_BACKEND_PORT:-8110}:8110"
    env_file:
      - .env
    depends_on:
      - postgres
      - redis
    networks:
      - addp-network

  # Standard 前端
  standard-frontend:
    build:
      context: ./standard/frontend
      dockerfile: Dockerfile
    container_name: addp-standard-frontend
    ports:
      - "${STANDARD_FRONTEND_PORT:-5181}:80"
    depends_on:
      - standard-backend
    networks:
      - addp-network
```

#### 5.3 修改开发脚本

编辑 `scripts/dev/start.sh`，添加 Standard 模块支持：

```bash
# 在 MODULE_CONFIG 中添加
MODULE_CONFIG["standard"]="standard,8110,5181"

# 在 start_backend 函数中添加
if [ "$module_name" = "standard" ]; then
    cd "$PROJECT_ROOT/standard/backend"
    go build -o "$PROJECT_ROOT/.dev-bins/addp-standard" cmd/server/main.go
    nohup "$PROJECT_ROOT/.dev-bins/addp-standard" > "$PROJECT_ROOT/logs/standard-backend.log" 2>&1 &
    echo $! > "$PROJECT_ROOT/.dev-pids/standard-backend.pid"
fi

# 在 start_frontend 函数中添加
if [ "$module_name" = "standard" ]; then
    cd "$PROJECT_ROOT/standard/frontend"
    npm run dev > "$PROJECT_ROOT/logs/standard-frontend.log" 2>&1 &
    echo $! > "$PROJECT_ROOT/.dev-pids/standard-frontend.pid"
fi
```

编辑 `scripts/dev/restart.sh`，添加 Standard 模块支持：

```bash
# 在帮助文本中添加
echo "  -standard    重启 Standard 模块"

# 在参数处理中添加
-standard)
    MODULES+=("standard")
    ;;
```

#### 5.4 修改 Gateway 路由

编辑 `gateway/internal/router/router.go`，添加 Standard 模块路由：

```go
// Standard 模块路由
standardPrefix := os.Getenv("STANDARD_ROUTE_PREFIX")
if standardPrefix == "" {
	standardPrefix = "/standard"
}
standardBackend := os.Getenv("STANDARD_BACKEND_URL")
if standardBackend == "" {
	standardBackend = "http://localhost:8110"
}

standardGroup := apiGroup.Group(standardPrefix)
{
	standardGroup.Any("/*path", func(c *gin.Context) {
		proxy(c, standardBackend, standardPrefix)
	})
}
```

#### 5.5 修改 Portal 配置

编辑 `portal/frontend/src/config/modules.js`：

```js
export const modules = [
  {
    name: 'system',
    title: '系统管理',
    icon: 'Setting',
    url: 'http://localhost:5173',
    description: '用户、租户、引擎管理',
  },
  {
    name: 'standard',
    title: '数据标准',
    icon: 'DataLine',
    url: 'http://localhost:5181',
    description: '业务域、术语、数据元、码值集',
  },
  {
    name: 'model',
    title: '数据建模',
    icon: 'DataAnalysis',
    url: 'http://localhost:5181',
    description: '业务实体、逻辑表、数仓分层',
  },
  // ... 其他模块
]
```

---

### 阶段 6：测试验证（3 小时）

#### 6.1 单模块测试

**测试 Standard 模块**：

```bash
# 启动 Standard 模块
bash scripts/dev/start.sh -standard

# 检查健康状态
curl http://localhost:8110/health

# 测试 API（需要 token）
TOKEN="your_jwt_token"
TENANT_ID=1

# 测试业务域
curl -H "Authorization: Bearer $TOKEN" \
     -H "X-Tenant-ID: $TENANT_ID" \
     http://localhost:8000/api/standard/domains

# 测试数据元
curl -H "Authorization: Bearer $TOKEN" \
     -H "X-Tenant-ID: $TENANT_ID" \
     http://localhost:8000/api/standard/elements
```

**测试 Model 模块**：

```bash
# 启动 Model 模块
bash scripts/dev/start.sh -model

# 检查健康状态
curl http://localhost:8181/health

# 测试 API
curl -H "Authorization: Bearer $TOKEN" \
     -H "X-Tenant-ID: $TENANT_ID" \
     http://localhost:8000/api/model/entities
```

#### 6.2 跨模块集成测试

**测试场景：创建引用数据元的逻辑表字段**

```bash
# 1. 创建数据元（在 Standard 模块）
curl -X POST http://localhost:8000/api/standard/elements \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "code": "user_name",
    "name": "用户名称",
    "data_type": "string",
    "length": 50
  }'

# 记录返回的 element_id，假设为 123

# 2. 创建逻辑表（在 Model 模块）
curl -X POST http://localhost:8000/api/model/logical-tables \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "code": "t_user",
    "name": "用户表",
    "table_type": "entity"
  }'

# 记录返回的 table_id，假设为 456

# 3. 创建字段并引用数据元（测试跨模块验证）
curl -X POST http://localhost:8000/api/model/logical-tables/456/fields \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "element_id": 123,
    "name": "用户名",
    "column_name": "user_name",
    "data_type": "VARCHAR",
    "length": 50
  }'

# 应该成功创建

# 4. 测试无效的 element_id（应该失败）
curl -X POST http://localhost:8000/api/model/logical-tables/456/fields \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "element_id": 99999,
    "name": "测试字段",
    "column_name": "test_field",
    "data_type": "VARCHAR"
  }'

# 应该返回 400 错误："invalid element_id: element_id 99999 not found"
```

#### 6.3 前端集成测试

1. 访问 Portal: http://localhost:5170
2. 登录后应该看到两个独立的模块卡片：
   - **数据标准**（Standard 模块）
   - **数据建模**（Model 模块）
3. 点击进入 Standard 模块，测试：
   - 业务域列表和创建
   - 业务术语列表和审批流程
   - 数据元列表、详情、质量规则编辑
   - 码值集和码值项管理
4. 点击进入 Model 模块，测试：
   - 业务实体列表和属性管理
   - 逻辑表列表和字段管理
   - 在字段编辑时，数据元下拉框应该加载 Standard 模块的数据
5. 验证跨模块引用：
   - 在 Standard 模块创建一个数据元
   - 在 Model 模块创建逻辑表字段时，选择刚创建的数据元
   - 保存成功

#### 6.4 全量启动测试

```bash
# 停止所有服务
bash scripts/dev/stop.sh

# 全量启动
bash scripts/dev/start.sh

# 检查所有模块状态
curl http://localhost:8180/health  # System
curl http://localhost:8110/health  # Standard
curl http://localhost:8181/health  # Model
# ... 其他模块

# 检查 Portal
curl http://localhost:5170

# 检查模块注册
curl -H "Authorization: Bearer $TOKEN" \
     http://localhost:8000/api/system/modules
# 应该看到 standard 和 model 两个模块
```

---

## 关键文件清单

### 需要新建的文件

1. **数据库迁移脚本**
   - `scripts/migrations/split_model_to_standard.sql`

2. **Standard 模块后端**（复制自 model，删除不需要的部分）
   - `standard/backend/cmd/server/main.go`
   - `standard/backend/internal/config/config.go`
   - `standard/backend/internal/models/*.go`（4 个文件，修改 TableName）
   - `standard/backend/internal/api/*.go`（5 个文件，修改路由前缀）
   - `standard/backend/internal/service/*.go`（4 个文件）
   - `standard/backend/internal/repository/*.go`（4 个文件）

3. **Standard 模块前端**（复制自 model，删除不需要的部分）
   - `standard/frontend/src/api/standard.js`
   - `standard/frontend/src/views/*.vue`（8 个页面）
   - `standard/frontend/package.json`
   - `standard/frontend/vite.config.js`

4. **跨模块集成**
   - `common/client/standard.go`

### 需要修改的文件

1. **Model 模块后端**
   - `model/backend/cmd/server/main.go`（删除 Standard 相关代码）
   - `model/backend/internal/api/router.go`（删除 Standard 路由）
   - `model/backend/internal/service/logical_table_service.go`（添加 element_id 验证）
   - `model/backend/internal/service/entity_service.go`（添加 element_id 验证）

2. **Model 模块前端**
   - `model/frontend/src/router/index.js`（删除 Standard 路由）
   - `model/frontend/src/views/LogicalTableDetail.vue`（调用 Standard API）
   - `model/frontend/src/views/EntityDetail.vue`（调用 Standard API）

3. **配置和脚本**
   - `.env`（添加 STANDARD_BACKEND_PORT 等）
   - `docker-compose.yml`（添加 standard-backend 和 standard-frontend 服务）
   - `scripts/dev/start.sh`（添加 -standard 支持）
   - `scripts/dev/restart.sh`（添加 -standard 支持）
   - `gateway/internal/router/router.go`（添加 Standard 路由）
   - `portal/frontend/src/config/modules.js`（添加 Standard 模块卡片）

---

## 端到端验证场景

### 场景 1：数据标准管理完整流程

1. 创建业务域：`金融业务` → `客户管理`
2. 创建业务术语：`客户编号`，定义、别名、标签
3. 审批术语：从 draft → approved
4. 创建码值集：`客户类型`（个人、企业、政府）
5. 创建数据元：`customer_no`，关联术语、码值集，定义质量规则
6. 审批数据元

### 场景 2：数据建模引用标准

1. 创建业务实体：`客户实体`
2. 添加实体属性：引用数据元 `customer_no`
3. 创建逻辑表：`t_customer`
4. 添加逻辑字段：引用数据元 `customer_no`
5. 预览 DDL，验证字段定义符合数据元标准

### 场景 3：跨模块数据一致性

1. 在 Standard 模块修改数据元的质量规则
2. 在 Model 模块查看引用该数据元的逻辑字段
3. 验证修改后的规则可以被 Quality 模块读取

---

## 回滚方案

如果迁移过程中出现问题，可以回滚：

```bash
# 1. 停止所有服务
bash scripts/dev/stop.sh

# 2. 恢复数据库备份
pg_restore -h localhost -p 15432 -U addp -d addp -n model \
  --clean /tmp/model_schema_backup_YYYYMMDD_HHMMSS.dump

# 3. 删除 standard schema
psql -h localhost -p 15432 -U addp -d addp -c "DROP SCHEMA IF EXISTS standard CASCADE;"

# 4. 恢复代码
git checkout model/
rm -rf standard/

# 5. 重新启动服务
bash scripts/dev/start.sh -model
```

---

## 完成标志

当以下条件全部满足时，拆分工作完成：

- ✅ Standard 模块可以独立启动，健康检查通过
- ✅ Model 模块可以独立启动，健康检查通过
- ✅ Portal 显示两个独立的模块卡片
- ✅ Standard 模块的所有 CRUD 功能正常
- ✅ Model 模块的所有 CRUD 功能正常
- ✅ Model 创建字段时引用 Standard 数据元成功
- ✅ Model 引用不存在的 element_id 时返回 400 错误
- ✅ 前端数据元下拉框加载正常
- ✅ 全量启动测试通过
- ✅ 文档更新完成

---

## 后续优化建议

拆分完成后，可以考虑以下优化：

1. **性能优化**：
   - 在 Model Service 中添加 Redis 缓存，缓存 Element 信息
   - 减少跨模块 API 调用频率

2. **降级策略**：
   - Standard 模块不可用时，Model 模块跳过 element_id 验证
   - 记录警告日志，不阻塞业务

3. **批量验证**：
   - 提供批量验证 element_id 的 API
   - 减少多次调用的网络开销

4. **前端优化**：
   - 数据元下拉框添加本地缓存
   - 支持搜索和分页

5. **监控告警**：
   - 监控跨模块调用的成功率和延迟
   - Standard 模块异常时发送告警
