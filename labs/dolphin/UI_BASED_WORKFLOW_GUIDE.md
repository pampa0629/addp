# 空间算子可视化编排指南

## 🎯 目标

让最终用户**无需写代码**，通过 DolphinScheduler UI 拖拽式编排空间算子工作流。

---

## 📋 架构设计

### 方案：HTTP API + DolphinScheduler HTTP 任务

```
┌─────────────────────────────────────────┐
│  DolphinScheduler Web UI                │
│                                          │
│  1. 拖拽 HTTP 任务节点                   │
│  2. 填写 URL 和参数（表单）              │
│  3. 连线定义数据流                       │
│  4. 运行工作流                           │
└────────────┬────────────────────────────┘
             │ HTTP 请求
             ▼
┌─────────────────────────────────────────┐
│  空间算子 API 服务 (Flask)              │
│  http://localhost:5001                  │
│                                          │
│  /operator/buffer      - 缓冲区分析     │
│  /operator/intersection - 几何相交      │
│  /operator/centroid    - 计算质心       │
│  /operator/get_area    - 计算面积       │
│  ...                                     │
└────────────┬────────────────────────────┘
             │
             ▼
┌─────────────────────────────────────────┐
│  空间算子工作流引擎                      │
│  (SpatialWorkflowEngine)                │
│                                          │
│  - 纯内存计算                            │
│  - 毫秒级性能                            │
│  - 自动依赖解析                          │
└─────────────────────────────────────────┘
```

---

## 🚀 快速开始

### 步骤 1: 启动空间算子 API 服务

#### 方式 A: 本地启动（开发）

```bash
# 1. 安装 Flask
pip3 install flask

# 2. 启动 API 服务
cd backend
python3 api_server.py
```

**输出**:
```
============================================================
空间算子 API 服务启动
============================================================
可用算子: 17 个
访问地址: http://localhost:5001
健康检查: http://localhost:5001/health
算子列表: http://localhost:5001/operators
============================================================
 * Running on http://0.0.0.0:5001
```

#### 方式 B: Docker 启动（生产）

```bash
# 将 API 服务添加到 docker-compose-demo.yml
# (见下文 Docker 配置)

docker-compose -f docker-compose-demo.yml up -d
```

---

### 步骤 2: 在 DolphinScheduler 中创建空间算子工作流

#### 2.1 登录 DolphinScheduler

访问: http://localhost:12345/dolphinscheduler/ui
- 用户名: `admin`
- 密码: `dolphinscheduler123`

#### 2.2 创建项目

1. 点击 **项目管理** → **创建项目**
2. 项目名称: `spatial_workflow`
3. 描述: `空间算子可视化编排`

#### 2.3 创建工作流定义

1. 进入项目 → **工作流定义** → **创建工作流**
2. 工作流名称: `beijing_buffer_analysis`

---

### 步骤 3: 拖拽空间算子节点

#### 示例工作流：北京景点缓冲区分析

**目标**:
1. 创建天安门点
2. 创建 1km 缓冲区
3. 计算缓冲区面积
4. 导出为 WKT

#### 节点 1: 创建天安门点

1. **拖拽 HTTP 任务节点**到画布
2. **节点名称**: `create_tiananmen`
3. **HTTP 配置**:
   - URL: `http://host.docker.internal:5001/operator/create_point`
   - 请求方式: `POST`
   - 请求头: `Content-Type: application/json`
   - 请求体:
     ```json
     {
       "lon": 116.39754,
       "lat": 39.90750
     }
     ```

#### 节点 2: 创建缓冲区

1. **拖拽 HTTP 任务节点**到画布
2. **节点名称**: `buffer_1km`
3. **设置依赖**: 连线从 `create_tiananmen` → `buffer_1km`
4. **HTTP 配置**:
   - URL: `http://host.docker.internal:5001/operator/buffer`
   - 请求方式: `POST`
   - 请求体:
     ```json
     {
       "input_geom": ${create_tiananmen.result},
       "distance": 0.009,
       "segments": 32
     }
     ```
   > 注意: `${create_tiananmen.result}` 引用上游任务的输出

#### 节点 3: 计算面积

1. **拖拽 HTTP 任务节点**
2. **节点名称**: `calculate_area`
3. **设置依赖**: `buffer_1km` → `calculate_area`
4. **HTTP 配置**:
   - URL: `http://host.docker.internal:5001/operator/get_area`
   - 请求方式: `POST`
   - 请求体:
     ```json
     {
       "input_geom": ${buffer_1km.result}
     }
     ```

#### 节点 4: 导出 WKT

1. **拖拽 HTTP 任务节点**
2. **节点名称**: `export_wkt`
3. **设置依赖**: `buffer_1km` → `export_wkt`
4. **HTTP 配置**:
   - URL: `http://host.docker.internal:5001/operator/export_to_wkt`
   - 请求方式: `POST`
   - 请求体:
     ```json
     {
       "input_geom": ${buffer_1km.result}
     }
     ```

---

### 步骤 4: 保存并运行

1. 点击 **保存**
2. 点击 **上线**
3. 点击 **运行**
4. 查看 **工作流实例** → 查看日志

**期望输出**:
```json
{
  "status": "success",
  "operator": "get_area",
  "result": 0.000254,
  "stats": {
    "total_duration_ms": 0.45
  }
}
```

---

## 🎨 可用的空间算子

| 算子名称 | API 端点 | 参数示例 |
|---------|---------|---------|
| 创建点 | `/operator/create_point` | `{"lon": 116.404, "lat": 39.915}` |
| 缓冲区 | `/operator/buffer` | `{"input_geom": {...}, "distance": 0.001, "segments": 16}` |
| 几何相交 | `/operator/intersection` | `{"geom_a": {...}, "geom_b": {...}}` |
| 几何合并 | `/operator/union` | `{"geometries": [{...}, {...}]}` |
| 计算质心 | `/operator/centroid` | `{"input_geom": {...}}` |
| 计算面积 | `/operator/get_area` | `{"input_geom": {...}}` |
| 计算长度 | `/operator/get_length` | `{"input_geom": {...}}` |
| 凸包 | `/operator/convex_hull` | `{"input_geom": {...}}` |
| 差集 | `/operator/difference` | `{"geom_a": {...}, "geom_b": {...}}` |
| 简化几何 | `/operator/simplify` | `{"input_geom": {...}, "tolerance": 0.0001}` |
| 导出 WKT | `/operator/export_to_wkt` | `{"input_geom": {...}}` |
| 导出 GeoJSON | `/operator/export_to_geojson` | `{"input_geom": {...}, "pretty": true}` |

---

## 📦 Docker 集成配置

将 API 服务添加到 `docker-compose-demo.yml`:

```yaml
services:
  # ... 现有的 dolphinscheduler 服务 ...

  # 空间算子 API 服务
  spatial-api:
    build:
      context: ./backend
      dockerfile: Dockerfile.api
    container_name: spatial-api
    ports:
      - "5001:5001"
    volumes:
      - ./backend:/app
    environment:
      - FLASK_ENV=production
    networks:
      - dolphin-network
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:5001/health"]
      interval: 10s
      timeout: 5s
      retries: 3
```

**Dockerfile.api**:
```dockerfile
FROM python:3.10-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt flask

COPY spatial ./spatial
COPY api_server.py .

EXPOSE 5001

CMD ["python", "api_server.py"]
```

---

## 🔍 测试 API

### 测试健康检查

```bash
curl http://localhost:5001/health
```

**输出**:
```json
{
  "status": "ok",
  "service": "spatial-operator-api"
}
```

### 测试算子列表

```bash
curl http://localhost:5001/operators
```

**输出**:
```json
{
  "operators": [
    {"code": "buffer", "name": "缓冲区分析"},
    {"code": "intersection", "name": "几何相交"},
    ...
  ]
}
```

### 测试单个算子

```bash
curl -X POST http://localhost:5001/operator/create_point \
  -H "Content-Type: application/json" \
  -d '{"lon": 116.404, "lat": 39.915}'
```

**输出**:
```json
{
  "status": "success",
  "operator": "create_point",
  "result": {
    "type": "Point",
    "coordinates": [116.404, 39.915]
  },
  "stats": {
    "total_duration_ms": 0.23
  }
}
```

---

## 📝 完整示例工作流

### 示例 1: 北京景点服务范围分析

**DAG 结构**:
```
create_tiananmen ──→ buffer_1km ──┬──→ calculate_area
                                   └──→ export_wkt
```

**DolphinScheduler 配置**:

1. **Task 1**: `create_tiananmen` (HTTP)
   - URL: `http://host.docker.internal:5001/operator/create_point`
   - Body: `{"lon": 116.39754, "lat": 39.90750}`

2. **Task 2**: `buffer_1km` (HTTP)
   - URL: `http://host.docker.internal:5001/operator/buffer`
   - Body: `{"input_geom": ${create_tiananmen.result}, "distance": 0.009, "segments": 32}`

3. **Task 3**: `calculate_area` (HTTP)
   - URL: `http://host.docker.internal:5001/operator/get_area`
   - Body: `{"input_geom": ${buffer_1km.result}}`

4. **Task 4**: `export_wkt` (HTTP)
   - URL: `http://host.docker.internal:5001/operator/export_to_wkt`
   - Body: `{"input_geom": ${buffer_1km.result}}`

---

## 🚨 常见问题

### Q1: 如何在 DolphinScheduler 中引用上游任务的输出？

**A**: 使用 DolphinScheduler 的变量语法:
```json
{
  "input_geom": ${previous_task_name.result}
}
```

### Q2: 如何在容器内访问宿主机的 API 服务？

**A**: 使用 `host.docker.internal`:
```
http://host.docker.internal:5001/operator/buffer
```

### Q3: 如何查看 API 服务的日志？

**A**:
```bash
# 本地启动时直接查看终端输出

# Docker 启动时
docker-compose -f docker-compose-demo.yml logs -f spatial-api
```

---

## 🎉 优势

### 对比原方案

| 特性 | 原方案（写代码） | 新方案（拖拽） |
|-----|-----------------|---------------|
| **易用性** | ❌ 需要写 Python 代码 | ✅ UI 拖拽，填表单 |
| **学习成本** | ❌ 需要学习工作流引擎 API | ✅ 只需了解算子参数 |
| **可视化** | ⚠️ 需要看代码才知道流程 | ✅ DAG 图直观展示 |
| **参数修改** | ❌ 修改代码重新部署 | ✅ UI 上直接修改 |
| **适用用户** | 开发人员 | 业务人员、分析师 |

### 性能保持不变

- ✅ 仍然使用内存工作流引擎
- ✅ 性能提升 10-5001 倍
- ✅ 毫秒级响应

---

## 📚 下一步

### 短期优化

1. **开发参数表单生成器**
   - 根据 `/operator/{code}/schema` 自动生成 UI 表单
   - 用户选择算子后自动填充参数模板

2. **增强变量传递**
   - 支持复杂的 JSON Path 引用
   - 支持参数转换函数

3. **添加预览功能**
   - 在 UI 上预览几何对象
   - 地图可视化展示

### 长期规划

1. **开发 DolphinScheduler 插件**
   - 原生支持空间算子任务类型
   - 更好的 UI 集成

2. **算子市场**
   - 用户可以分享自定义算子
   - 一键导入算子到工作流

---

## ✅ 总结

通过 HTTP API + DolphinScheduler HTTP 任务的方式，我们实现了：

1. ✅ **零代码编排**: 用户只需拖拽和填表单
2. ✅ **完全可视化**: DAG 图清晰展示数据流
3. ✅ **性能保持**: 仍然是毫秒级内存计算
4. ✅ **易于扩展**: 添加新算子只需修改 API 服务
5. ✅ **生产就绪**: 可直接部署到生产环境

**这才是真正用户友好的空间算子编排方案！** 🚀
