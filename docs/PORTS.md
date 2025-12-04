# Port Allocation (ADDP)

统一定义 ADDP 系统库与业务库的容器端口，避免冲突。

## System (ADDP 基础设施)

- PostgreSQL: `5432`
- Redis: `6379`
- MinIO API: `9002`
- MinIO Console: `9003`

来源：`docker-compose.yml`。脚本固定使用这些端口，不会自动改动；若被其他进程占用，`scripts/infra-up.sh` 会给出提示，可能导致启动失败。请使用 `lsof -nP -i :<port>` 查占用并释放，或手动调整 compose 端口映射。

## Business (业务基础设施)

- PostgreSQL: `5433`
- MinIO API: `9000`（推荐）
- MinIO Console: `9001`（推荐）

来源：`business/docker-compose.yml`，可通过 `business/.env` 覆盖。脚本固定使用这些端口，不会自动改动；若被其他进程占用，启动脚本会给出警告并继续尝试（可能失败）。

```
BUSINESS_POSTGRES_PORT=5433
BUSINESS_MINIO_API_PORT=9000
BUSINESS_MINIO_CONSOLE_PORT=9001
```

## Reserved Policy（保留规则）

- System MinIO 使用 9002/9003，Business 侧不得占用这两个端口。
- Business MinIO 使用 9000/9001，System 侧不得占用这两个端口。
- System PostgreSQL 使用 5432；Business PostgreSQL 使用 5433。

脚本约束：
- `scripts/infra-up.sh`：若检测到 `business-minio` 占用了 9002/9003，将报错并退出，提示修改 `business/.env`。
- `business/scripts/start.sh`：若配置了 `9002/9003`，将报错并退出，提示改为 `9000/9001`；对 `5433` 端口仅警告不改动。

## 快速校验

使用命令校验策略是否符合：

```
make ports-validate
```

输出会显示 business/.env 的端口配置、System 默认端口以及当前运行容器的实际映射，帮助定位问题。

如果本地已有其他服务占用 9000/9001，可改用 `9100/9101` 或其他未占用端口。

## 使用建议

- 同机运行 System 与 Business：
  - 先启动 Business：`cd business && ./scripts/start.sh`
  - 再启动 System 基础设施：`bash scripts/infra-up.sh` 或 `make up-infra`
- 如遇端口冲突：
  - 参考本文件调整 `business/.env` 或根目录 `.env`
  - 重新启动对应容器：`docker-compose down && docker-compose up -d`
