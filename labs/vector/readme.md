# Vector Tool - 图片向量化和语义检索工具

基于通义千问的 DashScope API (tongyi-embedding-vision-plus 模型) 实现图片向量化和语义检索功能。

## 项目特点

✅ **完整的向量化流程**: 支持单张图片和批量目录向量化  
✅ **语义检索**: 基于余弦相似度的图片检索  
✅ **多模态支持**: 支持图片、文本、视频的向量化  
✅ **分阶段日志**: 6 个清晰的处理阶段，便于调试  
✅ **动态维度检测**: 自动检测并配置向量维度  
✅ **独立数据库**: 使用端口 5436，与 ADDP 主系统隔离

## 快速开始

### 1. 配置已完成

`.env` 文件已配置好 API Key：
- DashScope API Key: `sk-f966cb8bbf914ec0b3dd3c1f771177fc`
- 数据库端口: `5436` (独立端口，避免与 ADDP 冲突)

### 2. 启动数据库

```bash
docker-compose up -d
```

如果遇到镜像拉取超时，配置镜像加速器后重试。

### 3. 编译

```bash
bash build.sh
```

### 4. 使用

```bash
# 初始化（首次运行，需要一张测试图片）
./vector init ../data/test.jpg

# 批量向量化
./vector batch ../data

# 语义检索
./vector search ../data/开会.jpg 5

# 查看状态
./vector status
```

## 技术架构

- **API**: 通义千问 DashScope (tongyi-embedding-vision-plus)
- **数据库**: PostgreSQL 15 + pgvector (HNSW 索引)
- **语言**: Go 1.24+
- **容器**: Docker (独立端口 5436)

## 文件说明

- [CLAUDE.md](CLAUDE.md) - 详细开发文档
- [readme.md](readme.md) - 原始需求文档
- `build.sh` - 编译脚本（解决 go.work 冲突）
- `docker-compose.yml` - 数据库容器配置

## 下一步

1. 准备测试图片到 `../data/` 目录
2. 启动数据库容器
3. 运行 `./vector init` 初始化
4. 开始向量化和检索测试

详细使用说明请参考 [CLAUDE.md](CLAUDE.md)
