# CLAUDE.md

本文件为 Claude Code (claude.ai/code) 提供在本仓库中工作时的指导说明。

## 项目概述

本项目是一个独立的实验室项目，用于验证**图片向量化和语义检索**功能。这是一个 Golang 开发的命令行工具，使用通义千问的 DashScope API 提取图片向量，并存储到 PostgreSQL 的 pgvector 扩展中进行语义检索。

**重要**: 本项目是独立的研究项目，不与 ADDP 主系统交互。目的是快速验证技术可行性，避免复杂度拖慢研发进度。

## 核心需求

根据 [readme.md](readme.md)，项目需要满足以下要求：

1. **向量提取**: 使用通义千问的 DashScope API 提取图片向量
2. **开发语言**: Golang
3. **向量存储**: PostgreSQL 的 pgvector 扩展，数据库使用容器部署
4. **功能**: 支持语义检索
5. **开发模式**: 本地 cmd 程序，图片路径硬编码
6. **调试支持**: 提供分阶段的详细输出，便于排查错误

## 技术栈

### 核心技术

- **语言**: Go 1.23+
- **AI 服务**: 阿里云通义千问 DashScope API（图片向量化）
- **数据库**: PostgreSQL 15+ with pgvector extension
- **容器**: Docker（用于 PostgreSQL 部署）

### 关键 Go 依赖（预期）

```go
// DashScope SDK
github.com/aliyun/alibabacloud-sdk-go-v2

// PostgreSQL 驱动
github.com/lib/pq
// 或使用 GORM
gorm.io/gorm
gorm.io/driver/postgres

// pgvector 支持
github.com/pgvector/pgvector-go
```

## 项目结构（建议）

```
vector/
├── CLAUDE.md              # 本文件
├── readme.md              # 项目需求说明
├── go.mod                 # Go 模块定义
├── go.sum                 # 依赖版本锁定
├── main.go                # 主入口（CLI）
├── cmd/                   # 命令行工具
│   └── vectorize/         # 向量化命令
├── internal/              # 内部实现
│   ├── dashscope/         # DashScope API 客户端
│   ├── database/          # PostgreSQL + pgvector
│   └── search/            # 语义检索实现
├── config/                # 配置文件
│   └── config.yaml        # API 密钥、数据库连接等
├── docker-compose.yml     # PostgreSQL 容器定义
└── testdata/              # 测试图片
    └── images/            # 硬编码的测试图片路径
```

## 开发工作流

### 1. 初始化项目

```bash
# 初始化 Go 模块
go mod init github.com/addp/labs/vector

# 安装依赖
go mod tidy
```

### 2. 启动 PostgreSQL（带 pgvector）

创建 `docker-compose.yml`:

```yaml
version: '3.8'
services:
  postgres:
    image: pgvector/pgvector:pg15
    container_name: vector-postgres
    environment:
      POSTGRES_DB: vectordb
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:
```

启动数据库：

```bash
docker-compose up -d

# 验证 pgvector 扩展
docker exec -it vector-postgres psql -U postgres -d vectordb -c "CREATE EXTENSION IF NOT EXISTS vector;"
```

### 3. 配置 DashScope API

创建配置文件 `config/config.yaml`:

```yaml
dashscope:
  api_key: "your-dashscope-api-key"  # 从阿里云控制台获取
  endpoint: "https://dashscope.aliyuncs.com/api/v1/services/vision/multimodal/embeddings"

database:
  host: "localhost"
  port: 5432
  user: "postgres"
  password: "postgres"
  dbname: "vectordb"
  sslmode: "disable"

# 测试图片路径（硬编码）
test_images:
  - "testdata/images/cat.jpg"
  - "testdata/images/dog.jpg"
  - "testdata/images/car.jpg"
```

**安全提示**: 不要将 API 密钥提交到 Git。使用环境变量或 `.gitignore` 排除配置文件。

### 4. 运行程序

```bash
# 向量化图片并存储
go run main.go vectorize --image testdata/images/cat.jpg

# 语义检索（查找相似图片）
go run main.go search --image testdata/images/query.jpg --top 5

# 构建可执行文件
go build -o vector-tool main.go
./vector-tool vectorize --image testdata/images/cat.jpg
```

### 5. 测试

```bash
# 运行单元测试
go test ./...

# 运行特定包的测试
go test ./internal/dashscope -v

# 运行集成测试
go test ./internal/database -v -tags=integration
```

## 核心实现要点

### 1. DashScope API 集成

**图片向量化流程**:

```go
// internal/dashscope/client.go
type Client struct {
    apiKey   string
    endpoint string
}

// 提取图片向量（返回 []float32）
func (c *Client) ExtractVector(imagePath string) ([]float32, error) {
    // 1. 读取图片文件
    // 2. Base64 编码（如果 API 需要）
    // 3. 调用 DashScope multimodal embeddings API
    // 4. 解析返回的向量（通常是 512 或 1024 维）
    // 5. 详细输出：API 请求、响应状态、向量维度
}
```

**详细输出示例**（用于排查错误）:

```
[Stage 1] 读取图片: testdata/images/cat.jpg
[Stage 2] 图片大小: 2.3 MB (2400x1800)
[Stage 3] 调用 DashScope API...
[Stage 4] API 响应状态: 200 OK
[Stage 5] 向量维度: 512
[Stage 6] 向量提取成功
```

### 2. pgvector 数据库操作

**数据库 Schema**:

```sql
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE image_vectors (
    id SERIAL PRIMARY KEY,
    image_path TEXT NOT NULL UNIQUE,
    vector vector(512),  -- 维度根据 DashScope 返回调整
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 创建向量索引（提升检索性能）
CREATE INDEX ON image_vectors USING ivfflat (vector vector_cosine_ops);
```

**存储向量**:

```go
// internal/database/repository.go
type Repository struct {
    db *sql.DB  // 或 *gorm.DB
}

func (r *Repository) InsertVector(imagePath string, vector []float32) error {
    // 1. 转换 []float32 为 pgvector 格式
    // 2. INSERT INTO image_vectors...
    // 3. 详细输出：SQL 语句、插入行数
}
```

### 3. 语义检索实现

**余弦相似度搜索**:

```go
// internal/search/searcher.go
func (s *Searcher) FindSimilar(queryVector []float32, topK int) ([]Result, error) {
    // 使用 pgvector 的余弦距离运算符
    query := `
        SELECT image_path, 1 - (vector <=> $1) AS similarity
        FROM image_vectors
        ORDER BY vector <=> $1
        LIMIT $2
    `
    // 1. 执行查询
    // 2. 返回 top K 相似图片
    // 3. 详细输出：查询向量、相似度分数、结果数量
}
```

**详细输出示例**:

```
[Stage 1] 提取查询图片向量...
[Stage 2] 向量维度: 512
[Stage 3] 执行语义检索（Top 5）...
[Stage 4] 找到 5 个相似图片:
  1. cat2.jpg (相似度: 0.95)
  2. cat3.jpg (相似度: 0.89)
  3. cat1.jpg (相似度: 0.87)
  4. kitten.jpg (相似度: 0.82)
  5. tiger.jpg (相似度: 0.75)
```

## 常用命令

### 数据库管理

```bash
# 连接数据库
docker exec -it vector-postgres psql -U postgres -d vectordb

# 查看所有向量
SELECT id, image_path, created_at FROM image_vectors;

# 查看向量维度
SELECT pg_typeof(vector) FROM image_vectors LIMIT 1;

# 清空数据（重新测试）
TRUNCATE image_vectors;

# 停止数据库
docker-compose down

# 停止并删除数据
docker-compose down -v
```

### Go 开发

```bash
# 格式化代码
go fmt ./...

# 静态检查
go vet ./...

# 下载依赖
go mod download

# 查看依赖树
go mod graph

# 更新依赖
go get -u ./...
```

## 调试和排错

### 分阶段详细输出

代码中应包含详细的日志输出，方便排查问题：

```go
// 使用标准日志或结构化日志（如 logrus）
import "log"

func main() {
    log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

    log.Println("[Stage 1] 初始化配置...")
    log.Println("[Stage 2] 连接数据库...")
    log.Println("[Stage 3] 读取图片...")
    // ... 每个关键步骤都输出日志
}
```

### 常见问题

1. **DashScope API 调用失败**
   - 检查 API 密钥是否正确
   - 检查网络连接（可能需要配置代理）
   - 查看 API 响应错误信息

2. **pgvector 扩展未安装**
   ```bash
   docker exec -it vector-postgres psql -U postgres -d vectordb -c "CREATE EXTENSION vector;"
   ```

3. **向量维度不匹配**
   - 确保 SQL schema 中的 `vector(512)` 维度与 DashScope 返回一致
   - 可以先打印向量长度再创建表

4. **检索结果不理想**
   - 尝试不同的距离度量（余弦、欧氏、内积）
   - 增加训练数据量
   - 调整 ivfflat 索引参数

## 参考资料

- [DashScope API 文档](https://help.aliyun.com/zh/dashscope/)
- [pgvector GitHub](https://github.com/pgvector/pgvector)
- [pgvector-go SDK](https://github.com/pgvector/pgvector-go)
- [Go PostgreSQL 最佳实践](https://www.calhoun.io/connecting-to-a-postgresql-database-with-gos-database-sql-package/)

## 开发原则

1. **快速验证**: 优先实现核心功能，验证技术可行性
2. **详细输出**: 每个关键步骤都输出日志，便于调试
3. **独立运行**: 不依赖 ADDP 主系统，保持简单
4. **可复现**: 使用硬编码的测试数据，确保结果一致
5. **文档记录**: 记录遇到的问题和解决方案，便于后续集成到 ADDP

## 下一步计划

验证成功后，考虑以下优化方向：

1. 支持批量向量化（提升效率）
2. 添加图片预处理（缩放、格式转换）
3. 实现多种距离度量（余弦、欧氏、内积）
4. 性能测试（大规模向量检索）
5. 将功能集成到 ADDP 的 Manager 或 Meta 模块
