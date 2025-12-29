# GeoPandas Engine 容器构建总结

## ✅ 构建完成状态

**构建时间**: 2025-12-22
**镜像标签**: `localhost:5001/addp-geopandas-engine:latest`
**镜像大小**: 2.05GB
**镜像 Digest**: `sha256:979c02f2147733f1728197d1f5bef6703794a1f07100ccb2fe8b9104ca1d6b41`

## 📦 镜像信息

### 基础镜像
- **Base**: `python:3.11-slim`
- **架构**: ARM64 (适配 Apple Silicon)

### 安装的依赖

#### 系统依赖
- build-essential (编译工具链)
- g++, gcc (C/C++ 编译器)
- libgeos-dev (几何运算库)
- libproj-dev (投影转换库)
- gdal-bin, libgdal-dev (地理数据抽象库)

#### Python 依赖 (24个)
- **核心空间库**:
  - geopandas==0.14.1 (空间数据处理)
  - shapely==2.0.2 (几何对象)
  - fiona==1.10.1 (地理文件IO)
  - pyproj==3.7.2 (坐标投影)

- **数据处理**:
  - numpy==1.26.4 (数值计算)
  - pandas==2.3.3 (数据分析)

- **Web 框架**:
  - flask==3.0.0 (API 服务)
  - flask-cors==4.0.0 (跨域支持)
  - gunicorn==21.2.0 (生产服务器)

- **数据库连接**:
  - psycopg2-binary==2.9.9 (PostgreSQL)
  - pymysql==1.1.0 (MySQL)
  - sqlalchemy==2.0.23 (ORM)
  - geoalchemy2==0.14.3 (空间SQL)

- **HTTP 客户端**:
  - requests==2.31.0 (HTTP 请求)

## 🔧 构建过程修改

### 1. 修改 Dockerfile

**文件**: `engines/geopandas/Dockerfile`

添加了构建工具依赖:
```dockerfile
RUN apt-get update && apt-get install -y \
    build-essential \
    g++ \
    gcc \
    libgeos-dev \
    libproj-dev \
    gdal-bin \
    libgdal-dev \
    && rm -rf /var/lib/apt/lists/*
```

**原因**: fiona 等 Python 包需要编译 C 扩展,需要 g++ 编译器。

### 2. 修改构建脚本

**文件**: `scripts/build/build-images.sh`

#### 添加智能缓存检查 (397-407行)
```bash
geopandas-engine|spark-sedona-engine)
    # Python service: compare source file time (Dockerfile + Python source files)
    comparison_time=$(find "$service_dir" -type f '(' -name "*.py" -o -name "requirements.txt" -o -name "Dockerfile" ')' \
        -not -path "*/venv/*" -not -path "*/__pycache__/*" 2>/dev/null | \
        xargs stat -f "%m" 2>/dev/null | sort -rn | head -1)
```

监控文件:
- `*.py` (Python 源码)
- `requirements.txt` (依赖列表)
- `Dockerfile` (构建配置)

排除目录:
- `venv/` (虚拟环境)
- `__pycache__/` (Python 缓存)

#### 添加构建逻辑 (504-513行)
```bash
geopandas-engine|spark-sedona-engine)
    # Python Engine: Python service built from source (GeoPandas or Spark Sedona)
    build_context="${service_dir}"
    dockerfile_path="${service_dir}/Dockerfile"
```

#### 添加到服务列表 (593行)
```bash
"geopandas-engine:engines/geopandas"
```

### 3. 更新 Docker 配置

**文件**: `~/.docker/daemon.json`

移除了有问题的镜像源,添加了本地 registry:
```json
{
  "insecure-registries": [
    "minio",
    "localhost:5001"
  ]
}
```

## 🚀 服务状态

### 运行配置
- **容器名**: `geopandas-engine`
- **端口映射**: `8099:8099`
- **Workers**: 4 个 Gunicorn 进程
- **超时**: 600 秒
- **环境变量**:
  - `PORT=8099`
  - `PYTHONUNBUFFERED=1`

### 健康检查
```bash
$ curl http://localhost:8099/health
{
  "service": "geopandas-engine",
  "status": "healthy",
  "version": "1.0.0"
}
```

### 可用算子
**总数**: 24 个空间算子

#### 几何处理 (8个)
- `buffer` - 缓冲区分析
- `intersection` - 几何相交
- `union` - 几何合并
- `centroid` - 计算质心
- `difference` - 几何差集
- `simplify` - 简化几何
- `convex_hull` - 凸包
- `envelope` - 最小外接矩形

#### 空间关系 (3个)
- `contains` - 包含判断
- `intersects` - 相交判断
- `distance_to` - 距离计算

#### 几何属性 (3个)
- `get_area` - 计算面积
- `get_length` - 计算长度
- `get_bounds` - 获取边界框

#### 格式转换 (2个)
- `load_from_wkt` - WKT → GeoDataFrame
- `to_wkt` - GeoDataFrame → WKT

#### 坐标系统 (2个)
- `to_crs` - 坐标系转换
- `set_crs` - 设置坐标系

#### 数据操作 (4个)
- `dissolve` - 融合
- `clip` - 裁剪
- `overlay` - 叠加分析
- `spatial_join` - 空间连接

#### 其他 (2个)
- `read_file` - 读取文件
- `to_file` - 写入文件

## 📋 使用方式

### 启动服务
```bash
# 使用 docker-compose
docker-compose up -d geopandas-engine

# 或使用脚本启动整个环境
bash scripts/dev/start.sh -develop
```

### 查看状态
```bash
# 查看容器状态
docker ps | grep geopandas

# 查看日志
docker logs -f geopandas-engine

# 健康检查
curl http://localhost:8099/health
```

### API 访问
```bash
# 查看所有算子
curl http://localhost:8099/api/operators | jq .

# 执行工作流
curl -X POST http://localhost:8099/api/workflow/execute \
  -H "Content-Type: application/json" \
  -d @workflow.json
```

### 停止服务
```bash
# 停止单个服务
docker-compose stop geopandas-engine

# 停止所有服务
bash scripts/dev/stop.sh
```

## 🔄 重新构建

如果修改了 geopandas-engine 的代码:

```bash
# 方式 1: 使用构建脚本
bash scripts/build/build-images.sh --services geopandas-engine

# 方式 2: 使用 docker-compose
docker-compose build geopandas-engine

# 方式 3: 手动构建
cd engines/geopandas
docker build -t localhost:5001/addp-geopandas-engine:latest .
docker push localhost:5001/addp-geopandas-engine:latest
```

然后重启服务:
```bash
bash scripts/dev/restart.sh -develop
```

## 📝 构建时间

- **依赖安装**: ~120 秒
- **镜像导出**: ~34 秒
- **总构建时间**: ~154 秒 (约 2.5 分钟)

## ⚠️ 注意事项

1. **网络问题**: 如遇网络超时,可以:
   - 使用国内镜像源
   - 配置 HTTP 代理
   - 使用多阶段构建预缓存依赖

2. **架构兼容**: 当前镜像为 ARM64,在 AMD64 服务器上需要重新构建:
   ```bash
   docker build --platform linux/amd64 -t localhost:5001/addp-geopandas-engine:latest .
   ```

3. **内存需求**: 运行时需要至少 2GB 内存 (4 workers)

4. **依赖版本**: Python 包版本已锁定在 `requirements.txt`,确保一致性

## 🎯 下一步

- [ ] 添加更多空间算子
- [ ] 优化镜像大小 (使用多阶段构建)
- [ ] 支持多架构构建 (ARM64 + AMD64)
- [ ] 添加性能监控和日志
- [ ] 集成到 CI/CD 流程

---

**构建完成时间**: 2025-12-22 19:38:41 +0800
**构建状态**: ✅ 成功
**测试状态**: ✅ 通过
