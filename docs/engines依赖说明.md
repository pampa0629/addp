# Engines 模块依赖版本说明

## 概述

ADDP 平台包含三个空间计算引擎，各引擎通过 HTTP REST API 通信，数据通过 JSON/GeoJSON 序列化传递，因此可以使用不同版本的依赖库而不会产生冲突。

## 依赖版本策略

### Geopandas 版本差异

| 引擎 | geopandas 版本 | 原因 |
|------|---------------|------|
| GeoPandas Engine | 0.14.1 | 生产级稳定版本，numpy<2.0 约束，工作流引擎优先稳定性 |
| Spark-Sedona Engine | 0.14.1 | 与 GeoPandas Engine 保持一致，分布式计算引擎 |
| Jupyter Engine | 1.0.0 | 最新版本，支持交互式开发新特性，Notebook 环境可容忍小幅变更 |

### 为什么不统一版本

**架构隔离保证**：

1. **模块完全独立**：三个引擎通过 HTTP REST API 通信
2. **无对象传递**：不存在跨引擎的 GeoDataFrame 对象传递
3. **数据序列化**：所有空间数据通过 JSON/GeoJSON 序列化传输
4. **独立运行环境**：各引擎使用独立 Python 虚拟环境或 Docker 容器

**实际考量**：

- GeoPandas/Spark-Sedona 是**生产工作流引擎**，优先稳定性，使用成熟的 0.14.1 版本
- Jupyter 是**交互式开发环境**，使用最新 1.0.0 支持新特性（如改进的空间索引、新增几何操作等）
- 强制统一需要代码改造成本高（API 迁移、numpy 2.0 适配），收益有限

### 版本差异的具体影响

**geopandas 0.14.1 vs 1.0.0 主要差异**：

| 方面 | 0.14.1 | 1.0.0 |
|------|--------|-------|
| Shapely 要求 | >= 2.0（兼容） | >= 2.0（仅支持） |
| I/O 引擎 | Fiona 默认 | Pyogrio 默认 |
| Numpy 要求 | < 2.0 | >= 1.26 |
| 空间索引 | rtree 支持 | Shapely 2.0 原生索引 |
| 已弃用 API | 可用但警告 | 已移除 |

**关键差异示例**：

```python
# 0.14.1 中可用（但已弃用）
gdf.geometry.values.data  # 1.0.0 中移除

# 1.0.0 中弃用的运算符重载
gdf1 & gdf2  # 建议改为 gdf1.intersection(gdf2)
gdf1 | gdf2  # 建议改为 gdf1.union(gdf2)
```

---

## 其他关键依赖

### 统一版本依赖

以下依赖在三个引擎中保持统一：

| 依赖 | 版本 | 说明 |
|------|------|------|
| pandas | >=2.0,<3.0 | 数据处理核心库，显式声明版本范围 |
| flask | 3.0.0 | Web 框架 |
| flask-cors | 4.0.0 | CORS 支持 |
| psycopg2-binary | 2.9.9 | PostgreSQL 连接 |
| pymysql | 1.1.0 | MySQL 连接 |
| sqlalchemy | 2.0.23 | ORM 框架 |
| requests | 2.31.0 | HTTP 客户端 |

### 引擎特有依赖

**GeoPandas Engine**：

```txt
numpy<2.0              # 与 geopandas 0.14.1 兼容
shapely==2.0.2         # 空间几何操作
geoalchemy2==0.14.3    # PostGIS 支持
simpleeval==0.9.13     # 字段计算表达式求值
gunicorn==21.2.0       # WSGI 服务器
```

**Spark-Sedona Engine**：

```txt
pyspark==3.5.0          # Spark 分布式计算框架
apache-sedona==1.5.1    # Spark 空间扩展
gunicorn==21.2.0        # WSGI 服务器（新增）
```

**Jupyter Engine**：

```txt
jupyterlab==4.0.9       # Jupyter Lab 界面
papermill==2.5.0        # Notebook 参数化执行
ipykernel==6.27.1       # IPython 内核
nbformat==5.9.2         # Notebook 格式处理
```

---

## 升级注意事项

### 如果未来需要统一为 geopandas 1.0.0

需要进行以下改造：

1. **Numpy 2.0 适配**
   ```bash
   # 所有引擎升级 numpy 版本约束
   numpy>=1.26,<3.0
   ```

2. **API 迁移**
   ```python
   # 检查并替换以下用法
   gdf.geometry.values.data → gdf.geometry.values
   gdf.__xor__() → gdf.symmetric_difference()
   gdf.__or__() → gdf.union()
   ```

3. **空间索引更新**
   ```python
   # 移除 rtree 依赖，使用 Shapely 2.0 原生索引
   sindex.query_bulk() → sindex.query()
   ```

4. **I/O 引擎切换**
   ```bash
   # 移除 Fiona，使用 Pyogrio
   pip uninstall fiona
   pip install pyogrio
   ```

5. **测试验证**
   - 运行所有空间算子单元测试
   - 验证 GeoJSON 序列化/反序列化
   - 检查工作流执行结果一致性

### 升级收益评估

**升级收益**：
- 性能提升（Pyogrio I/O 引擎更快）
- 支持 numpy 2.0（未来 Python 3.13+ 可能要求）
- 新增空间操作（Voronoi 图改进、几何集合操作增强）

**升级成本**：
- 代码改造工作量：约 2-3 天
- 测试验证工作量：约 1-2 天
- 风险：可能影响现有工作流结果

**建议时机**：
- 当 Python 3.13 成为主流版本时（numpy 2.0 强制要求）
- 当需要 geopandas 1.0.0 特有新功能时
- 在大版本升级窗口期进行统一升级

---

## 依赖管理最佳实践

### 虚拟环境隔离

**开发环境**（本地）：

```bash
# 各引擎使用独立 venv
engines/geopandas/venv
engines/spark-sedona/venv
engines/jupyter/venv
```

**生产环境**（Docker）：

```yaml
# 各引擎使用独立容器
geopandas-engine:
  image: addp-geopandas-engine:latest
  # 独立依赖安装

spark-sedona-engine:
  image: addp-spark-sedona-engine:latest
  # 独立依赖安装
```

### 依赖版本锁定

- **显式声明范围**：使用 `>=X.Y,<Z.0` 格式明确版本范围
- **避免 `>=X.Y.Z`**：过于宽松，可能引入破坏性变更
- **关键库固定版本**：如 `geopandas==0.14.1` 避免意外升级

### 依赖更新流程

1. **定期检查**：每季度检查依赖更新
2. **测试环境验证**：先在测试环境升级验证
3. **逐个引擎更新**：避免同时更新多个引擎
4. **回归测试**：运行完整的空间算子测试套件
5. **文档更新**：更新本文档记录版本变更原因

---

## 常见问题

### Q1: 为什么 GeoPandas 和 Jupyter 的 geopandas 版本不同？

**A**: 因为它们的用途不同：
- GeoPandas Engine 是生产工作流引擎，优先稳定性
- Jupyter Engine 是交互式开发环境，可以使用最新特性
- 两者通过 HTTP API 通信，版本差异不影响数据交换

### Q2: 版本不同会导致计算结果不一致吗？

**A**: 不会，原因：
- 空间算法实现在 Shapely 2.0.x（三个引擎都使用）
- 数据通过 GeoJSON 序列化传输，与 geopandas 版本无关
- 已验证相同输入在不同版本下产生相同 GeoJSON 输出

### Q3: 什么时候需要统一版本？

**A**: 以下情况需要考虑统一：
- Python 3.13+ 强制要求 numpy 2.0
- 需要使用 geopandas 1.0.0 特有新功能
- 发现版本差异导致的兼容性问题（目前未发现）

### Q4: 如何验证依赖版本是否正确？

**A**: 运行以下命令：

```bash
# 开发环境
engines/geopandas/venv/bin/pip list | grep -E 'geopandas|pandas|numpy'
engines/spark-sedona/venv/bin/pip list | grep -E 'geopandas|pandas|numpy'
engines/jupyter/venv/bin/pip list | grep -E 'geopandas|pandas|numpy'

# Docker 环境
docker exec geopandas-engine pip list | grep -E 'geopandas|pandas|numpy'
docker exec spark-sedona-engine pip list | grep -E 'geopandas|pandas|numpy'
docker exec jupyter-engine pip list | grep -E 'geopandas|pandas|numpy'
```

---

## 版本变更历史

| 日期 | 引擎 | 变更 | 原因 |
|------|------|------|------|
| 2025-12-23 | 所有 | 显式声明 `pandas>=2.0,<3.0` | 避免安装低版本 pandas |
| 2025-12-23 | Spark-Sedona | 添加 `gunicorn==21.2.0` | 统一使用生产级 WSGI 服务器 |
| 2025-12-23 | Spark-Sedona | 修改 `pandas>=2.2.0` → `pandas>=2.0,<3.0` | 统一版本范围格式 |

---

## 参考资源

- [GeoPandas 1.0.0 发布说明](https://geopandas.org/en/v1.0.0/docs/changelog.html)
- [GeoPandas 0.14.1 文档](https://geopandas.org/en/v0.14.4/)
- [从 PyGEOS 迁移到 Shapely 2.0 指南](https://geopandas.org/en/v1.0.0/docs/user_guide/pygeos_to_shapely.html)
- [Numpy 2.0 迁移指南](https://numpy.org/devdocs/numpy_2_0_migration_guide.html)
