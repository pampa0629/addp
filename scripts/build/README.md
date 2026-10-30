# Build Scripts 使用指南

本目录维护编译、镜像构建、推送与部署包生成。命令在仓库根目录执行；正式编译和镜像构建分别使用 `make build` 与 `make build-images`，参数通过 `BUILD_ARGS` 与 `IMAGE_BUILD_ARGS` 传入。完整流程见[构建打包与推送](../docs/构建打包与推送.md)，部署操作见[部署指南](../../docs/guide/addp部署和开发步骤.md)。

## 目录

- [compile.sh](#compilesh) - Go 二进制编译
- [build-images.sh](#build-imagessh) - 产品镜像构建
- [build-supermap-workflow-base.sh](#build-supermap-workflow-basesh) - SuperMap 私有基础镜像
- [push-images.sh](#push-imagessh) - 跨 Registry 推送
- [package.sh](#packagesh) - 部署包生成

## compile.sh

编译正式 Go 服务、Worker 与共享启动工具。服务范围以 [compile.sh](compile.sh) 的 `SERVICES` 为准，不在文档维护第二份清单。

### 使用方法

```bash
# 默认编译当前架构的 Linux 产物
make build

# 编译本机 OS 产物
make build BUILD_ARGS=--local

# 编译所选服务的两种架构
make build BUILD_ARGS="--arch both --services system-backend,gateway"
```

### 参数说明

| 参数 | 值 | 说明 |
| --- | --- | --- |
| `--arch` | `amd64` / `arm64` / `both` | 默认当前主机架构；`both` 编译两种架构 |
| `--local` | 无 | 使用 `go env GOOS` 指定的本机目标系统 |
| `--services` | 逗号分隔的服务名 | 默认 `all`；名称必须在脚本登记，空清单、未知项和重复项均失败 |
| `--force` | 无 | 忽略产物指纹的复用判断，重新编译 |

### 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `BUILD_TYPE` | `release` | 产物目录标识，如 `release` 或 `debug`；不等同于自动启用调试编译选项 |
| `GOOS` | `linux` | 目标系统；`--local` 改用 `go env GOOS` |
| `ADDP_BUILD_GOMODCACHE` | 仓库 `.gomodcache` | Go 模块缓存 |
| `ADDP_BUILD_GOPATH` | 仓库 `.gopath` | Go 工作目录 |
| `ADDP_BUILD_GOCACHE` | 仓库 `.cache/go-build` | Go 编译缓存 |

### 输出目录结构

产物位于 `dist/{BUILD_TYPE}-{GOOS}-{arch}/`；例如 `dist/release-linux-amd64/system`、`meta-worker`、`transfer-bounded-worker`、`transfer-continuous-worker` 和 `runtime-log`。Backend 名称去掉 `-backend` 后缀，Worker 保留 `-worker`。`--local` 的产物供本机使用，不能代替 Linux 产品镜像所需二进制。

### 智能缓存机制

缓存比较真实 Go 依赖图、共享代码、构建配置及工具链输入的指纹，不按文件修改时间判断。已有产物缺少指纹也须重编译。构建前后输入必须一致，成功后才替换正式二进制并记录指纹；失败清理本次临时产物。原子构建与身份规则见[生命周期规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md#二原子构建)。

保留 Go 模块和编译缓存；需要复验产物时使用 `make build BUILD_ARGS=--force`，不以定期删除缓存作为日常构建步骤。

## build-images.sh

构建 `{REGISTRY}/addp-{SERVICE}:{IMAGE_TAG}` 产品镜像。默认构建当前主机架构的 Linux 镜像，并推送到所选 Registry；`--verify` 只在本机验证产品镜像。

操作边界导航：[参数与服务选择](#参数与服务选择时序) · [镜像复用与缓存](#镜像复用与缓存边界) · [daemon 配置](#docker-daemon-配置边界) · [多架构构建器](#多架构构建器边界) · [构建结果与摘要](#构建结果与摘要)。

### 使用方法

```bash
# 常规构建并推送
make build-images

# 只验证所选镜像，不推送产品镜像
make build-images IMAGE_BUILD_ARGS="--services system-backend,agent-backend,console,nginx --verify --jobs 1"

# 查看 CI 基线及当前改动影响的服务
make select-image-services
```

### 参数说明

| 参数 | 值 | 说明 |
| --- | --- | --- |
| `--registry` | Registry 地址 | 默认 `localhost:5001` |
| `--tag` | 标签 | 覆盖 `IMAGE_TAG`，默认 `latest` |
| `--services` | 逗号分隔的服务名 | 默认 `all`；非 `all` 清单按登记名称匹配与数量比对，失败时机见[检查时序](#参数与服务选择时序) |
| `--multi-arch` | 无 | 请求 `linux/amd64,linux/arm64` 构建并推送；仍受各 Runtime 平台约束限制 |
| `--force` | 无 | 跳过整镜像复用判断；与 Docker 层缓存的区别见[缓存边界](#镜像复用与缓存边界) |
| `--skip-cache` | 无 | 通用构建不附加 `--cache-from`；不强制重建整镜像，见[缓存边界](#镜像复用与缓存边界) |
| `--verify` | 无 | 非交互强制构建，不推送产品镜像、不修改 Docker daemon 配置；只支持单一本机平台，不能与 `--multi-arch` 同用 |
| `--jobs` | `1` / `2` | 默认 `2`；两种取值使用同一调度路径 |

### 参数与服务选择时序

[build-images.sh](build-images.sh) 的检查分散在不同阶段，当前执行顺序如下：

| 阶段 | 实际检查与失败时机 |
| --- | --- |
| 工具链 | 先执行共享 Node 校验，再进入参数循环；工具链不符时不会继续解析选项。规则见 [Node 工具说明](../utils/README.md#node-runtimesh)。 |
| 参数解析与组合 | 未知选项、非 `1`／`2` 的 `--jobs`，以及 `--verify` 与 `--multi-arch` 同用，在进入 `main` 前非零退出。`--registry`、`--tag`、`--services` 直接取下一实参，没有统一的空值和格式预检；不能把解析通过视为参数全部有效。 |
| 环境准备 | 普通模式先走 [daemon 配置](#docker-daemon-配置边界)，再检查 [Buildx](#多架构构建器边界) 和 Registry；`--verify` 只跳过 daemon 配置阶段。此时尚未核对服务清单。 |
| 服务选择 | 从 `main` 的登记清单匹配非 `all` 输入，再比较匹配数量与解析数量，发现未知或重复服务时返回失败。该检查早于基础镜像预热和产品任务调度，但晚于上述环境准备。 |
| 后续准备与构建 | 通过选择后才预热基础镜像、判断整镜像复用并调度任务；预编译产物、专用 Runtime 平台等约束还可能在缓存或实际构建分支中发现，并非全部集中在前置检查。 |

因此，服务名错误返回失败时，先前的 daemon 配置、Docker 重启或同名构建器处理可能已经发生；失败不自动回滚这些准备操作。执行前按登记清单选择服务，不以无效 `--services` 试探环境；阶段退出与报告缺失按[构建结果与摘要](#构建结果与摘要)分别核对。本文说明既有实现的检查范围，不将分散检查描述为一次无副作用的完整预检。

### 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `REGISTRY` | `localhost:5001` | 镜像目标 Registry |
| `IMAGE_TAG` | `latest` | 镜像标签，可由 `--tag` 覆盖 |
| `BUILD_TYPE` | `release` | 读取对应类型的预编译 Go 产物，须与编译时一致 |

### 构建的服务列表

服务与构建上下文以 [build-images.sh](build-images.sh) 的 `main` 登记为准；`make select-image-services` 输出 CI 的构建选择，不代替正式服务登记。新增正式服务、Worker、前端或 Compose 镜像时，须同时补齐构建登记；检查入口为 `make test-build-registration`。

### 前置条件

所选 Go 镜像使用预编译二进制，须先用 `make build` 生成对应 `BUILD_TYPE` 和 Linux 架构的产物。前端与独立 Runtime 按各自 Dockerfile 准备依赖，不要求无关 Go 二进制。

多架构构建需要 Docker Buildx 和可写 Registry。Model3D 当前每次只构建一个原生 Linux 平台；SuperMap 依赖私有 Linux ARM64 基础镜像，不能把全量 `--multi-arch` 当作所有 Runtime 已支持双架构的承诺。

### 镜像复用与缓存边界

镜像准备使用 [build-images.sh](build-images.sh) 的实际判定，需区分以下层次：

| 层次 | 当前依据 | 证明范围 |
| --- | --- | --- |
| Go 二进制复用 | `compile.sh` 的构建输入内容指纹，见[编译缓存](#智能缓存机制)。 | 只证明该编译产物的输入判定，不等于镜像已按同一输入重建。 |
| 整个产品镜像跳过构建 | `check_service_changed` 文本匹配远端 `/tags/list` 中的目标 tag，再把所选输入的修改时间与 `.build-cache/<service>-<tag>.timestamp` 比较。 | 这是 tag 与时间戳判定，不核验镜像 digest、完整构建上下文或各目标平台内容；日志中的“up-to-date”不能作为源码一致性证据。 |
| Docker 层缓存 | 通用单平台构建在本机镜像可 inspect 时附加 `--cache-from`；多平台构建在远端 manifest 可 inspect 时引用 Registry 层缓存。 | 只提供构建可用的缓存来源，不能替代本次构建、推送或运行结果。 |

整镜像时间戳文件不包含 Registry、目标架构或 `BUILD_TYPE`，缺失时按 0 读取。Go Backend／Worker／Gateway 分支主要比较请求中首个架构的预编译二进制及 `runtime-log` 时间，Manager 另比较随镜像打包的插件和 Dockerfile；前端、Python 与各 Runtime 只枚举脚本分支指定的文件和共享依赖。具体输入范围以源码为准，不能把部分文件枚举、未变的时间戳或读取异常解释为完整上下文已核验，也不能把一次复用外推到另一个 Registry 或全部平台。

`--force` 和 `--verify` 都跳过整镜像复用判断。`--skip-cache` 只移除通用构建的显式 `cache-from`，不会跳过上层复用判断，也不追加 `--no-cache` 或 `--pull`，更不会清理共享 Docker 缓存；`--force` 同样不保证 Docker 层重新执行。SuperMap 的专用分支显式使用 `--no-cache`，Model3D 委托自己的构建脚本，各自限制仍按[前置条件](#前置条件)和对应 Runtime 说明核对。

基础镜像预热发生在整镜像复用筛选之前，所选产品镜像全部跳过也不表示整个流程没有 Registry 访问或基础镜像推送。当前代码在普通构建／推送的成功分支记录完成时刻，`--verify` 不写产品构建成功时间戳。新建、跳过和失败项的判定见[构建结果与摘要](#构建结果与摘要)；跨 Registry 推送还有独立的[本地推送记录](#工作原理)，不能与这里的构建时间戳混用。

### Docker daemon 配置边界

普通构建在镜像操作前执行 [build-images.sh](build-images.sh) 的 `check_docker_config`：任一 `~/.docker/daemon.json` 或 `/etc/docker/daemon.json` 存在即继续，不解析其内容，也不证明当前 daemon 已使用这些配置。两者均不存在时进入交互选择：

- 选择自动配置：macOS 写入 `~/.docker/daemon.json` 并尝试退出、重开 Docker Desktop；Linux 要求当前进程为 root，写入 `/etc/docker/daemon.json`，再执行 daemon-reload 和 Docker 重启。函数按固定模板整体写文件，已有目标会先生成时间戳备份，不做字段合并或失败后的自动恢复。
- 选择继续：不配置镜像源，直接进入后续构建准备。
- 选择手工配置并退出，或输入非法选项：当前入口非零结束。自动配置函数失败却会被调用方打印后继续，不能把构建流程继续执行视为配置成功。

自动配置会影响主机 Docker，须由操作者确认其运行范围；它与[独立 Registry 信任配置脚本](../registry/README.md#外部-registry-信任配置)的参数、文件分支和确认方式不同。`--verify` 跳过此 daemon 配置阶段，但仍执行 Registry 检查及基础镜像预热／推送；它只禁止产品镜像推送，不代表整个入口只读或不访问 Registry。

### 多架构构建器边界

`check_buildx` 在本机单平台模式直接返回，不创建构建器；`--verify` 与 `--multi-arch` 的组合在进入构建准备前即被拒绝。选择多架构时，脚本检查 Buildx 可用性，并按固定名称处理 `addp-builder`：

- 同名构建器可 inspect 时先尝试删除，删除失败被忽略，再执行创建；不按工作区核验归属，也不复用用户预先选择的其他 builder。执行前须确认该同名资源允许被替换。
- 把固定镜像源模板写入 `/tmp/buildkitd.toml`，用 `docker-container` 驱动、host 网络、该配置和 `--bootstrap` 创建构建器，`--use` 会将其设为当前选择。
- 创建成功后删除临时配置；创建失败时可能未到删除步骤。现有退出清理只回收构建调度进程与日志目录，不删除该构建器、不恢复之前的选择，也不清理这份独立配置文件。

构建器启动成功只证明准备步骤完成；目标架构的基础镜像、各 Runtime 支持范围、产品镜像与推送结果仍分别核对。平台限制见[前置条件](#前置条件)，Registry 与主机配置见[daemon 配置边界](#docker-daemon-配置边界)。

### 构建结果与摘要

基础镜像预热完成后才调度产品镜像；各任务写入独立临时日志：单并发持续输出新增内容，多并发在任务完成时输出，再汇总退出码、耗时与失败项。调度报告的任何非零任务结果均使整个构建失败；调度阶段中断时终止并回收本次构建进程，清理临时日志，不停止开发服务或删除已有镜像和数据卷；构建器和独立 BuildKit 配置的保留范围见[多架构构建器边界](#多架构构建器边界)。

终端的跳过项只记录外层整镜像复用，已调度任务另按退出码归并。`build_service` 在服务目录不存在时只打印警告并返回 0，该项不会进入外层跳过列表，仍可能被计入成功构建数量；因此汇总数量和零退出不能证明每个所选服务都新建了镜像或完成推送，须核对原始任务日志与实际镜像。

设置 `ADDP_CI_SUMMARY_FILE` 且执行到摘要写入分支时，额外记录所选服务、失败项和任务退出码／耗时；未设置或在工具链、参数、环境、服务筛选／基础镜像准备阶段提前失败时，可能不生成本次摘要。脚本不在入口清除该路径的旧文件，已有文件须确认属于本次运行，不能把旧报告作为当前失败回执。

摘要不记录最终镜像 digest、平台清单或源码绑定，也不是发布制品清单；新建、复用、缺失跳过和未执行项按实际证据分别认定。

### 构建与 CI 边界

前端基础镜像版本统一读取根 `.node-version`，通过 `NODE_VERSION` 构建参数传入。所有前端使用 `npm ci`，包含 Linux musl 平台包的锁文件是不可变构建输入。

前端只复制依赖清单、构建配置、源码和静态资源，不把宿主机的 `node_modules` 或 `dist` 覆盖到镜像构建阶段。

基础镜像预热只处理所选服务实际引用的镜像：优先复用本机同架构镜像，否则最多尝试拉取三次；拉取或推送到临时 Registry 失败仍会使构建失败。
前端通过 `npm ci` 消费锁文件中的 Linux arm64-musl 与 x64-musl 可选包；Dockerfile 不追加架构专用安装命令。构建登记检查核对两类锁定包并拒绝写死架构包名，入口为 `make test-build-registration`，同时纳入 `make test-platform`。共享依赖布局、源码复制与 Nginx 路径要求见[前端镜像构建规范](../docs/前端镜像构建规范.md)。

Platform CI 通过 `make select-image-services` 保留 System Backend、Agent Backend、Console 和 Nginx 四类构建基线，再自动追加本次改动影响的模块镜像。`common/`、`common-frontend/`、`common-python/` 按实际消费者扩散，`.dockerignore` 变更扩散到全部 Hosted Runner 可构建镜像。Model3D 统一 Linux 构建支持 amd64/arm64，Model3D 或其 common-python 依赖变化会进入 Hosted amd64 镜像验证。只有 SuperMap 私有 arm64 基础镜像不在 GitHub amd64 Runner 范围内。

配置文件存在、daemon 重启或 Docker API 就绪均不能证明所选镜像实际构建与推送成功；镜像构建通过也不代替真实服务、分布式 Worker 或产品发布验收。

## build-supermap-workflow-base.sh

**用途**: 使用完整 SuperMap iObjects C++ SDK 母版构建稳定基础镜像。基础镜像包含完整 SDK、许可、C++ 构建工具和系统依赖，不包含 ADDP Runtime 源码；完整 SDK 用于日常编译和未来扩展算子，最终运行镜像会另行裁剪。

完整 SDK 母版保存在仓库外，并通过 `SUPERMAP_CPP_SDK_PATH` 显式传入；许可固定放在 Git 忽略的 `engines/supermap-workflow/vendor/license`。日常代码镜像构建通过 `.dockerignore` 排除整个 `vendor/`，不会把许可或完整 SDK 放入普通构建上下文。

### 使用方法

```bash
SUPERMAP_CPP_SDK_PATH=/path/to/supermap-iobjectscpp-12.1.0-linux-arm64-all \
  bash scripts/build/build-supermap-workflow-base.sh
```

基础镜像只在首次安装、SuperMap C++ SDK 或许可升级时重建。日常 C++ 开发统一执行：

```bash
bash scripts/dev/restart.sh -supermap-workflow
```

局部重启和 `restart.sh -all` 会根据 C++ 源码、CMake、Dockerfile、基础镜像 ID 和目标平台计算构建指纹。输入变化时在多阶段镜像中重新编译并测试 C++ Runtime，然后替换运行容器；输入未变化时复用现有代码镜像。

## push-images.sh

将本机源镜像重新标记并推送到目标 Registry，适用于构建 Registry 与发布 Registry 不同的情况。普通 `make build-images` 已向所选 Registry 推送产品镜像，无需为同一目标重复推送。

### 使用方法

```bash
# 登录目标 Registry 后，推送指定服务与版本
# docker.io/myorg、Harbor 项目和其他 namespace 均按同一参数传入
docker login harbor.example.com:5001
bash scripts/build/push-images.sh --registry harbor.example.com:5001/addp --tag v1.0.0 --services system-backend,manager-backend,gateway

# 仅查看拟推送项；仍检查 Docker 与本机源镜像
bash scripts/build/push-images.sh --registry harbor.example.com:5001/addp --tag v1.0.0 --services system-backend,gateway --dry-run
```

### 参数说明

| 参数 | 值 | 说明 |
| --- | --- | --- |
| `--registry` | 目标地址 | 必填，包含所需 namespace / project |
| `--tag` | 标签 | 默认读取 `IMAGE_TAG`，缺省为 `latest` |
| `--services` | 逗号分隔的服务名 | 默认 `all`；按推送脚本的 `ALL_SERVICES` 遍历 |
| `--source-registry` | 源地址 | 默认读取 `SOURCE_REGISTRY`，缺省为 `localhost:5001` |
| `--dry-run` | 无 | 不重新标记或推送镜像，只显示计划 |
| `--force` | 无 | 跳过本地推送缓存，重新推送 |

### 镜像命名规则

普通服务使用 `{SOURCE_REGISTRY}/addp-{service}:{tag}` 作为源，目标为 `{TARGET_REGISTRY}/addp-{service}:{tag}`。每个服务对应独立 repository，版本用 tag 表达；生产发布使用明确版本，并在构建、推送、打包和部署时保持一致。

| Registry 类型 | 目标地址示例 |
| --- | --- |
| Docker Hub | `docker.io/USERNAME` |
| Harbor | `harbor.example.com:5001/PROJECT` |
| 私有 Registry | `registry.company.com:5000` |
| GitHub Container Registry | `ghcr.io/USERNAME` |
| 阿里云 ACR | `crpi-xxx.cn-region.personal.cr.aliyuncs.com/NAMESPACE` |

### 推送的镜像列表

完整清单以 [push-images.sh](push-images.sh) 的 `ALL_SERVICES` 为准，不在文档固定镜像数量。当前默认清单还包含 `postgres`：它使用本机 `pampa0629/addp-postgres:15-arm64`，不读取 `SOURCE_REGISTRY` 或普通服务的标签；不能据此推断它支持其他架构。

### 前置条件

源镜像必须已经存在，目标账号须具有推送权限并完成 `docker login`。登录使用 Registry 主机，目标参数可额外包含 namespace / project。

### 工作原理

脚本依次检查源镜像、重新标记、推送并输出成功、失败、缺失跳过与缓存复用摘要。每个镜像最多尝试推送 5 次，重试间隔 15 秒。成功后在 `.push-cache` 记录目标引用与本机源 image ID；缓存命中只表示该本地记录未变化，不是对远端镜像仍存在的实时验证。

源镜像缺失会跳过；存在其他成功或缓存复用项时，命令可能返回成功。因此发布前须核对摘要中的缺失项，不能把退出码为零等同于所需镜像全部送达。

### 故障排查

| 问题 | 核对与处理 |
| --- | --- |
| 源镜像不存在 | 核对源 Registry、标签和本机镜像；按上文先编译所需 Go 产物，再构建镜像 |
| `denied` | 核对目标 namespace、账号权限及 `docker login` 主机 |
| 推送超时 | 核对网络和目标 Registry，可用 `--services` 缩小本次范围 |
| `insufficient_storage` | 由目标 Registry 管理员核对配额和保留策略 |

## package.sh

生成包含现有镜像或 Registry 配置的部署包，不在目标服务器重新编译应用。

### 使用方法

```bash
# 导出本机已有镜像，生成离线镜像包
IMAGE_TAG=v1.0.0 bash scripts/build/package.sh --mode images

# 生成目标 Registry 的配置目录
IMAGE_TAG=v1.0.0 bash scripts/build/package.sh --mode registry --registry harbor.example.com:5001/addp
```

### 参数说明

| 参数 | 值 | 说明 |
| --- | --- | --- |
| `--mode` | `images` / `registry` | 默认 `images`；不接受 `offline` |
| `--registry` | 地址 | 默认读取 `REGISTRY`，缺省为 `localhost:5001` |
| `--server` | `user@host` | 生成后传输到远端 `~/addp-deploy/`，优先 rsync，缺少时使用 scp |

`IMAGE_TAG` 默认 `latest`，决定 images 模式导出的镜像版本。两种模式都会复制 `.env.example`；根 `.env` 存在时也会复制，包及其传输目标按含凭据的部署制品管理。部署时须核对实际 `.env` 的 Registry 和版本，不能仅靠包目录名判断。

### 两种部署模式

| 模式 | 前提与内容 | 服务器操作 | 产物 |
| --- | --- | --- | --- |
| `images` | 本机已构建的 ADDP 镜像，通过 `docker save` 导出；附配置和加载脚本 | 使用 `load-images.sh` 加载，再按生产入口部署 | 目录及 tarball |
| `registry` | 目标 Registry 已有所需版本镜像；只生成部署配置，不导出镜像 | 核对 Registry 访问与凭据，拉取所需镜像后部署 | 配置目录，不生成 tarball |

images 模式的导出清单由 [package.sh](package.sh) 的 `export_images` 维护；缺失或导出失败的镜像会列入摘要，但不会阻止后续打包。包生成成功不证明镜像清单完整，也不证明所有 Infra、业务引擎与外部依赖已离线齐备。部署前须核对所需镜像、架构和版本，不保留固定包体积或导出数量承诺。

### 生成的包结构

两种模式均生成 `dist/package-{mode}-{timestamp}/`，包含三份根 Compose 定义、环境模板、Infra/Prod 脚本、Business 目录、Nginx 配置、`README.md` 和 `DEPLOY_INFO.txt`；已有根 `.env` 时同时携带该文件。两种模式都会更新平台与 Runtime Compose 中的 Registry 地址。

images 模式额外包含 `images/*.tar` 与 `load-images.sh`，生成 `dist/addp-deploy-images-{timestamp}.tar.gz`；registry 模式保留配置目录，可用 `--server` 传输。实际复制范围和成功项以脚本及打包输出为准，不把清单存在当作完整部署验收。

## 完整的构建流程

编译、镜像构建、跨 Registry 推送、打包和部署的顺序统一见[构建打包与推送](../docs/构建打包与推送.md)。本地容器与生产环境分别使用 [Local 指南](../local/README.md)和 [Prod 指南](../prod/README.md)的标准生命周期入口，不复制直接 Compose 启停或临时服务器命令。

CI 编排与标准验证见[测试与发布入口](../docs/测试与发布入口.md#ci-登记与隔离检查)及[测试与验收规范](../../docs/spec/addp测试与验收规范.md)，以仓库实际 Workflow 为准，不维护示例 release YAML。

## 故障排查

| 问题 | 核对与处理 |
| --- | --- |
| Go 镜像缺少二进制 | 核对 `dist/{BUILD_TYPE}-linux-{arch}/`，按所选平台执行 `make build BUILD_ARGS="--arch <架构> --services <服务列表>"` |
| Buildx 不支持多平台 | 检查 `docker buildx ls` 与 bootstrap 原始错误，按[构建器边界](#多架构构建器边界)核对固定同名资源及 Registry；单平台验证使用 `--verify` |
| Registry 不可达 | 按 [Registry 指南](../registry/README.md)核对实际部署；不直接创建第二个同名 Registry 容器 |
| 产物复用不符合预期 | Go 编译的 `BUILD_ARGS=--force` 见[编译缓存](#智能缓存机制)；镜像的 `IMAGE_BUILD_ARGS=--force`、tag／时间戳和层缓存分别按[镜像复用边界](#镜像复用与缓存边界)核对 |

## 相关文档

- [脚本总导航](../README.md)
- [部署和开发步骤](../../docs/guide/addp部署和开发步骤.md)
- [前端镜像构建规范](../docs/前端镜像构建规范.md)
- [生产部署脚本](../prod/README.md)
- [开发环境脚本](../dev/README.md)
- [项目总体架构](../../CLAUDE.md)
