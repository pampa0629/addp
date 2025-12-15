# Apache Doris 学习环境

欢迎来到 Apache Doris 学习环境！这个项目提供了完整的 Doris 学习路径，从基础概念到高级特性，再到 ADDP 平台集成。

## 快速开始

### 1. 启动 Doris 集群

```bash
# 在 labs/doris_test 目录下执行
docker-compose up -d

# 等待服务启动（约 30-60 秒）
docker-compose ps

# 查看日志
docker-compose logs -f
```

### 2. 初始化集群

```bash
# 执行初始化脚本（添加 BE 节点）
bash scripts/init_cluster.sh

# 验证集群状态
bash scripts/health_check.sh
```

### 3. 连接 Doris

```bash
# 使用 MySQL 客户端连接
mysql -h127.0.0.1 -P19030 -uroot

# 查看数据库
SHOW DATABASES;

# 创建测试数据库
CREATE DATABASE learning_db;
USE learning_db;
```

### 4. 开始学习

按照以下顺序学习：

1. **第一周 - 基础掌握** → [sql/01-basic/](sql/01-basic/)
   - 4 种表模型
   - 分区和分桶
   - 数据类型

2. **第二周 - 数据导入** → [sql/02-data-load/](sql/02-data-load/)
   - INSERT 语句
   - Stream Load
   - CSV 导入

3. **第三周 - 查询优化** → [sql/03-query/](sql/03-query/)
   - 聚合查询
   - JOIN 优化
   - 物化视图

4. **第四周 - 高级特性** → [sql/04-advanced/](sql/04-advanced/)
   - Bitmap 索引
   - Rollup 表
   - 动态分区

5. **ADDP 集成** → [sql/05-integration/](sql/05-integration/)
   - 资源配置
   - 元数据扫描
   - 数据传输

---

## 目录结构

```
doris_test/
├── README.md                    # 本文件
├── docker-compose.yml           # Docker Compose 配置
├── sql/                         # SQL 脚本
│   ├── 01-basic/               # 基础操作
│   ├── 02-data-load/           # 数据导入
│   ├── 03-query/               # 查询优化
│   ├── 04-advanced/            # 高级特性
│   └── 05-integration/         # ADDP 集成
├── data/                        # 测试数据
│   ├── sample_data.csv
│   ├── generate_data.py
│   └── README.md
├── docs/                        # 学习文档
│   ├── 01_architecture.md
│   ├── 02_table_models.md
│   ├── 03_data_load.md
│   ├── 04_query_optimization.md
│   └── 05_addp_integration.md
└── scripts/                     # 管理脚本
    ├── init_cluster.sh
    ├── health_check.sh
    └── cleanup.sh
```

---

## 访问方式

### MySQL 协议

```bash
# 命令行连接
mysql -h127.0.0.1 -P19030 -uroot

# DBeaver / Navicat 等图形化工具
Host: 127.0.0.1
Port: 19030
User: root
Password: (空)
```

### Web UI

- **FE Web UI**: http://localhost:18030
  - 查看集群状态
  - 监控查询性能
  - 查看系统配置

- **BE Web UI**: http://localhost:18040
  - 查看 BE 节点状态
  - 监控资源使用
  - 查看数据分布

---

## 学习路径

### 第一周：基础掌握

**目标**: 理解 Doris 核心概念

- [ ] 部署 Doris 集群并验证
- [ ] 理解 FE/BE 架构
- [ ] 掌握 4 种表模型差异
- [ ] 练习基础 DDL/DML 操作
- [ ] 导入 10 万条测试数据

**参考**: [docs/01_architecture.md](docs/01_architecture.md), [docs/02_table_models.md](docs/02_table_models.md)

### 第二周：数据导入

**目标**: 掌握高性能数据导入方法

- [ ] INSERT 语句导入
- [ ] Stream Load 批量导入
- [ ] CSV 文件导入
- [ ] 性能对比测试
- [ ] 导入 100 万条数据

**参考**: [docs/03_data_load.md](docs/03_data_load.md)

### 第三周：查询优化

**目标**: 理解查询优化技巧

- [ ] EXPLAIN 分析执行计划
- [ ] 创建物化视图
- [ ] Colocation Join 优化
- [ ] Bitmap 精确去重
- [ ] 查询性能对比

**参考**: [docs/04_query_optimization.md](docs/04_query_optimization.md)

### 第四周：ADDP 集成

**目标**: 在 ADDP 平台使用 Doris

- [ ] 在 System 模块注册 Doris 资源
- [ ] Meta 模块扫描元数据
- [ ] Develop 工作台执行 SQL
- [ ] Transfer 模块导入导出数据

**参考**: [docs/05_addp_integration.md](docs/05_addp_integration.md)

---

## 常用命令

### 集群管理

```bash
# 启动集群
docker-compose up -d

# 停止集群
docker-compose down

# 查看日志
docker-compose logs -f doris-fe
docker-compose logs -f doris-be

# 重启服务
docker-compose restart

# 清理数据（⚠️ 会删除所有数据）
bash scripts/cleanup.sh
```

### Doris SQL

```sql
-- 查看 FE 节点
SHOW FRONTENDS\G

-- 查看 BE 节点
SHOW BACKENDS\G

-- 查看数据库
SHOW DATABASES;

-- 查看表
SHOW TABLES FROM learning_db;

-- 查看表结构
DESC learning_db.table_name;

-- 查看表数据量
SELECT COUNT(*) FROM learning_db.table_name;

-- 查看执行计划
EXPLAIN SELECT * FROM learning_db.table_name;
```

---

## 性能基准

### 硬件要求

**最小配置**（开发学习）:
- CPU: 4 核
- 内存: 8GB
- 磁盘: 20GB SSD

**推荐配置**（性能测试）:
- CPU: 8 核+
- 内存: 16GB+
- 磁盘: 50GB+ SSD

### 性能参考

| 操作 | 数据量 | 性能指标 |
|------|--------|---------|
| **INSERT** | 1 万行 | 约 10 秒 |
| **Stream Load** | 100 万行 | 约 10-20 秒 |
| **聚合查询** | 100 万行 | < 1 秒 |
| **JOIN 查询** | 2 张表各 50 万行 | < 2 秒 |
| **Bitmap 去重** | 1000 万行 | < 1 秒 |

---

## 故障排查

### 问题 1: FE 启动失败

**症状**: `docker-compose ps` 显示 FE 容器反复重启

**排查**:
```bash
# 查看 FE 日志
docker-compose logs doris-fe

# 检查端口占用
lsof -i :19030
lsof -i :18030
```

**解决**: 修改 docker-compose.yml 中的端口映射，避免冲突

### 问题 2: BE 节点未添加

**症状**: `SHOW BACKENDS` 显示为空

**排查**:
```bash
# 检查 BE 是否启动
docker-compose ps

# 查看 BE 日志
docker-compose logs doris-be
```

**解决**: 手动添加 BE 节点
```sql
ALTER SYSTEM ADD BACKEND "doris-be:9050";
```

### 问题 3: 查询性能慢

**症状**: 简单查询耗时超过 5 秒

**排查**:
```sql
-- 查看执行计划
EXPLAIN SELECT * FROM table_name WHERE ...;

-- 检查分区裁剪
EXPLAIN SELECT * FROM table_name WHERE date_col = '2025-01-01';
```

**解决**:
1. 创建合适的索引
2. 使用分区裁剪
3. 创建物化视图

---

## 学习资源

### 官方文档

- **Doris 官网**: https://doris.apache.org/zh-CN/
- **快速开始**: https://doris.apache.org/zh-CN/docs/get-starting/quick-start
- **表设计指南**: https://doris.apache.org/zh-CN/docs/table-design/data-model
- **数据导入**: https://doris.apache.org/zh-CN/docs/data-operate/import/import-way/stream-load-manual

### 社区资源

- **GitHub**: https://github.com/apache/doris
- **Slack**: https://join.slack.com/t/apachedoriscommunity/
- **中文社区**: https://doris.apache.org/zh-CN/community/

### 推荐阅读

1. **Doris 架构设计**: 理解 FE/BE 角色和 MPP 架构
2. **表模型选择**: 根据业务场景选择合适的表模型
3. **查询优化技巧**: EXPLAIN、物化视图、Colocation Join
4. **数据导入最佳实践**: Stream Load vs INSERT vs Broker Load

---

## 贡献

欢迎提交问题和改进建议！如果你发现任何错误或有更好的学习案例，请：

1. 在 ADDP 项目中提 Issue
2. 提交 Pull Request
3. 完善学习文档

---

## 许可证

本学习环境遵循 ADDP 项目许可证。

Apache Doris 遵循 Apache License 2.0。

---

**Happy Learning! 🚀**

边学边做，保持好奇心，记录每一个发现！
