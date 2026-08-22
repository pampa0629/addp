# 本地构建 Registry

本目录只管理 ADDP 构建链路使用的本地临时 Docker Registry。唯一地址为 `localhost:5001`，容器名为 `registry`；它是镜像构建缓存和本地部署源，不承担生产镜像仓库或备份职责。

## 标准入口

启动或确认 Registry 健康：

```bash
make registry-start
# 或直接调用同一实现
bash scripts/registry/start.sh
```

检查容器、API 和镜像目录：

```bash
make registry-status
# 或直接调用同一实现
bash scripts/registry/check.sh
```

`start.sh` 已幂等处理容器不存在、容器停止和运行但不健康的情况。仓库不提供第二套初始化脚本，也不通过根 `Makefile` 实现停止或删除容器。

## 外部 Registry 信任配置

需要让 Docker 信任局域网或其他 HTTP Registry 时，使用：

```bash
bash scripts/registry/configure.sh 192.168.1.100:5001
```

该脚本只辅助配置 Docker daemon，不创建 Registry。生产镜像仓库的地址、认证、持久化、备份和高可用由部署环境负责。

## 故障排查

```bash
docker logs registry
curl http://localhost:5001/v2/
lsof -i :5001
```

镜像构建的完整入口和参数见 [构建脚本说明](../build/README.md)。
