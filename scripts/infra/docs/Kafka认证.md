# ADDP Infra：Kafka认证

[返回 Infra 指南](../README.md)

## Infra Kafka 认证

默认 `docker-compose.infra.yml` 使用唯一 `redpanda_data` 卷，不向 Transfer 任务、API、数据库或 System Engine 引入发行版字段。完整认证会重建 `addp-redpanda` 和 `addp-kafka-connect`，存在活动 connector 时脚本会拒绝执行：

```bash
bash scripts/test/certify-infra-kafka.sh
```

生产拓扑认证使用 `docker-compose.infra.ha.yml`，在同一 Kafka API 数据面中启动 3 broker、2 个同组 Connect worker、SASL_SSL、RF=3 和 Raft majority；宿主机 listener 为 `19092`、`19093`、`19094`：

```bash
bash scripts/test/certify-infra-kafka-ha.sh
```

脚本会生成临时 CA/证书，验证 `acks=all`、关闭 write caching、单 broker `SIGKILL`、双 broker quorum、Connect owner failover、PostgreSQL/MySQL CDC、DLQ、retention、Stop cleanup、性能和资源，并在所有退出路径恢复默认单机 Redpanda profile。认证证据写入操作系统临时目录。
