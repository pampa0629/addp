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

### 启动与重建边界

[start.sh](start.sh) 固定使用容器名 `registry`、镜像 `registry:2` 和宿主机 `5001` → 容器 `5000` 映射。运行中的同名容器通过回环 `/v2/` 探测时直接复用；同名容器已停止或探测失败时，脚本会执行 `docker rm -f` 后新建，而非仅做只读健康确认。它按名称查找，不核验 Compose／工作区归属、镜像或实际挂载；执行前须确认该同名容器确属本地构建用途。新建命令未限定回环发布地址，也未配置 TLS／认证；本机 URL 不能作为运行实例仅本机可访问或具有访问控制的证明。

新建命令没有显式复用持久卷，脚本也不负责备份／恢复；删除后重建不能视为镜像目录会保留，更不能把未显式删除卷视为数据已恢复或已清理。启动后的轮询只检查固定回环 `/v2/`，探测耗尽时非零退出，但不自动清理本次已创建容器。仓库不提供第二套初始化脚本，也不通过根 `Makefile` 提供停止／删除目标；这些限制不意味着 `registry-start` 没有删除副作用。

### 状态查询与验证范围

[check.sh](check.sh) 先按固定名称查询容器是否存在和运行，再以带 HTTP 失败判定及 3 秒超时的请求探测 `http://localhost:5001/v2/`；任一步未满足时非零退出。Docker 查询异常也可能被归入“容器不存在”，不能仅凭提示确认根因；容器名称与回环响应同时存在也不证明该响应由此容器提供。

后续 `/_catalog` 请求没有 HTTP 失败判定或显式超时，输出只按文本提取 `addp-` 名称，未校验完整 JSON、分页、tag 或 digest。传输错误可能在 `set -e` 的赋值步骤提前退出；HTTP 错误响应或解析不完整却仍可能到达末尾“Healthy”。因此目录展示和零退出不能证明镜像清单完整、所需版本存在、Docker daemon 认证／信任有效或实际 pull／push 成功，也不能替代镜像构建、公开 HTTPS 与产品验收。

## 外部 Registry 信任配置

该入口修改或提示配置 Docker 的 `insecure-registries`，不创建 Registry。由管理员先确认主机 Docker 的配置归属和允许重启的范围，再在自己的终端运行；地址参数使用实际 `host:port`，不带协议前缀：

```bash
bash scripts/registry/configure.sh 192.168.1.100:5001
```

[configure.sh](configure.sh) 的操作取决于当前平台及文件状态：

| 分支 | 实际写入与重启行为 |
| --- | --- |
| macOS（`OSTYPE` 以 `darwin` 开头） | 打印 Docker Desktop 设置步骤，按固定路径尝试读取 `settings.json`，等待按键；脚本不写配置、不自动重启。用户按提示执行 Apply & Restart 才产生配置和重启操作。 |
| Linux 路径，已有 `/etc/docker/daemon.json` 且文本匹配地址 | 打印“已在信任列表中”，不编辑或重启；匹配只用 `grep`，不解析 JSON，也不核验 daemon 已加载该配置。 |
| Linux 路径，已有文件但未匹配地址 | 显示现有配置并询问是否现在编辑；回答 y／Y 后执行 `sudo nano`，编辑器成功退出后执行 `sudo systemctl restart docker`。这一分支没有 `service` 回退，也不检查编辑后的 JSON 或目标配置是否真正改变。 |
| Linux 路径，文件不存在 | 不经上述 y／N 确认，直接通过 sudo 创建目录，把固定 `/tmp/daemon.json` 移入目标并设为 644；随后优先用 `systemctl`、否则用 `service` 重启 Docker。 |

脚本把其他非 `darwin` 环境也分入 Linux 路径，这不构成其他系统已受支持的证明。Linux 分支不自动备份或回滚；编辑、权限或重启失败可能在 `set -e` 下提前退出，已经写入的内容仍须单独核对。运行前须确认文件是否存在，不能把交互式入口理解为每次写入或重启前都会询问。

末尾只由宿主机 `curl -s` 请求 `http://<地址>/v2/`，没有 HTTP 失败判定或显式超时：401／404 等响应也可能显示“连接成功”，连接失败只打印提示后继续显示“配置完成”。即使没有编辑配置或返回零退出，也不能证明 daemon 信任已生效、认证／TLS 正确、目标镜像存在或实际 pull／push 成功；这些结果需按所选 Registry 和镜像分别验证。Linux 编辑提示会显示现有 daemon 配置，终端原始输出不宜直接作为脱敏证据。

生产镜像仓库的地址、认证、持久化、备份和高可用由部署环境负责。镜像构建脚本还有独立的[daemon 配置分支](../build/README.md#docker-daemon-配置边界)，其写入、确认和失败行为不沿用上表。

## 故障排查

```bash
docker logs registry
curl http://localhost:5001/v2/
lsof -i :5001
```

镜像构建的完整入口和参数见 [构建脚本说明](../build/README.md)。
