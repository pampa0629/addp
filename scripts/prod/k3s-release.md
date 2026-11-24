# 混合架构 K3s 镜像交付

## 制作发布包

1. 在原生 ARM64 和 AMD64 Linux 构建环境检出同一完整 Git 提交。使用现有 Compose 发布的镜像准备阶段生成两份冻结清单。构建端使用环境模板解析 Compose 输入，站点密钥在部署阶段独立提供。除 SuperMap 外，所有发布选项一致。

   ```bash
   # 102 ARM64 在原生 SDK 和基础镜像已经就绪时使用 true。
   # 138 AMD64 和不选 SuperMap 的发布使用 false。
   export ADDP_SUPERMAP_WORKFLOW_ENABLED=false
   COMPOSE_ENV_FILES="$PWD/.env.example" python3 scripts/prod/release-images.py prepare \
     --tag "$(git rev-parse HEAD)" --registry localhost:5001
   ```
2. 在操作端收齐两个 `dist/compose-release/manifest.json`，生成发布清单。将同一份 `release.json` 送回两端，在各自原生 Docker 环境生成离线包。两个构建端的 Docker 中均须保留冻结清单记录的镜像，并支持 `docker save --platform`。输出目录必须尚未存在，且其父目录在可写的数据盘上。导出期间需为未压缩和压缩归档预留空间。

   ```bash
   python3 scripts/prod/k3s-release.py pair \
     --arm64 arm64-manifest.json --amd64 amd64-manifest.json > release.json
   python3 scripts/prod/k3s-release.py export --release release.json --output /path/to/node-bundle
   ```

## 现场导入与调度

1. 每个 K3s Linux 节点收到对应架构的发布包。先按可信交付清单核对 `SHA256SUMS`，再运行以下命令。导入后核对每个镜像的配置摘要，成功才打印此发布的节点标签。由集群操作员用 `kubectl label node <节点名> <打印的标签>` 登记已验证的节点。扩容节点执行同样步骤。保留离线包用于缓存清理后的重新导入。

   ```bash
   cd /path/to/node-bundle
   sha256sum -c SHA256SUMS
   sudo python3 k3s-release.py load-node --bundle .
   ```

2. 站点 Kubernetes 工作负载模板的容器 `image` 使用 `addp-release.invalid/<项目>/<服务>`，项目取冻结清单中的 `infra`、`platform` 或 `runtimes`。指标来源使用 `sources/<源镜像名称>`。把站点工作负载以 Kubernetes JSON List 交给渲染入口，核对输出，再按站点安装顺序应用。站点配置、密钥、存储和入口策略独立交付。

   ```bash
   python3 k3s-release.py render --release release.json --workloads site-workloads.json > workloads.json
   ```

同一发布和服务在所有支持的架构使用同一个离线镜像引用。`release.json` 记录各架构的 Registry 摘要及 Docker 镜像 ID，`config-digests.json` 记录从对应 Docker 归档取得的原生镜像配置摘要，供 K3s 导入后核对。工作负载只调度到已导入该发布的节点，镜像从本机缓存获取。双架构镜像不添加架构或节点名选择器。单架构组件自动加架构约束。现有 PVC 及其存储拓扑继续决定有状态 Pod 的可用节点。跨节点扩副本前按组件确认并发执行语义。

## System 跨节点服务发现

空库先启动一个 System 并完成引导和模块声明发布。两个可调度节点须已经导入并校验同一发布的各自原生镜像。System 使用共享 PostgreSQL、相同的服务身份密钥、发现绑定和集群内可达的 Service 地址。现场部署清单负责提供这些配置。Gateway 的 `SYSTEM_URL` 指向 System Service，以便切换副本后仍可重新连接。

站点模板中为 System Pod 设置标签 `app.kubernetes.io/name=system-backend`。完成单副本引导后，在同一 Deployment 的 `spec` 中写入以下配置，保留发布节点标签、现场准入和资源约束，并确保两个节点都符合调度条件。用原有 `render` 入口生成清单后部署。

```yaml
replicas: 2
template:
  spec:
    topologySpreadConstraints:
      - topologyKey: kubernetes.io/hostname
        maxSkew: 1
        minDomains: 2
        whenUnsatisfiable: DoNotSchedule
        matchLabelKeys: [pod-template-hash]
        labelSelector:
          matchLabels:
            app.kubernetes.io/name: system-backend
```

```bash
kubectl -n "$SITE_NAMESPACE" rollout status deployment/system-backend
kubectl -n "$SITE_NAMESPACE" get pods -l app.kubernetes.io/name=system-backend -o wide
kubectl -n "$SITE_NAMESPACE" get endpointslices -l kubernetes.io/service-name=system-backend
```

验收须在两个不同节点各看到一个 Ready System Pod，并确认 System Service 选中了这两个端点。使用已授权的业务账号通过 Gateway 访问 Catalog。先撤回一个 Catalog Pod，确认两个 System 返回相同的剩余端点，再退出当前的发现写入者。持续检查 Gateway 业务响应和存活副本的路由 revision、端点标识及截止时间，观测窗口应超过原端点截止时间。通过条件为业务请求持续成功、已撤回的端点未重新出现、存活副本重新核对后延长了剩余端点的截止时间。恢复两个副本后，可交换故障方向复验。这个演练覆盖 System 进程失效，数据库和 K3s 控制节点的容灾各自验收。可信 Service 绑定、Meta 后台资格及单模块查询权限见[健康与平台控制面规范](../../docs/spec/api/健康与平台控制面.md#第三阶段-catalog-服务发现契约)。

## 有界 Worker 资源伸缩

发布包根据冻结的 platform 镜像清单，为 `meta-worker`、`transfer-bounded-worker`、`quality-worker`、`security-worker` 中实际交付的服务生成各自的伸缩清单。每个服务分别选择启用。continuous Worker 的在途同步会话需要另行验证 checkpoint、退避和恢复；此有界任务配置只用于上述四种 Worker。

两台可调度节点须已加载本次发布的原生镜像，集群须提供资源指标 API。Deployment 名、容器名和 Pod 标签 `app.kubernetes.io/name` 均使用该 Worker 的服务名。先用 `kubectl top nodes` 和 `kubectl top pods` 核对指标与可调度容量。空库先完成 System 引导、模块声明发布和对应 Backend 初始化，再启动 Worker；Backend Ready 后确认 Worker 已登记并持续上报。各进程使用独立身份，按 System 登记资格和本模块执行租约领取任务。

在发布包目录执行，以下示例为 Transfer 的有界 Worker：

```bash
WORKER=transfer-bounded-worker
kubectl -n "$SITE_NAMESPACE" patch deployment "$WORKER" --type strategic --patch-file "k3s-$WORKER-deployment-patch.json"
kubectl -n "$SITE_NAMESPACE" rollout status "deployment/$WORKER"
kubectl -n "$SITE_NAMESPACE" apply -f "k3s-$WORKER-autoscaling.json"
kubectl -n "$SITE_NAMESPACE" get "hpa/$WORKER" "pdb/$WORKER"
kubectl -n "$SITE_NAMESPACE" get pods -l "app.kubernetes.io/name=$WORKER" -o wide
```

示例配置保留 2 至 6 个副本，按 Worker 容器 CPU、内存相对资源请求的利用率伸缩，初始目标分别为 70% 和 80%。每个副本请求 250m CPU、256Mi 内存。现场应按任务规模和容量调整。后续发布将相同资源请求、滚动升级与跨主机分布写入站点 Deployment 清单，并交由 HPA 管理副本数，避免重新应用固定 replicas。滚动升级按 Deployment 的 `pod-template-hash` 只统计同版本副本，防止旧副本退出后两个新 Worker 都留在同一主机。现场集群须支持拓扑分布的 `matchLabelKeys`。缩容后复查存活副本所在节点，调度器仅在放置新 Pod 时计算分布。PDB 要求自愿驱逐时至少一个 Pod 可用。业务验收还须确认两个进程均可领取任务、同一执行只有一个有效持有者，并验证单副本退出后的在途租约恢复。

Compose 可用 `docker compose up -d --scale meta-worker=2 meta-worker` 配置常驻副本；独立部署可由现场进程管理器运行多个 Worker 进程。两者的 CPU／内存自动伸缩需要现场编排器。单主机上的多个进程共享同一故障域。

## 可选的 KingbaseES 试用业务库

内部验证站点可在原生 Linux AMD64 K3s 节点加载固定的 KingbaseES 官方 Docker 介质，并沿用镜像内置试用许可。站点自行保管下载介质，保留节点导入所需的原文件。既有 Compose 及正式许可验收入口见 [Business 部署配置](../../docs/spec/configuration/Business数据引擎部署.md)。

在目标 K3s 节点的检出目录执行：

```bash
sudo bash business/scripts/k3s-kingbase-trial.sh load-node /path/to/official-amd64-docker.tar
```

在已有专用业务命名空间的集群操作端执行。`AMD64_HOSTNAME_LABEL` 使用已导入镜像的节点的 `kubernetes.io/hostname` 标签值。预先限定业务库的网络可达范围，准备仓库外、仅部署者可读的环境文件。文件包含独立的 `DB_PASSWORD` 和 `ADDP_READER_PASSWORD`，只读账号密码使用 20–64 位字母数字组合。

```bash
kubectl -n "$SITE_NAMESPACE" create secret generic kingbase-trial-credentials \
  --from-env-file="$PRIVATE_KINGBASE_ENV"
bash business/scripts/k3s-kingbase-trial.sh render \
  "$SITE_NAMESPACE" "$AMD64_HOSTNAME_LABEL" kingbase-trial-credentials | kubectl apply -f -
bash business/scripts/k3s-kingbase-trial.sh initialize "$SITE_NAMESPACE"
```

工作负载使用 ClusterIP、`local-path` 持久卷和节点已校验的缓存镜像。数据库容器退出前通过原生命令正常停库。初始化复用 Business 的 SQL 样例，在一个事务中写入样例和探针。已有探针时直接保留样例。验证入口只读检查试用有效期及样例：

```bash
bash business/scripts/k3s-kingbase-trial.sh verify "$SITE_NAMESPACE"
```

System 以 `engine_type=kingbase` 登记。填写集群内 `kingbase.<namespace>.svc.cluster.local:54321`、数据库 `business` 和只读账号 `addp_reader`，在 NetworkPolicy 允许的范围内进行保存后的连接测试。重建验收使用同一 PVC，比较 Pod UID、试用有效期、样例记录数和连接结果。

## 唯一控制节点失联后的隔离恢复演练

主机仍可用时，将快照、站点配置、验证账号、原域名的 TLS 证书和原生镜像存到另一台机器。故障发生后，恢复入口仅使用备用机的 Docker Engine 和 Compose v2，无需原 K3s API、镜像仓库或源码。此入口恢复 Console、System 登录、Gateway、Catalog 前后端、平台 PostgreSQL、一个独立 Business PostgreSQL、Catalog 搜索和指定的业务 Bucket。Redis 从空缓存启动。其他业务引擎、Worker、监控、对象历史版本和对象访问策略有各自的恢复范围。

### 故障前准备

1. 两端准备 Python 3.11 或以上。源端使用现场已有的 kubectl 上下文，先核对节点、命名空间和 Deployment。脚本沿用本节所列站点的工作负载名称与 Secret。选一张有实际业务行、可完整校验的小表。查明当前在用、已经成功导出的 Meilisearch dump 的任务 UID 和文件名。在源站点操作端运行下例，私有目标目录必须尚未存在。

   ```bash
   umask 077
   python3 scripts/prod/k3s_dr_capture.py \
     --namespace "$SITE_NAMESPACE" --out "$PRIVATE_BACKUP/snapshot" \
     --business-db business --business-table addp_flow_102.source_sales \
     --search-dump 20261118-083308933.dump --search-task 84 \
     --bucket develop --bucket graph --bucket manager --bucket meta --bucket orchestrator \
     --bucket service --bucket standard --bucket system --bucket transfer
   ```

   dump 名称、任务号、业务库、业务表和 Bucket 按站点替换。每个 PostgreSQL 库独立导出 MVCC 快照，在同一事务核对验证表行数和内容摘要，并另存角色与授权。Bucket 保存所选范围的当前对象内容。脚本核对既有搜索导出的任务回执，记录当前索引、主键、设置和文档摘要。故障后必须能从该 dump 重现这些事实。在线采集期间，各库、对象和搜索没有共同的事务时间点。

2. 在备用 Linux 主机准备权限为 0700 的 `KIT`。通过受控加密通道送入 `snapshot/`，复制本目录的 `k3s_dr_capture.py` 和 `k3s-dr-recover.py` 到 `KIT/`。单独交付下列私有文件，权限设为 0600。备份含真实数据库角色、服务凭据及账号数据，按生产数据的权限和加密保留要求存放。

   | 文件 | 内容 |
   |---|---|
   | `tls/leaf.pem`、`tls/leaf.key` | 原域名的服务端证书链和匹配的私钥 |
   | `tls/ca.pem` | 演练客户端已经信任的 CA 公共证书 |
   | `login.json` | 有权读取 Catalog 的验证租户账号，字段为 `username`、`password`；存在多个租户时另填 `tenant_membership_id` |

   本次验证使用已有的密码认证租户账号。保持备用机的叶证书按期续签，尤其是使用短期 Caddy 内部证书时。CA 签发私钥留在原有签发设施。密码、会话令牌、证书私钥以及生成的 `compose.json` 均留在受控目录。

3. 将同一现场发布版本的原生镜像预载到备用机的本地 Docker。将如下服务名映射到逐项核对过的本地镜像引用，保存为 `references.json`。服务名为 `edge`（带 curl 的 nginx）、`console`、`catalog-frontend`、`system-backend`、`gateway`、`catalog-backend`、`postgres`、`redis`、`minio`（含 mc 和 curl）、`meilisearch`。查询归属可用 `docker image inspect`。在备用机冻结本地不可变 ID，离线转运可使用 Docker save/load：

   ```bash
   cd "$KIT"
   python3 k3s-dr-recover.py lock-images --references references.json --out images.json
   (cd snapshot && sha256sum -c SHA256SUMS)
   openssl x509 -in tls/leaf.pem -checkend 3600 -noout
   ```

   PostgreSQL 镜像须支持源库全部扩展的相同版本，数据库主版本一致、修订版不低于源库。现场的 138 使用 PostgreSQL 15、PostGIS 3.4.3、pgvector 0.8.0；通用 AMD64 镜像的 PostGIS 3.7 缺少源库所需的 `postgis_tiger_geocoder`。这个站点可在已有带 pgvector 的 Debian bookworm PostgreSQL 镜像上安装签名归档中的两个匹配包。把以下内容保存为私有构建目录的 Dockerfile，将基镜像参数设置为已经核实的镜像摘要，再运行 `docker build --build-arg BASE="$BASE" -t addp-dr-postgres:15-postgis343 .`，将输出镜像列入上述清单。

   ```dockerfile
   ARG BASE
   FROM ${BASE}
   RUN set -eu; \
       printf '%s\n' 'deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg] https://apt-archive.postgresql.org/pub/repos/apt bookworm-pgdg-archive main' >/etc/apt/sources.list.d/dr.list; \
       apt-get update; \
       apt-get install -y --allow-downgrades --no-install-recommends \
         postgresql-15-postgis-3=3.4.3+dfsg-2.pgdg120+1 \
         postgresql-15-postgis-3-scripts=3.4.3+dfsg-2.pgdg120+1; \
       test -f /usr/share/postgresql/15/extension/postgis_tiger_geocoder.control; \
       rm -rf /var/lib/apt/lists/* /etc/apt/sources.list.d/dr.list
   ```

### 在备用机计时恢复

使用备用机的本地 Linux Docker。恢复入口创建独立 Compose 项目、内部网络和空卷，从故障计时开始即禁止拉取镜像。专用客户端先向原主机 IPv4 地址发起同域名 HTTPS 请求，确认路由不可达，随后在同一客户端解析原域名到恢复入口，验证受信任 TLS、Console、Catalog 前端、用户登录及经过 Gateway 的租户目录查询。

```bash
cd "$KIT"
RUN_DIR="$PWD/../dr-$(date -u +%Y%m%dT%H%M%SZ)"
python3 k3s-dr-recover.py restore --kit "$PWD" --run-dir "$RUN_DIR" \
  --hostname addp.macstudio.local --primary-ip 192.168.88.102
cat "$RUN_DIR/result.json"
```

通过条件为 `primary_unreachable`、两个数据库内容校验、所选 Bucket 回读、搜索内容与设置校验、`login_verified` 和 `catalog_verified` 全部完成。登录和 Catalog 使用同一轮恢复数据签发的新会话。RTO 从开始独立恢复至 `catalog_verified`，包含制品校验和空卷恢复；故障发现、人工决策及局域网名称切换另计。各库的 `lower` 至 `observed` 限定取得快照的时间，`age_at_fault_seconds` 以较早端保守计算本次备份年龄。记录的行数、摘要用来核验内容。实际丢失条数需要在源库有独立提交证据时另行核算。

在 macOS 上运行 Colima 的现场，可以额外从操作员电脑测试同一 TLS 入口。先在 138 的 Linux 恢复环境取出该项目的入口地址：

```bash
PROJECT=$(cat "$RUN_DIR/project")
EDGE=$(docker compose -p "$PROJECT" -f "$RUN_DIR/compose.json" ps -q edge)
docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$EDGE"
```

在 138 的 macOS 终端将输出赋给 `EDGE_IP`，用仅本机可见的转发端口连接到恢复入口。在 101 的另一个终端建立第二段 SSH 转发。端口占用时选择另一个空闲高端口。两段转发均可用 Ctrl-C 结束。

```bash
# 138，SSH 配置来自本机现有 Colima 实例
SSH_CONFIG=$(mktemp)
colima ssh-config --profile addp-validation > "$SSH_CONFIG"
ssh -F "$SSH_CONFIG" -S none -o ExitOnForwardFailure=yes -N \
  -L "127.0.0.1:18443:$EDGE_IP:443" colima-addp-validation

# 101
ssh -o ExitOnForwardFailure=yes -N -L 127.0.0.1:18443:127.0.0.1:18443 addp-builder-138

# 101，在另一个终端；原域名、SNI 和证书校验均使用正式站点配置
/usr/bin/curl --noproxy '*' --connect-to addp.macstudio.local:443:127.0.0.1:18443 \
  -fsS -o /dev/null -w 'HTTP %{http_code}, TLS %{ssl_verify_result}\n' https://addp.macstudio.local/
```

演练只在隔离网络内发布原域名。正式接管之前，应先隔离主站写入，确认恢复点，再将现有 HTTPS 入口或受管名称解析切到备用机。使用 `.local` 的站点还须处理原主机 mDNS 广播与客户端缓存。发生新业务写入后恢复主站，要先对账、回迁并完成流量交接。

失败时检查专属项目，不会自动销毁现场。依据下列命令查看容器状态；包含服务配置的日志按私有材料保存。若需复跑，使用新的 `RUN_DIR`。演练取证完成后，以下清理仅针对该次项目及其卷：

```bash
PROJECT=$(cat "$RUN_DIR/project")
docker compose -p "$PROJECT" -f "$RUN_DIR/compose.json" ps -a
docker compose -p "$PROJECT" -f "$RUN_DIR/compose.json" down -v
```
