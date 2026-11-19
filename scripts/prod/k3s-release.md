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
2. 在操作端收齐两个 `dist/compose-release/manifest.json`，生成发布清单。将同一份 `release.json` 送回两端，在各自原生 Docker 环境生成离线包。输出目录必须尚未存在，且其父目录在可写的数据盘上。两个构建端的 Docker 中均须保留冻结清单记录的镜像。

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

同一发布和服务在所有支持的架构使用同一个离线镜像引用。两份包各自记录原 Registry 摘要和镜像配置摘要。工作负载只调度到已导入该发布的节点，镜像从本机缓存获取。双架构镜像不添加架构或节点名选择器。单架构组件自动加架构约束。现有 PVC 及其存储拓扑继续决定有状态 Pod 的可用节点。跨节点扩副本前按组件确认并发执行语义。

