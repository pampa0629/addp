# Port Allocation (ADDP)

统一定义 ADDP 系统库与业务库的容器端口，避免冲突。

## System (ADDP 基础设施)

- PostgreSQL: `15432`
- Redis: `16379`
- MinIO API: `19000`
- MinIO Console: `19001`
- Meilisearch: `17700`

来源：`docker-compose.infra.yml`。脚本固定使用这些端口，不会自动改动；若被其他进程占用，`scripts/infra/up.sh` 会给出提示，可能导致启动失败。请使用 `lsof -nP -i :<port>` 查占用并释放，或手动调整 compose 端口映射。

## Business (业务库)

- PostgreSQL: `5433`
- MinIO API: `9002`
- MinIO Console: `9003`

来源：`business/docker-compose.yml`，可通过 `business/.env` 覆盖。脚本固定使用这些端口，不会自动改动；若被其他进程占用，启动脚本会给出警告并继续尝试（可能失败）。

```bash
BUSINESS_POSTGRES_PORT=5433
BUSINESS_MINIO_API_PORT=9002
BUSINESS_MINIO_CONSOLE_PORT=9003
```

## Reserved Policy（保留规则）

- **System MinIO 使用 19000/19001**，Business 侧不得占用这两个端口。
- **Business MinIO 使用 9002/9003**，System 侧不得占用这两个端口。
- System PostgreSQL 使用 15432；Business PostgreSQL 使用 5433。

脚本约束：

- `scripts/infra/up.sh`：若检测到 `business-minio` 占用了 19000/19001，将报错并退出，提示修改 `business/.env`。
- `business/scripts/start.sh`：若配置了 19000/19001，将报错并退出，提示改为 9002/9003；对 5433 端口仅警告不改动。

## 快速校验

使用命令校验策略是否符合：

```
make ports-validate
```

输出会显示 business/.env 的端口配置、System 默认端口以及当前运行容器的实际映射，帮助定位问题。

如果本地已有其他服务占用 19000/19001，可改用其他未占用端口。

## 使用建议

- 同机运行 System 与 Business：
  - 先启动 Business：`cd business && ./scripts/start.sh`
  - 再启动 System 基础设施：`bash scripts/infra-up.sh` 或 `make up-infra`
- 如遇端口冲突：
  - 参考本文件调整 `business/.env` 或根目录 `.env`
  - 重新启动对应容器：`docker-compose down && docker-compose up -d`


### 端口分配

**ADDP 系统服务**:


| 服务                  | 开发端口 | Docker 端口 | 说明                       |
| --------------------- | -------- | ----------- | -------------------------- |
| **Nginx Gateway**     | **80**   | **80**      | **统一入口 (推荐)**        |
| **Portal Frontend**   | **5170** | **5170**    | **Portal UI (通过 Nginx)** |
| Gateway               | 8000     | 8000        | API Gateway (后端路由)     |
| System Backend        | 8180     | 8180        | 认证、用户、日志 (统一使用8180避免端口冲突) |
| System Frontend       | 5173     | 8090        | 独立访问                   |
| Manager Backend       | 8081     | 8081        | 数据源、文件               |
| Manager Frontend      | 5174     | 8091        | 独立访问                   |
| Meta Backend          | 8082     | 8082        | 元数据、血缘               |
| Meta Frontend         | 5175     | 8092        | 独立访问                   |
| Transfer Backend      | 8083     | 8083        | 导入/导出任务              |
| Transfer Frontend     | 5176     | 8093        | 独立访问                   |
| Orchestrator Backend  | 8084     | 8084        | 工作流编排                 |
| Orchestrator Frontend | 5177     | 8094        | 独立访问                   |
| Develop Backend       | 8085     | 8085        | 开发工具                   |
| Develop Frontend      | 5178     | 8095        | 独立访问                   |
| Service Backend       | 8086     | 8086        | 数据服务、OGC 标准服务     |
| Service Frontend      | 5180     | 8096        | 独立访问                   |
| Monitor Backend       | 8100     | 8100        | 执行监控、统计分析         |
| Monitor Frontend      | 5179     | 5179        | 监控仪表盘                 |
| Copilot Backend       | 8087     | 8087        | AI 助手 (工作流/SQL生成)   |
| Jupyter Lab UI        | 8088     | 8088        | Jupyter 笔记本开发界面     |
| Math Workflow Engine  | 8089     | 8089        | 数学计算工作流引擎         |
| Jupyter API Server    | 8097     | 8097        | Jupyter 执行引擎 API       |
| Spark Workflow Engine | 8098     | 8098        | Spark 分布式工作流引擎     |
| Python Workflow Engine     | 8099     | 8099        | 空间计算引擎 (Python)      |
| PostgreSQL (System)   | 15432    | 15432       | ADDP 系统元数据            |
| Redis                 | 16379    | 16379       | 缓存和队列                 |
| MinIO System API      | 19000    | 19000       | 系统文件存储               |
| MinIO System Console  | 19001    | 19001       | 系统 MinIO Web UI          |
| Meilisearch           | 17700    | 17700       | 全文检索引擎               |
