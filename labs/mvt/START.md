# 快速启动指南

## 立即开始使用 MVT 快显系统

### 1. 初始化项目（首次运行）

```bash
cd /Users/pampa/code/addp/labs/mvt

# 安装依赖并创建配置文件
make init
```

这将：
- 创建 `backend/config/app.yaml` 和 `backend/config/datasources.yaml`
- 安装 Go 模块依赖
- 安装 npm 前端依赖

### 2. 启动基础设施（PostGIS + Redis）

```bash
make up
```

验证服务状态：
```bash
docker-compose ps
```

应该看到：
- `mvt-postgis` (PostgreSQL + PostGIS) - 端口 5432
- `mvt-redis` (Redis 缓存) - 端口 6379

### 3. 准备测试数据

连接到 PostgreSQL：
```bash
make db-shell
```

在 psql 中执行：

```sql
-- 创建 PostGIS 扩展
CREATE EXTENSION IF NOT EXISTS postgis;

-- 创建测试表
CREATE TABLE buildings (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100),
    height NUMERIC(10, 2),
    floors INTEGER,
    geom GEOMETRY(Polygon, 4326)
);

-- 创建空间索引（重要！）
CREATE INDEX idx_buildings_geom ON buildings USING GIST(geom);

-- 插入测试数据（北京区域 10000 个随机建筑）
INSERT INTO buildings (name, height, floors, geom)
SELECT
    'Building ' || i,
    random() * 100 + 10,
    floor(random() * 20 + 2)::int,
    ST_SetSRID(
        ST_Buffer(
            ST_MakePoint(
                116.4 + random() * 0.1,
                39.9 + random() * 0.1
            )::geography,
            random() * 50 + 10
        )::geometry,
        4326
    )
FROM generate_series(1, 10000) i;

-- 验证数据
SELECT COUNT(*), ST_Extent(geom) FROM buildings;

-- 退出
\q
```

### 4. 启动开发服务器

**方式一：同时启动后端和前端（推荐）**

```bash
make dev
```

**方式二：分别启动（用于调试）**

终端 1（后端）：
```bash
make dev-backend
```

终端 2（前端）：
```bash
make dev-frontend
```

### 5. 访问应用

打开浏览器访问：http://localhost:5180

你应该能看到：
1. **左侧面板**：显示数据源列表（"测试建筑物数据"）
2. **中间地图**：显示 OpenStreetMap 底图
3. 点击数据源，地图上应该显示建筑物多边形

### 6. 功能测试

#### 加载数据源
- 点击左侧 "测试建筑物数据"
- 地图上应该出现青色建筑物多边形
- 缩放地图查看不同级别的显示

#### 交互测试
- **点击建筑物**：查看属性信息（名称、高度、楼层）
- **缩放地图**：观察瓦片动态加载
- **平移地图**：缓存命中后加载速度更快

#### 缓存管理
- 点击 "清空所有缓存" 按钮
- 点击 "清空当前数据源缓存" 按钮
- 观察浏览器 Network 面板中的 `X-Cache` 响应头（HIT/MISS）

### 7. API 测试

#### 获取数据源列表
```bash
curl http://localhost:8090/api/datasources
```

#### 获取单个瓦片
```bash
curl http://localhost:8090/tiles/buildings_test/14/13423/6403.mvt \
  --output test.mvt

# 查看瓦片大小
ls -lh test.mvt
```

#### 健康检查
```bash
curl http://localhost:8090/health
```

#### 缓存统计
```bash
curl http://localhost:8090/api/cache/stats
```

### 8. 常用命令

```bash
# 查看 Docker 日志
make logs

# 停止服务
make down

# 连接 PostgreSQL
make db-shell

# 连接 Redis
make redis-cli

# 清空 Redis 缓存
make redis-flush

# 构建生产版本
make build
```

### 9. 目录结构

```
mvt/
├── backend/
│   ├── cmd/server/main.go           # 主程序入口
│   ├── internal/
│   │   ├── api/                     # HTTP 路由和处理器
│   │   ├── config/                  # 配置加载
│   │   ├── service/                 # 业务逻辑
│   │   └── models/                  # 数据模型
│   ├── config/datasources.yaml      # 数据源配置
│   └── config/app.yaml              # 应用配置
│
├── frontend/
│   ├── src/
│   │   ├── components/MapViewer.vue  # 地图组件
│   │   ├── api/                      # API 客户端
│   │   └── App.vue                   # 根组件
│   └── package.json
│
├── docker-compose.yml               # Docker 编排
├── Makefile                         # 构建脚本
├── DESIGN.md                        # 详细设计文档
├── QUICKSTART.md                    # 完整快速开始指南
└── readme.md                        # 项目说明
```

### 10. 下一步

- **添加更多数据源**：编辑 `backend/config/datasources.yaml`
- **自定义样式**：修改 `MapViewer.vue` 中的图层样式
- **导入真实数据**：使用 shp2pgsql 导入 Shapefile
- **调整缓存策略**：修改 `backend/config/app.yaml` 中的缓存参数

### 11. 故障排查

#### 后端无法启动
```bash
# 检查 PostgreSQL 是否运行
docker-compose ps postgis

# 查看后端日志
go run cmd/server/main.go
```

#### 前端无法连接后端
```bash
# 检查后端是否运行
curl http://localhost:8090/health

# 检查 CORS 配置
# 确保 backend/config/app.yaml 中 frontend_url=http://localhost:5180
```

#### 地图上看不到数据
```bash
# 检查数据是否存在
make db-shell
# 然后执行: SELECT COUNT(*) FROM buildings;

# 检查空间索引
# 执行: SELECT indexname FROM pg_indexes WHERE tablename = 'buildings';
```

#### 瓦片加载慢
```bash
# 检查空间索引是否存在
make db-shell
# 执行: CREATE INDEX idx_buildings_geom ON buildings USING GIST(geom);

# 清空缓存重新测试
make redis-flush
```

---

**🎉 恭喜！你已经成功搭建了 MVT 快显系统。**

如有问题，请参考：
- 详细设计文档：[DESIGN.md](DESIGN.md)
- 完整指南：[QUICKSTART.md](QUICKSTART.md)
- 项目说明：[readme.md](readme.md)
