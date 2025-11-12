# MVT 快显系统 - 启动前检查清单

## ✅ 环境准备

### 必备软件
- [ ] Go 1.21+ 已安装（`go version`）
- [ ] Node.js 18+ 已安装（`node --version`）
- [ ] Docker 已安装（`docker --version`）
- [ ] Docker Compose 已安装（`docker-compose --version`）

### 端口检查
- [ ] 5432 端口可用（PostgreSQL）
- [ ] 6379 端口可用（Redis）
- [ ] 8090 端口可用（后端）
- [ ] 5180 端口可用（前端）

```bash
# 检查端口占用
lsof -i :5432
lsof -i :6379
lsof -i :8090
lsof -i :5180
```

## ✅ 项目初始化

- [ ] 运行 `make init` 成功
- [ ] `backend/config/app.yaml` 文件已创建
- [ ] `backend/config/datasources.yaml` 文件已创建
- [ ] Go 模块依赖已下载
- [ ] npm 前端依赖已安装

## ✅ Docker 服务

- [ ] 运行 `make up` 成功
- [ ] PostGIS 容器运行中（`docker-compose ps`）
- [ ] Redis 容器运行中（`docker-compose ps`）
- [ ] 可以连接到 PostgreSQL（`make db-shell`）
- [ ] 可以连接到 Redis（`make redis-cli`）

## ✅ 测试数据

- [ ] PostGIS 扩展已创建（`CREATE EXTENSION postgis`）
- [ ] buildings 表已创建
- [ ] 空间索引已创建（`idx_buildings_geom`）
- [ ] 测试数据已插入（10000 条记录）
- [ ] 验证数据范围（`SELECT ST_Extent(geom) FROM buildings`）

## ✅ 后端服务

- [ ] 运行 `make dev-backend` 成功
- [ ] 日志显示 "Connected to PostgreSQL"
- [ ] 日志显示 "Connected to Redis"
- [ ] 日志显示 "Server starting on :8090"
- [ ] 健康检查通过（`curl http://localhost:8090/health`）
- [ ] 数据源 API 可用（`curl http://localhost:8090/api/datasources`）

## ✅ 前端应用

- [ ] 运行 `make dev-frontend` 成功
- [ ] Vite 开发服务器启动在 5180 端口
- [ ] 浏览器可访问 http://localhost:5180
- [ ] 页面加载无错误（检查浏览器控制台）
- [ ] 左侧面板显示数据源列表
- [ ] 地图正常显示 OpenStreetMap 底图

## ✅ 功能测试

### 数据源加载
- [ ] 点击 "测试建筑物数据" 无报错
- [ ] 地图上显示青色建筑物多边形
- [ ] 缩放到 14 级可以看到建筑物
- [ ] 缩放到 10 级以下建筑物消失（minZoom 限制）

### 瓦片加载
- [ ] 打开浏览器 Network 面板
- [ ] 可以看到 `/tiles/buildings_test/{z}/{x}/{y}.mvt` 请求
- [ ] 瓦片请求返回 200 状态码
- [ ] 第一次请求返回 `X-Cache: MISS`
- [ ] 第二次请求返回 `X-Cache: HIT`（缓存命中）

### 交互功能
- [ ] 点击建筑物弹出属性信息
- [ ] 属性显示 name, height, floors 字段
- [ ] 鼠标悬停建筑物时光标变为指针
- [ ] 平移地图时瓦片动态加载

### 缓存管理
- [ ] 点击 "清空所有缓存" 按钮成功
- [ ] 点击 "清空当前数据源缓存" 按钮成功
- [ ] 清空后再次加载瓦片显示 `X-Cache: MISS`
- [ ] 缓存统计 API 可用（`curl http://localhost:8090/api/cache/stats`）

## ✅ 性能测试

### 瓦片生成速度
- [ ] 无缓存首次生成 < 100ms（可接受范围）
- [ ] 缓存命中响应 < 10ms
- [ ] 浏览器 Network 面板瓦片传输大小 < 50KB

### 缓存命中率
- [ ] 同一区域多次访问缓存命中
- [ ] Redis 键数量增长正常（`redis-cli DBSIZE`）
- [ ] 内存使用在配置范围内（`redis-cli INFO memory`）

### 数据库连接
- [ ] 连接池正常工作（日志无 "too many connections"）
- [ ] 空闲连接自动回收
- [ ] 可以同时切换多个数据源

## ✅ 错误处理

### 数据源不存在
- [ ] 访问不存在的数据源返回 404
- [ ] 前端显示友好错误提示

### 瓦片超出范围
- [ ] 访问超出 minZoom/maxZoom 的瓦片返回空瓦片
- [ ] 前端不报错

### 服务不可用
- [ ] 停止 PostgreSQL 后后端返回 500
- [ ] 停止 Redis 后仍能从 PostGIS 生成瓦片（降级）

## ✅ 文档完整性

- [ ] readme.md 存在且内容完整
- [ ] DESIGN.md 存在且内容完整
- [ ] QUICKSTART.md 存在且内容完整
- [ ] START.md 存在且内容完整
- [ ] SUMMARY.md 存在且内容完整
- [ ] Makefile 存在且命令可用

## ✅ 代码质量

### 后端
- [ ] 所有 Go 文件编译通过（`go build ./...`）
- [ ] 代码格式正确（`go fmt ./...`）
- [ ] 无明显错误（`go vet ./...`）

### 前端
- [ ] npm run build 构建成功
- [ ] 无 ESLint 错误（如果配置了）
- [ ] 浏览器控制台无 warning/error

## 🎉 最终验证

### 完整流程测试
1. [ ] 全新环境执行 `make init` 成功
2. [ ] 执行 `make up` 启动 Docker 成功
3. [ ] 导入测试数据成功
4. [ ] 执行 `make dev` 启动开发环境成功
5. [ ] 访问 http://localhost:5180 应用正常
6. [ ] 加载数据源并查看瓦片成功
7. [ ] 点击要素查看属性成功
8. [ ] 清空缓存功能正常

### 用户体验
- [ ] 首次加载时间 < 3 秒
- [ ] 地图操作流畅无卡顿
- [ ] 界面美观符合现代设计
- [ ] 错误提示清晰友好

## 📝 已知问题记录

记录在测试过程中发现的问题：

```
1. 

2. 

3. 
```

## 🚀 部署准备

- [ ] 生产环境配置文件已准备
- [ ] 数据库连接字符串已配置
- [ ] Redis 连接已配置
- [ ] 前端 API 地址已配置为生产地址
- [ ] 日志级别设置为 info/warn
- [ ] 执行 `make build` 构建成功

---

**检查完成日期**: _____________
**检查人**: _____________
**状态**: ⬜ 通过  ⬜ 部分通过  ⬜ 失败

**备注**:
