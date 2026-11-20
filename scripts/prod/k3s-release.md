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

## Meta Worker 资源伸缩

两台已加载相同发布镜像的可调度节点、可用的 Kubernetes 资源指标 API，以及名为 `meta-worker` 的 Deployment 和容器是这份配置的前提。Pod 模板使用标签 `app.kubernetes.io/name=meta-worker`；工作负载须能调度到两个不同主机。由现场工作负载清单解除单主机约束，保留发布版本和存储约束。先检查 `kubectl top nodes` 和两端容量。每个 Worker 使用独立进程身份，继续由 System 登记及数据库执行租约控制新任务领取。

在发布包所在目录执行：

```bash
kubectl -n "$SITE_NAMESPACE" patch deployment meta-worker --type strategic --patch-file k3s-meta-worker-deployment-patch.json
kubectl -n "$SITE_NAMESPACE" rollout status deployment/meta-worker
kubectl -n "$SITE_NAMESPACE" apply -f k3s-meta-worker-autoscaling.json
kubectl -n "$SITE_NAMESPACE" get hpa,pdb
kubectl -n "$SITE_NAMESPACE" get pods -l app.kubernetes.io/name=meta-worker -o wide
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
